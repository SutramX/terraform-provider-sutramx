package provider

import (
	"context"
	"fmt"
	"net/http/httptest"
	"regexp"
	"testing"

	"github.com/hashicorp/terraform-plugin-go/tfprotov6"
	"github.com/hashicorp/terraform-plugin-testing/helper/resource"
	"github.com/hashicorp/terraform-plugin-testing/terraform"
)

const (
	testGroupA = "00000000-0000-4000-8000-00000000a001"
	testGroupB = "00000000-0000-4000-8000-00000000a002"
)

func alertChannelConfig(routing string) string {
	return fmt.Sprintf(`
resource "sutramx_monitor" "web" {
  key  = "web/home"
  name = "Website"
  url  = "https://example.com"
}

resource "sutramx_alert_channel" "ops" {
  type   = "webhook"
  name   = "Ops"
  config = { webhook_url = "https://hooks.example.com/abcd1234" }
%s
}
`, routing)
}

// routing_scope = "groups" is written, read back and imported as groups,
// and switching between scopes updates the channel in place.
func TestFakeAPIAlertChannelGroupRouting(t *testing.T) {
	api := newFakeAPI()
	server := httptest.NewServer(api)
	defer server.Close()
	t.Setenv("SUTRAMX_API_URL", server.URL)
	t.Setenv("SUTRAMX_API_KEY", "sk_test")

	groups := alertChannelConfig(fmt.Sprintf(`  routing_scope = "groups"
  group_ids     = [%q, %q]`, testGroupA, testGroupB))
	routing := func() map[string]any {
		api.mu.Lock()
		defer api.mu.Unlock()
		for _, connection := range api.connections {
			return connection["routing"].(map[string]any)
		}
		return nil
	}
	resource.UnitTest(t, resource.TestCase{
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		Steps: []resource.TestStep{
			{
				Config: groups,
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttr("sutramx_alert_channel.ops", "routing_scope", "groups"),
					resource.TestCheckResourceAttr("sutramx_alert_channel.ops", "group_ids.#", "2"),
					resource.TestCheckTypeSetElemAttr("sutramx_alert_channel.ops", "group_ids.*", testGroupA),
					resource.TestCheckNoResourceAttr("sutramx_alert_channel.ops", "monitor_ids"),
					func(_ *terraform.State) error {
						got := routing()
						if got["scope"] != "groups" || len(got["group_ids"].([]any)) != 2 {
							return fmt.Errorf("stored routing = %v, want groups with 2 group ids", got)
						}
						return nil
					},
				),
			},
			{Config: groups, PlanOnly: true},
			{
				Config:                  groups,
				ResourceName:            "sutramx_alert_channel.ops",
				ImportState:             true,
				ImportStateVerify:       true,
				ImportStateVerifyIgnore: []string{"config", "signing_secret"},
			},
			{
				Config: alertChannelConfig(`  routing_scope = "monitors"
  monitor_ids   = [sutramx_monitor.web.id]`),
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttr("sutramx_alert_channel.ops", "routing_scope", "monitors"),
					resource.TestCheckResourceAttr("sutramx_alert_channel.ops", "monitor_ids.#", "1"),
					resource.TestCheckNoResourceAttr("sutramx_alert_channel.ops", "group_ids"),
				),
			},
			{
				Config: alertChannelConfig(""),
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttr("sutramx_alert_channel.ops", "routing_scope", "all"),
					resource.TestCheckNoResourceAttr("sutramx_alert_channel.ops", "group_ids"),
					resource.TestCheckNoResourceAttr("sutramx_alert_channel.ops", "monitor_ids"),
					func(_ *terraform.State) error {
						if got := routing(); got["scope"] != "all" {
							return fmt.Errorf("stored routing = %v, want all", got)
						}
						return nil
					},
				),
			},
		},
	})
}

// Group routing set in the dashboard on a channel Terraform routes to every
// monitor is drift: the plan shows it (it used to be read back as "all", so
// the next apply overwrote it silently), and apply restores the configuration.
func TestFakeAPIAlertChannelGroupRoutingDrift(t *testing.T) {
	api := newFakeAPI()
	server := httptest.NewServer(api)
	defer server.Close()
	t.Setenv("SUTRAMX_API_URL", server.URL)
	t.Setenv("SUTRAMX_API_KEY", "sk_test")

	config := alertChannelConfig("")
	resource.UnitTest(t, resource.TestCase{
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		Steps: []resource.TestStep{
			{Config: config},
			{
				PreConfig: func() {
					api.mu.Lock()
					defer api.mu.Unlock()
					for _, connection := range api.connections {
						connection["routing"] = fakeRouting(map[string]any{"scope": "groups", "group_ids": []any{testGroupA}})
					}
				},
				Config:             config,
				PlanOnly:           true,
				ExpectNonEmptyPlan: true,
			},
			{
				Config: config,
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttr("sutramx_alert_channel.ops", "routing_scope", "all"),
					resource.TestCheckNoResourceAttr("sutramx_alert_channel.ops", "group_ids"),
				),
			},
			{Config: config, PlanOnly: true},
		},
	})
}

// A channel routed by groups in the dashboard imports with its groups, so a
// configuration describing it plans no changes.
func TestFakeAPIImportGroupRoutedChannel(t *testing.T) {
	api := newFakeAPI()
	api.connections["00000000-0000-4000-8000-000000000301"] = map[string]any{
		"id": "00000000-0000-4000-8000-000000000301", "integration_type": "slack", "name": "Team", "status": "connected",
		"config":  map[string]any{"webhook_url": "https://hooks.slack.com/…1234"},
		"routing": fakeRouting(map[string]any{"scope": "groups", "group_ids": []any{testGroupB, testGroupA}}),
	}
	server := httptest.NewServer(api)
	defer server.Close()
	t.Setenv("SUTRAMX_API_URL", server.URL)
	t.Setenv("SUTRAMX_API_KEY", "sk_test")

	config := fmt.Sprintf(`
resource "sutramx_alert_channel" "team" {
  type          = "slack"
  name          = "Team"
  config        = { webhook_url = "https://hooks.slack.com/services/T0/B0/x" }
  routing_scope = "groups"
  group_ids     = [%q, %q]
}
`, testGroupA, testGroupB)
	resource.UnitTest(t, resource.TestCase{
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		Steps: []resource.TestStep{
			{
				Config:             config,
				ResourceName:       "sutramx_alert_channel.team",
				ImportState:        true,
				ImportStateId:      "00000000-0000-4000-8000-000000000301",
				ImportStatePersist: true,
				ImportStateCheck: func(states []*terraform.InstanceState) error {
					if len(states) != 1 || states[0].Attributes["routing_scope"] != "groups" || states[0].Attributes["group_ids.#"] != "2" {
						return fmt.Errorf("imported routing: %v", states[0].Attributes)
					}
					return nil
				},
			},
			// Only config (never read back) differs from the import: the
			// plan writes it and nothing else.
			{Config: config},
			{Config: config, PlanOnly: true},
		},
	})
}

// The id list must match routing_scope; this is caught at plan time instead
// of an "inconsistent result" after apply.
func TestAlertChannelRoutingValidation(t *testing.T) {
	api := newFakeAPI()
	server := httptest.NewServer(api)
	defer server.Close()
	t.Setenv("SUTRAMX_API_URL", server.URL)
	t.Setenv("SUTRAMX_API_KEY", "sk_test")

	cases := []struct {
		name    string
		routing string
		err     string
	}{
		{"groups without group_ids", `routing_scope = "groups"`, `group_ids is required when routing_scope = "groups"`},
		{"monitors without monitor_ids", `routing_scope = "monitors"`, `monitor_ids is required when routing_scope = "monitors"`},
		{"group_ids with default scope", fmt.Sprintf(`group_ids = [%q]`, testGroupA), `group_ids can only be set when routing_scope = "groups"`},
		{"monitor_ids with groups scope", fmt.Sprintf(`routing_scope = "groups"
  group_ids     = [%q]
  monitor_ids   = [%q]`, testGroupA, testGroupB), `monitor_ids can only be set when routing_scope = "monitors"`},
		{"group id not a UUID", `routing_scope = "groups"
  group_ids     = ["backend"]`, `monitor group ids are UUIDs`},
		{"unknown scope", `routing_scope = "tags"`, `value must be one of`},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			resource.UnitTest(t, resource.TestCase{
				ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
				Steps: []resource.TestStep{{
					Config: fmt.Sprintf(`
resource "sutramx_alert_channel" "ops" {
  type   = "webhook"
  name   = "Ops"
  config = { webhook_url = "https://hooks.example.com/abcd1234" }
  %s
}
`, tc.routing),
					PlanOnly:    true,
					ExpectError: regexp.MustCompile(regexp.QuoteMeta(tc.err)),
				}},
			})
		})
	}
}

// group_ids is a new optional attribute: state written by earlier versions
// (without it) upgrades without a schema version bump.
func TestAlertChannelStateWithoutGroupIDsUpgrades(t *testing.T) {
	ctx := context.Background()
	server, err := testAccProtoV6ProviderFactories["sutramx"]()
	if err != nil {
		t.Fatal(err)
	}
	if _, err := server.GetProviderSchema(ctx, &tfprotov6.GetProviderSchemaRequest{}); err != nil {
		t.Fatal(err)
	}
	resp, err := server.UpgradeResourceState(ctx, &tfprotov6.UpgradeResourceStateRequest{
		TypeName: "sutramx_alert_channel",
		Version:  0,
		RawState: &tfprotov6.RawState{JSON: []byte(`{"id":"00000000-0000-4000-8000-000000000001","type":"webhook","name":"Ops",` +
			`"config":{"webhook_url":"https://hooks.example.com/x"},"routing_scope":"monitors",` +
			`"monitor_ids":["00000000-0000-4000-8000-000000000002"],"status":"connected","signing_secret":null}`)},
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
