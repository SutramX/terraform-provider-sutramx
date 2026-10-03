package provider

import (
	"context"
	"encoding/json"
	"net/http/httptest"
	"testing"

	"github.com/hashicorp/terraform-plugin-framework/diag"
	"github.com/hashicorp/terraform-plugin-framework/types"
	"github.com/hashicorp/terraform-plugin-go/tfprotov6"
	"github.com/hashicorp/terraform-plugin-testing/helper/resource"

	"github.com/sutramx/terraform-provider-sutramx/internal/client"
)

// Resources made outside Terraform (dashboard, sutramx.yml) and then
// imported: a configuration that describes them as they are must plan no
// changes. The fake API returns them the way the server stores them: config
// keys in insertion order (nested too), regions in stored order,
// server-managed config keys, status page monitors with sections.
func TestFakeAPIImportPlansNoChanges(t *testing.T) {
	api := newFakeAPI()
	api.monitors["00000000-0000-4000-8000-000000000101"] = map[string]any{
		"id": "00000000-0000-4000-8000-000000000101", "external_id": "api/health", "name": "API health", "type": "api",
		"url": "https://api.example.com/health", "interval_seconds": 60, "is_active": true,
		"config":        json.RawMessage(`{"timeout":7000,"headers":{"X-Trace":"1","Accept":"application/json"},"expected_status_codes":[200,204]}`),
		"tags":          []string{"prod", "api"},
		"probe_regions": []string{"sin", "bom", "fra"},
	}
	// A dashboard monitor (no key) whose configuration omits config_json and
	// tags (unmanaged) and regions (plan default: none stored).
	api.monitors["00000000-0000-4000-8000-000000000102"] = map[string]any{
		"id": "00000000-0000-4000-8000-000000000102", "external_id": nil, "name": "Dashboard", "type": "http",
		"url": "https://example.com", "interval_seconds": 300, "is_active": false,
		"config": json.RawMessage(`{"timeout":10000,"follow_redirects":true}`), "tags": []string{}, "probe_regions": nil,
	}
	api.monitors["00000000-0000-4000-8000-000000000103"] = map[string]any{
		"id": "00000000-0000-4000-8000-000000000103", "external_id": "db", "name": "DB", "type": "port",
		"url": nil, "interval_seconds": 120, "is_active": true,
		"config": json.RawMessage(`{"port":5432,"host":"db.example.com"}`), "tags": []string{}, "probe_regions": nil,
	}
	api.pages["00000000-0000-4000-8000-000000000201"] = map[string]any{
		"id": "00000000-0000-4000-8000-000000000201", "title": "Acme status", "slug": "acme", "description": "All systems",
		"is_public": true, "show_response_times": true, "is_whitelabel": false, "accent_color": "#0d9488",
		"monitors": []any{
			map[string]any{"id": "00000000-0000-4000-8000-000000000101", "name": "API health", "section": "API"},
			map[string]any{"id": "00000000-0000-4000-8000-000000000102", "name": "Dashboard", "section": nil},
		},
	}
	api.pages["00000000-0000-4000-8000-000000000202"] = map[string]any{
		"id": "00000000-0000-4000-8000-000000000202", "title": "Internal", "slug": "internal",
		"is_public": false, "show_response_times": false, "is_whitelabel": false,
		"monitors": []any{map[string]any{"id": "00000000-0000-4000-8000-000000000103", "name": "DB", "section": "Data"}},
	}
	api.connections["00000000-0000-4000-8000-000000000301"] = map[string]any{
		"id": "00000000-0000-4000-8000-000000000301", "integration_type": "webhook", "name": "Ops", "status": "connected",
		"config":  map[string]any{"webhook_url": "https://hooks.example.com/…1234"},
		"routing": map[string]any{"scope": "monitors", "monitor_ids": []string{"00000000-0000-4000-8000-000000000101"}},
	}
	server := httptest.NewServer(api)
	defer server.Close()
	t.Setenv("SUTRAMX_API_URL", server.URL)
	t.Setenv("SUTRAMX_API_KEY", "sk_test")

	const config = `
resource "sutramx_monitor" "api" {
  key              = "api/health"
  name             = "API health"
  type             = "api"
  url              = "https://api.example.com/health"
  interval_seconds = 60
  tags             = ["api", "prod"]
  regions          = ["bom", "fra", "sin"]
  config_json = jsonencode({
    timeout               = 7000
    expected_status_codes = [200, 204]
    headers               = { Accept = "application/json", X-Trace = "1" }
  })
}

# config_json and tags left out: not managed.
resource "sutramx_monitor" "dash" {
  name   = "Dashboard"
  url    = "https://example.com"
  paused = true
}

resource "sutramx_monitor" "db" {
  key         = "db"
  name        = "DB"
  type        = "port"
  config_json = jsonencode({ host = "db.example.com", port = 5432 })
}

resource "sutramx_status_page" "public" {
  title               = "Acme status"
  slug                = "acme"
  description         = "All systems"
  accent_color        = "#0d9488"
  show_response_times = true
  monitors = [
    { monitor_id = sutramx_monitor.api.id, section = "API" },
    { monitor_id = sutramx_monitor.dash.id },
  ]
}

# monitors left out: managed in the dashboard.
resource "sutramx_status_page" "internal" {
  title     = "Internal"
  is_public = false
}
`
	importStep := func(address, id string) resource.TestStep {
		return resource.TestStep{Config: config, ResourceName: address, ImportState: true, ImportStateId: id, ImportStatePersist: true}
	}
	resource.UnitTest(t, resource.TestCase{
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		Steps: []resource.TestStep{
			importStep("sutramx_monitor.api", "api/health"),
			importStep("sutramx_monitor.dash", "00000000-0000-4000-8000-000000000102"),
			importStep("sutramx_monitor.db", "00000000-0000-4000-8000-000000000103"),
			importStep("sutramx_status_page.public", "00000000-0000-4000-8000-000000000201"),
			importStep("sutramx_status_page.internal", "00000000-0000-4000-8000-000000000202"),
			{Config: config, PlanOnly: true},
		},
	})
}

// Created by Terraform, then imported again: the imported state equals the
// state the apply wrote, except write-only values the API never returns
// (alert channel config, the webhook signing secret).
func TestFakeAPIImportStateVerify(t *testing.T) {
	api := newFakeAPI()
	server := httptest.NewServer(api)
	defer server.Close()
	t.Setenv("SUTRAMX_API_URL", server.URL)
	t.Setenv("SUTRAMX_API_KEY", "sk_test")

	const config = `
resource "sutramx_monitor" "web" {
  key         = "web/home"
  name        = "Website"
  url         = "https://example.com/"
  tags        = ["prod"]
  regions     = ["sin", "bom"]
  config_json = jsonencode({ timeout = 10000, headers = { b = "2", a = "1" } })
}

resource "sutramx_monitor" "bare" {
  name = "Bare"
  url  = "https://example.org"
}

resource "sutramx_status_page" "status" {
  title    = "Status"
  slug     = "acme-status"
  monitors = [{ monitor_id = sutramx_monitor.web.id, section = "Web" }, { monitor_id = sutramx_monitor.bare.id }]
}

resource "sutramx_status_page" "unmanaged" {
  title = "Unmanaged monitors"
}

resource "sutramx_alert_channel" "ops" {
  type          = "webhook"
  name          = "Ops"
  config        = { webhook_url = "https://hooks.example.com/abcd1234" }
  routing_scope = "monitors"
  monitor_ids   = [sutramx_monitor.web.id]
}
`
	verify := func(address string, ignore ...string) resource.TestStep {
		return resource.TestStep{Config: config, ResourceName: address, ImportState: true, ImportStateVerify: true, ImportStateVerifyIgnore: ignore}
	}
	resource.UnitTest(t, resource.TestCase{
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		Steps: []resource.TestStep{
			{Config: config},
			{Config: config, PlanOnly: true},
			verify("sutramx_monitor.web"),
			verify("sutramx_monitor.bare"),
			verify("sutramx_status_page.status"),
			verify("sutramx_status_page.unmanaged"),
			verify("sutramx_alert_channel.ops", "config", "signing_secret"),
		},
	})
}

// Read stores config_json in the form jsonencode() produces (keys sorted at
// every level, no whitespace), so a configuration using jsonencode matches
// an imported monitor whatever order the server keeps the keys in.
func TestApplyMonitorNormalizesConfig(t *testing.T) {
	ctx := context.Background()
	var diags diag.Diagnostics
	model := monitorModel{ConfigJSON: types.StringNull(), Regions: types.SetNull(types.StringType)}
	applyMonitor(ctx, client.Monitor{
		ID:           "id",
		Name:         "n",
		Type:         "http",
		Config:       json.RawMessage(`{"timeout": 5000, "headers": {"X-B": "<b>", "X-A": "a"}, "notification_emails": ["a@b.c"]}`),
		ProbeRegions: []string{"sin", "bom"},
	}, &model, &diags)
	if diags.HasError() {
		t.Fatal(diags)
	}
	if got, want := model.ConfigJSON.ValueString(), `{"headers":{"X-A":"a","X-B":"\u003cb\u003e"},"timeout":5000}`; got != want {
		t.Fatalf("config_json = %s, want %s", got, want)
	}
	var regions []string
	model.Regions.ElementsAs(ctx, &regions, false)
	if len(regions) != 2 {
		t.Fatalf("regions = %v", regions)
	}
	// A configured value with another spelling of the same JSON is kept.
	model.ConfigJSON = types.StringValue(`{ "timeout": 5000, "headers": { "X-A": "a", "X-B": "<b>" } }`)
	applyMonitor(ctx, client.Monitor{ID: "id", Config: json.RawMessage(`{"timeout":5000,"headers":{"X-B":"<b>","X-A":"a"}}`)}, &model, &diags)
	if got := model.ConfigJSON.ValueString(); got != `{ "timeout": 5000, "headers": { "X-A": "a", "X-B": "<b>" } }` {
		t.Fatalf("configured spelling not kept: %s", got)
	}
}

// regions changed from a list to a set (order never mattered to the API).
// Both are JSON arrays in the state file, so state written by earlier
// versions upgrades without a schema version bump.
func TestMonitorStateWithRegionsListUpgrades(t *testing.T) {
	ctx := context.Background()
	server, err := testAccProtoV6ProviderFactories["sutramx"]()
	if err != nil {
		t.Fatal(err)
	}
	if _, err := server.GetProviderSchema(ctx, &tfprotov6.GetProviderSchemaRequest{}); err != nil {
		t.Fatal(err)
	}
	resp, err := server.UpgradeResourceState(ctx, &tfprotov6.UpgradeResourceStateRequest{
		TypeName: "sutramx_monitor",
		Version:  0,
		RawState: &tfprotov6.RawState{JSON: []byte(`{"id":"00000000-0000-4000-8000-000000000001","key":"web","name":"Web","type":"http",` +
			`"url":"https://example.com","interval_seconds":60,"config_json":null,"tags":["prod"],"regions":["sin","bom"],` +
			`"paused":false,"heartbeat_url":null}`)},
	})
	if err != nil {
		t.Fatal(err)
	}
	for _, d := range resp.Diagnostics {
		if d.Severity == tfprotov6.DiagnosticSeverityError {
			t.Fatalf("upgrade: %s: %s", d.Summary, d.Detail)
		}
	}
	if resp.UpgradedState == nil {
		t.Fatal("no upgraded state")
	}
}
