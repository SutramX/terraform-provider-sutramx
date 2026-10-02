package provider

import (
	"net/http/httptest"
	"regexp"
	"testing"

	"github.com/hashicorp/terraform-plugin-testing/helper/resource"
)

func TestFakeAPIMaintenanceAndEscalationDataSources(t *testing.T) {
	api := newFakeAPI()
	api.maintenance = []any{
		map[string]any{
			"id": "00000000-0000-4000-8000-000000000101", "title": "DB upgrade", "description": "",
			"status": "scheduled", "effectiveStatus": "scheduled",
			"startTime": "2030-01-05T02:00:00.000Z", "endTime": "2030-01-05T03:00:00.000Z", "timezone": "Asia/Kolkata",
			"impact": "high", "scopeType": "monitor",
			"monitorIds": []string{"00000000-0000-4000-8000-000000000001"}, "groupIds": []string{},
			"monitorNames": []string{"API"}, "groupNames": []string{}, "affectedServices": []string{"api"},
			"recurrence": map[string]any{"type": "weekly", "weekdays": []int{0, 6}, "until": "2030-06-01T00:00:00.000Z"},
			"createdAt":  "2029-12-01T00:00:00.000Z", "updatedAt": "2029-12-01T00:00:00.000Z",
		},
		map[string]any{
			"id": "00000000-0000-4000-8000-000000000102", "title": "Old window", "description": "Done",
			"status": "scheduled", "effectiveStatus": "completed",
			"startTime": "2020-01-05T02:00:00.000Z", "endTime": "2020-01-05T03:00:00.000Z", "timezone": "UTC",
			"impact": "low", "scopeType": "global", "monitorIds": nil, "groupIds": nil, "affectedServices": nil,
			"recurrence": map[string]any{"type": "none", "weekdays": []int{}, "until": nil},
		},
	}
	api.escalation = []any{
		map[string]any{
			"id": "00000000-0000-4000-8000-000000000201", "name": "Primary", "is_enabled": true, "max_depth": 5,
			"created_at": "2029-12-01T00:00:00.000Z", "updated_at": "2029-12-01T00:00:00.000Z",
			"steps": []any{
				map[string]any{"id": "00000000-0000-4000-8000-000000000301", "step_order": 1, "delay_minutes": 0, "channel": "email", "target_type": "email", "target_value": "oncall@example.com", "target_label": "oncall@example.com"},
				map[string]any{"id": "00000000-0000-4000-8000-000000000302", "step_order": 2, "delay_minutes": 15, "channel": "slack", "target_type": "integration", "target_value": "00000000-0000-4000-8000-000000000401", "target_label": "Slack #ops"},
			},
		},
		map[string]any{"id": "00000000-0000-4000-8000-000000000202", "name": "Disabled", "is_enabled": false, "max_depth": 3, "steps": []any{}},
	}
	server := httptest.NewServer(api)
	defer server.Close()
	t.Setenv("SUTRAMX_API_URL", server.URL)
	t.Setenv("SUTRAMX_API_KEY", "sk_test")

	resource.UnitTest(t, resource.TestCase{
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		Steps: []resource.TestStep{
			{
				Config: `
data "sutramx_maintenance_windows" "all" {}
data "sutramx_maintenance_windows" "upcoming" {
  effective_status = "scheduled"
}
data "sutramx_escalation_policies" "all" {}
data "sutramx_escalation_policies" "primary" {
  name = "Primary"
}
`,
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttr("data.sutramx_maintenance_windows.all", "windows.#", "2"),
					resource.TestCheckResourceAttr("data.sutramx_maintenance_windows.all", "ids.1", "00000000-0000-4000-8000-000000000102"),
					resource.TestCheckResourceAttr("data.sutramx_maintenance_windows.all", "windows.1.scope_type", "global"),
					resource.TestCheckResourceAttr("data.sutramx_maintenance_windows.all", "windows.1.monitor_ids.#", "0"),
					resource.TestCheckResourceAttr("data.sutramx_maintenance_windows.all", "windows.1.description", "Done"),
					resource.TestCheckNoResourceAttr("data.sutramx_maintenance_windows.all", "windows.1.recurrence_until"),
					resource.TestCheckResourceAttr("data.sutramx_maintenance_windows.upcoming", "windows.#", "1"),
					resource.TestCheckResourceAttr("data.sutramx_maintenance_windows.upcoming", "windows.0.title", "DB upgrade"),
					resource.TestCheckNoResourceAttr("data.sutramx_maintenance_windows.upcoming", "windows.0.description"),
					resource.TestCheckResourceAttr("data.sutramx_maintenance_windows.upcoming", "windows.0.timezone", "Asia/Kolkata"),
					resource.TestCheckResourceAttr("data.sutramx_maintenance_windows.upcoming", "windows.0.monitor_ids.0", "00000000-0000-4000-8000-000000000001"),
					resource.TestCheckResourceAttr("data.sutramx_maintenance_windows.upcoming", "windows.0.affected_services.0", "api"),
					resource.TestCheckResourceAttr("data.sutramx_maintenance_windows.upcoming", "windows.0.recurrence_type", "weekly"),
					resource.TestCheckResourceAttr("data.sutramx_maintenance_windows.upcoming", "windows.0.recurrence_weekdays.#", "2"),
					resource.TestCheckResourceAttr("data.sutramx_maintenance_windows.upcoming", "windows.0.recurrence_weekdays.1", "6"),
					resource.TestCheckResourceAttr("data.sutramx_maintenance_windows.upcoming", "windows.0.recurrence_until", "2030-06-01T00:00:00.000Z"),

					resource.TestCheckResourceAttr("data.sutramx_escalation_policies.all", "policies.#", "2"),
					resource.TestCheckResourceAttr("data.sutramx_escalation_policies.all", "policies.1.enabled", "false"),
					resource.TestCheckResourceAttr("data.sutramx_escalation_policies.all", "policies.1.steps.#", "0"),
					resource.TestCheckResourceAttr("data.sutramx_escalation_policies.primary", "policies.#", "1"),
					resource.TestCheckResourceAttr("data.sutramx_escalation_policies.primary", "policies.0.id", "00000000-0000-4000-8000-000000000201"),
					resource.TestCheckResourceAttr("data.sutramx_escalation_policies.primary", "policies.0.max_depth", "5"),
					resource.TestCheckResourceAttr("data.sutramx_escalation_policies.primary", "policies.0.steps.#", "2"),
					resource.TestCheckResourceAttr("data.sutramx_escalation_policies.primary", "policies.0.steps.1.delay_minutes", "15"),
					resource.TestCheckResourceAttr("data.sutramx_escalation_policies.primary", "policies.0.steps.1.target_type", "integration"),
					resource.TestCheckResourceAttr("data.sutramx_escalation_policies.primary", "policies.0.steps.1.target_label", "Slack #ops"),
				),
			},
		},
	})
}

// Workspace data sources need a key (unlike regions and plans).
func TestWorkspaceDataSourcesNeedAPIKey(t *testing.T) {
	server := httptest.NewServer(newFakeAPI())
	defer server.Close()
	t.Setenv("SUTRAMX_API_URL", server.URL)
	t.Setenv("SUTRAMX_API_KEY", "")

	resource.UnitTest(t, resource.TestCase{
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		Steps: []resource.TestStep{
			{
				Config:      `data "sutramx_escalation_policies" "all" {}`,
				ExpectError: regexp.MustCompile(`Missing SutramX API key`),
			},
		},
	})
}

func TestMaintenanceWindowsStatusValidation(t *testing.T) {
	resource.UnitTest(t, resource.TestCase{
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		Steps: []resource.TestStep{
			{
				Config: `
data "sutramx_maintenance_windows" "bad" {
  effective_status = "active"
}
`,
				ExpectError: regexp.MustCompile(`value must be one of`),
			},
		},
	})
}
