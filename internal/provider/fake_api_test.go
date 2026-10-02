package provider

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"regexp"
	"strings"
	"sync"
	"testing"

	"github.com/hashicorp/terraform-plugin-testing/helper/resource"
	"github.com/hashicorp/terraform-plugin-testing/terraform"
)

// fakeAPI is an in-memory stand-in for the SutramX endpoints the provider
// uses, so plan/apply/import/destroy cycles run in CI without an account.
// It mirrors the server behaviour that matters to Terraform: keyed upserts,
// unmanaged omitted fields, lower-cased tags and masked integration config.
type fakeAPI struct {
	mu          sync.Mutex
	nextID      int
	monitors    map[string]map[string]any // id -> monitor
	pages       map[string]map[string]any
	connections map[string]map[string]any
	// Read-only lists (GET /maintenance, GET /settings/escalation-policies).
	maintenance []any
	escalation  []any
}

func newFakeAPI() *fakeAPI {
	return &fakeAPI{monitors: map[string]map[string]any{}, pages: map[string]map[string]any{}, connections: map[string]map[string]any{}}
}

func (f *fakeAPI) id() string {
	f.nextID++
	return fmt.Sprintf("00000000-0000-4000-8000-%012d", f.nextID)
}

func writeJSON(w http.ResponseWriter, status int, body any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(body)
}

func notFound(w http.ResponseWriter) {
	writeJSON(w, http.StatusNotFound, map[string]any{"error": "Not found", "code": "NOT_FOUND"})
}

func (f *fakeAPI) monitorByKey(key string) map[string]any {
	for _, monitor := range f.monitors {
		if monitor["external_id"] == key {
			return monitor
		}
	}
	return nil
}

func (f *fakeAPI) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if r.Header.Get("Authorization") != "Bearer sk_test" && !strings.HasPrefix(r.URL.Path, "/catalog") {
		writeJSON(w, http.StatusUnauthorized, map[string]any{"error": "Invalid API key"})
		return
	}
	var body map[string]any
	_ = json.NewDecoder(r.Body).Decode(&body)
	parts := strings.Split(strings.Trim(r.URL.EscapedPath(), "/"), "/")
	for i := range parts {
		parts[i], _ = url.PathUnescape(parts[i])
	}
	// Route on the resource prefix: "automation/monitors", "status/pages", or the first segment.
	prefix := parts[0]
	if (parts[0] == "automation" || parts[0] == "status") && len(parts) > 1 {
		prefix = parts[0] + "/" + parts[1]
	}
	route := r.Method + " " + prefix

	switch {
	case route == "PUT automation/monitors" && len(parts) == 5 && parts[2] == "by-id":
		monitor := f.monitors[parts[3]]
		if monitor == nil {
			notFound(w)
			return
		}
		monitor["external_id"] = body["key"]
		writeJSON(w, 200, monitor)
	case route == "PUT automation/monitors" && len(parts) == 3:
		key := parts[2]
		monitor := f.monitorByKey(key)
		action := "updated"
		if monitor == nil {
			action = "created"
			monitor = map[string]any{"id": f.id(), "external_id": key, "interval_seconds": 120, "config": map[string]any{}, "tags": []string{}, "probe_regions": nil, "url": nil}
			f.monitors[monitor["id"].(string)] = monitor
		}
		for _, field := range []string{"name", "type", "url", "interval_seconds", "config"} {
			if value, ok := body[field]; ok {
				monitor[field] = value
			}
		}
		if tags, ok := body["tags"].([]any); ok {
			lowered := []string{}
			for _, tag := range tags {
				lowered = append(lowered, strings.ToLower(tag.(string)))
			}
			monitor["tags"] = lowered
		}
		if regions, ok := body["regions"]; ok {
			monitor["probe_regions"] = regions
		}
		if paused, ok := body["paused"].(bool); ok {
			monitor["is_active"] = !paused
		}
		writeJSON(w, 200, map[string]any{"action": action, "monitor": monitor})
	case route == "GET automation/monitors" && len(parts) == 3:
		if monitor := f.monitorByKey(parts[2]); monitor != nil {
			writeJSON(w, 200, monitor)
			return
		}
		notFound(w)
	case route == "GET monitors" && len(parts) == 2:
		if monitor := f.monitors[parts[1]]; monitor != nil {
			// The server adds an owner-managed key the provider must ignore.
			copied := map[string]any{}
			for k, v := range monitor {
				copied[k] = v
			}
			config := map[string]any{"notification_emails": []string{"ops@example.com"}}
			if existing, ok := monitor["config"].(map[string]any); ok {
				for k, v := range existing {
					config[k] = v
				}
			}
			copied["config"] = config
			writeJSON(w, 200, copied)
			return
		}
		notFound(w)
	case route == "DELETE monitors":
		delete(f.monitors, parts[1])
		w.WriteHeader(http.StatusNoContent)

	case route == "POST status/pages" && len(parts) == 2:
		page := map[string]any{"id": f.id(), "title": body["title"], "slug": "generated", "is_public": true, "show_response_times": false, "is_whitelabel": false, "monitors": []any{}}
		if v, ok := body["description"]; ok {
			page["description"] = v
		}
		if v, ok := body["is_public"]; ok {
			page["is_public"] = v
		}
		f.pages[page["id"].(string)] = page
		writeJSON(w, 201, page)
	case route == "PATCH status/pages":
		page := f.pages[parts[2]]
		for k, v := range body {
			if k == "hide_powered_by" {
				page["is_whitelabel"] = v
			} else {
				page[k] = v
			}
		}
		writeJSON(w, 200, page)
	case route == "PUT status/pages" && len(parts) == 4:
		page := f.pages[parts[2]]
		list := []any{}
		for _, item := range body["monitors"].([]any) {
			entry := item.(map[string]any)
			list = append(list, map[string]any{"id": entry["monitor_id"], "name": "m", "section": entry["section"]})
		}
		page["monitors"] = list
		writeJSON(w, 200, page)
	case route == "GET status/pages":
		if page := f.pages[parts[2]]; page != nil {
			writeJSON(w, 200, page)
			return
		}
		notFound(w)
	case route == "DELETE status/pages":
		delete(f.pages, parts[2])
		writeJSON(w, 200, map[string]any{"success": true})

	case route == "GET maintenance" && len(parts) == 1:
		writeJSON(w, 200, f.maintenance)
	case route == "GET settings" && len(parts) == 2 && parts[1] == "escalation-policies":
		writeJSON(w, 200, f.escalation)
	// API keys are never workspace owners: the real API refuses these writes.
	case prefix == "maintenance" || (prefix == "settings" && len(parts) > 1 && parts[1] == "escalation-policies"):
		writeJSON(w, http.StatusForbidden, map[string]any{"error": "Workspace owner permission required", "code": "WORKSPACE_OWNER_REQUIRED"})

	case route == "GET integrations":
		list := []any{}
		for _, connection := range f.connections {
			list = append(list, connection)
		}
		writeJSON(w, 200, map[string]any{"connections": list})
	case r.Method == "POST" && len(parts) == 3 && parts[0] == "integrations" && parts[2] == "connections":
		id := f.id()
		f.connections[id] = map[string]any{"id": id, "integration_type": parts[1], "name": body["name"], "status": "connected", "config": map[string]any{"webhook_url": "https://hooks.example.com/…1234"}, "routing": body["routing"]}
		writeJSON(w, 200, map[string]any{"connection_id": id, "signing_secret": "whsec_test"})
	case r.Method == "PUT" && len(parts) == 3 && parts[1] == "connections":
		connection := f.connections[parts[2]]
		connection["name"] = body["name"]
		connection["routing"] = body["routing"]
		writeJSON(w, 200, map[string]any{"connection_id": parts[2]})
	case r.Method == "DELETE" && len(parts) == 3 && parts[1] == "connections":
		delete(f.connections, parts[2])
		writeJSON(w, 200, map[string]any{"success": true})
	default:
		writeJSON(w, http.StatusNotFound, map[string]any{"error": "no fake route for " + r.Method + " " + r.URL.Path})
	}
}

func TestFakeAPILifecycle(t *testing.T) {
	api := newFakeAPI()
	server := httptest.NewServer(api)
	defer server.Close()
	t.Setenv("SUTRAMX_API_URL", server.URL)
	t.Setenv("SUTRAMX_API_KEY", "sk_test")

	resource.UnitTest(t, resource.TestCase{
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		Steps: []resource.TestStep{
			{
				Config: `
resource "sutramx_monitor" "web" {
  key         = "web/home"
  name        = "Website"
  url         = "https://Example.com/Path"
  tags        = ["prod"]
  regions     = ["bom", "sin"]
  config_json = jsonencode({ timeout = 10000, expected_status_codes = [200] })
}

resource "sutramx_monitor" "generated" {
  name = "No key"
  url  = "https://example.org"
}

resource "sutramx_status_page" "status" {
  title    = "Status"
  slug     = "acme-status"
  monitors = [{ monitor_id = sutramx_monitor.web.id, section = "Web" }]
}

resource "sutramx_alert_channel" "ops" {
  type          = "webhook"
  name          = "Ops"
  config        = { webhook_url = "https://hooks.example.com/abcd1234" }
  routing_scope = "monitors"
  monitor_ids   = [sutramx_monitor.web.id]
}
`,
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttr("sutramx_monitor.web", "key", "web/home"),
					resource.TestCheckResourceAttr("sutramx_monitor.web", "paused", "false"),
					resource.TestCheckResourceAttr("sutramx_monitor.web", "interval_seconds", "120"),
					resource.TestMatchResourceAttr("sutramx_monitor.generated", "key", regexp.MustCompile(`^tf-[0-9a-f]{12}$`)),
					resource.TestCheckResourceAttr("sutramx_status_page.status", "slug", "acme-status"),
					resource.TestCheckResourceAttr("sutramx_status_page.status", "monitors.0.section", "Web"),
					resource.TestCheckResourceAttr("sutramx_alert_channel.ops", "signing_secret", "whsec_test"),
					resource.TestCheckResourceAttr("sutramx_alert_channel.ops", "status", "connected"),
				),
			},
			// Same config again: no diff (config_json formatting, server-added keys, URL host case).
			{
				Config: `
resource "sutramx_monitor" "web" {
  key         = "web/home"
  name        = "Website"
  url         = "https://Example.com/Path"
  tags        = ["prod"]
  regions     = ["bom", "sin"]
  config_json = jsonencode({ expected_status_codes = [200], timeout = 10000 })
}

resource "sutramx_monitor" "generated" {
  name = "No key"
  url  = "https://example.org"
}

resource "sutramx_status_page" "status" {
  title    = "Status"
  slug     = "acme-status"
  monitors = [{ monitor_id = sutramx_monitor.web.id, section = "Web" }]
}

resource "sutramx_alert_channel" "ops" {
  type          = "webhook"
  name          = "Ops"
  config        = { webhook_url = "https://hooks.example.com/abcd1234" }
  routing_scope = "monitors"
  monitor_ids   = [sutramx_monitor.web.id]
}
`,
				PlanOnly: true,
			},
			{
				ResourceName:            "sutramx_monitor.web",
				ImportState:             true,
				ImportStateId:           "web/home",
				ImportStateVerify:       true,
				ImportStateVerifyIgnore: []string{"config_json"},
			},
			// Update in place: rename, pause, re-key, drop regions back to the plan default.
			{
				Config: `
resource "sutramx_monitor" "web" {
  key         = "web/homepage"
  name        = "Website (renamed)"
  url         = "https://example.com/Path"
  tags        = ["prod", "web"]
  paused      = true
  config_json = jsonencode({ timeout = 5000 })
}

resource "sutramx_monitor" "generated" {
  name = "No key"
  url  = "https://example.org"
}
`,
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttr("sutramx_monitor.web", "key", "web/homepage"),
					resource.TestCheckResourceAttr("sutramx_monitor.web", "paused", "true"),
					resource.TestCheckNoResourceAttr("sutramx_monitor.web", "regions"),
					resource.TestCheckResourceAttr("sutramx_monitor.web", "tags.#", "2"),
				),
			},
		},
		CheckDestroy: func(_ *terraform.State) error {
			api.mu.Lock()
			defer api.mu.Unlock()
			if len(api.monitors) != 0 || len(api.pages) != 0 || len(api.connections) != 0 {
				return fmt.Errorf("resources left after destroy: %d monitors, %d pages, %d channels", len(api.monitors), len(api.pages), len(api.connections))
			}
			return nil
		},
	})

	// A plain apply must not have created duplicate monitors for the re-key.
	if count := len(api.monitors); count != 0 {
		t.Fatalf("expected every monitor destroyed, %d left", count)
	}
}

// A dashboard monitor (no key) imported by id: the plan after import is
// empty, and the first update gives it a key without an "inconsistent result".
func TestFakeAPIImportUnkeyedMonitor(t *testing.T) {
	api := newFakeAPI()
	api.monitors["00000000-0000-4000-8000-000000000999"] = map[string]any{
		"id": "00000000-0000-4000-8000-000000000999", "external_id": nil, "name": "Dashboard", "type": "http",
		"url": "https://example.com", "interval_seconds": 120, "is_active": true,
		"config": map[string]any{"timeout": 7000}, "tags": []string{"dash"}, "probe_regions": nil,
	}
	server := httptest.NewServer(api)
	defer server.Close()
	t.Setenv("SUTRAMX_API_URL", server.URL)
	t.Setenv("SUTRAMX_API_KEY", "sk_test")

	config := func(interval int) string {
		return fmt.Sprintf(`
resource "sutramx_monitor" "dash" {
  name             = "Dashboard"
  url              = "https://example.com"
  interval_seconds = %d
  tags             = ["dash"]
  config_json      = jsonencode({ timeout = 7000 })
}
`, interval)
	}
	resource.UnitTest(t, resource.TestCase{
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		Steps: []resource.TestStep{
			{
				Config:             config(120),
				ResourceName:       "sutramx_monitor.dash",
				ImportState:        true,
				ImportStateId:      "00000000-0000-4000-8000-000000000999",
				ImportStatePersist: true,
			},
			{Config: config(120), PlanOnly: true},
			{
				Config: config(300),
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttr("sutramx_monitor.dash", "interval_seconds", "300"),
					resource.TestCheckResourceAttr("sutramx_monitor.dash", "key", "tf-00000000"),
				),
			},
		},
	})
}
