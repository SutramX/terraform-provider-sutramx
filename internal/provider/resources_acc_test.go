package provider

import (
	"fmt"
	"os"
	"testing"

	"github.com/hashicorp/terraform-plugin-testing/helper/acctest"
	"github.com/hashicorp/terraform-plugin-testing/helper/resource"
)

// Acceptance tests: TF_ACC=1 SUTRAMX_API_KEY=sk_... go test ./internal/provider -run TestAcc -v
// They need a Terraform CLI (downloaded automatically, or TF_ACC_TERRAFORM_PATH)
// and a workspace whose plan allows a few monitors and one status page.

func TestAccMonitorResource(t *testing.T) {
	key := "tf-acc-" + acctest.RandString(8)
	resource.Test(t, resource.TestCase{
		PreCheck:                 func() { testAccPreCheck(t) },
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		Steps: []resource.TestStep{
			{
				Config: fmt.Sprintf(`
resource "sutramx_monitor" "test" {
  key              = %[1]q
  name             = "Acceptance %[1]s"
  url              = "https://example.com"
  interval_seconds = 300
  tags             = ["acc"]
  config_json      = jsonencode({ timeout = 10000 })
}
`, key),
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttr("sutramx_monitor.test", "key", key),
					resource.TestCheckResourceAttr("sutramx_monitor.test", "type", "http"),
					resource.TestCheckResourceAttr("sutramx_monitor.test", "paused", "false"),
					resource.TestCheckResourceAttrSet("sutramx_monitor.test", "id"),
				),
			},
			{
				ResourceName:            "sutramx_monitor.test",
				ImportState:             true,
				ImportStateId:           key,
				ImportStateVerify:       true,
				ImportStateVerifyIgnore: []string{"config_json"},
			},
			{
				Config: fmt.Sprintf(`
resource "sutramx_monitor" "test" {
  key              = %[1]q
  name             = "Acceptance %[1]s renamed"
  url              = "https://example.com/"
  interval_seconds = 600
  tags             = ["acc", "renamed"]
  paused           = true
  config_json      = jsonencode({ timeout = 15000 })
}
`, key),
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttr("sutramx_monitor.test", "name", "Acceptance "+key+" renamed"),
					resource.TestCheckResourceAttr("sutramx_monitor.test", "interval_seconds", "600"),
					resource.TestCheckResourceAttr("sutramx_monitor.test", "paused", "true"),
					resource.TestCheckResourceAttr("sutramx_monitor.test", "tags.#", "2"),
				),
			},
		},
	})
}

func TestAccStatusPageResource(t *testing.T) {
	suffix := acctest.RandString(8)
	resource.Test(t, resource.TestCase{
		PreCheck:                 func() { testAccPreCheck(t) },
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		Steps: []resource.TestStep{
			{
				Config: fmt.Sprintf(`
resource "sutramx_monitor" "page" {
  key  = "tf-acc-page-%[1]s"
  name = "Status page monitor %[1]s"
  url  = "https://example.com"
  interval_seconds = 300
}

resource "sutramx_status_page" "test" {
  title     = "Acceptance %[1]s"
  slug      = "tf-acc-%[1]s"
  is_public = false
  monitors = [
    { monitor_id = sutramx_monitor.page.id, section = "Web" },
  ]
}
`, suffix),
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttr("sutramx_status_page.test", "slug", "tf-acc-"+suffix),
					resource.TestCheckResourceAttr("sutramx_status_page.test", "monitors.#", "1"),
					resource.TestCheckResourceAttr("sutramx_status_page.test", "monitors.0.section", "Web"),
				),
			},
			{
				ResourceName:            "sutramx_status_page.test",
				ImportState:             true,
				ImportStateVerify:       true,
				ImportStateVerifyIgnore: []string{"monitors"},
			},
		},
	})
}

func TestAccAlertChannelResource(t *testing.T) {
	if os.Getenv("SUTRAMX_ACC_ALERT_CHANNELS") == "" || os.Getenv("SUTRAMX_ACC_WEBHOOK_URL") == "" {
		t.Skip("set SUTRAMX_ACC_ALERT_CHANNELS=1, SUTRAMX_ACC_WEBHOOK_URL and an automation-access key to test alert channels")
	}
	name := "tf-acc-" + acctest.RandString(6)
	resource.Test(t, resource.TestCase{
		PreCheck:                 func() { testAccPreCheck(t) },
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		Steps: []resource.TestStep{
			{
				Config: fmt.Sprintf(`
resource "sutramx_alert_channel" "test" {
  type   = "webhook"
  name   = %q
  config = { webhook_url = %q }
}
`, name, os.Getenv("SUTRAMX_ACC_WEBHOOK_URL")),
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttr("sutramx_alert_channel.test", "routing_scope", "all"),
					resource.TestCheckResourceAttrSet("sutramx_alert_channel.test", "signing_secret"),
				),
			},
			{
				ResourceName:            "sutramx_alert_channel.test",
				ImportState:             true,
				ImportStateVerify:       true,
				ImportStateVerifyIgnore: []string{"config", "signing_secret"},
			},
		},
	})
}

func TestAccDataSources(t *testing.T) {
	resource.Test(t, resource.TestCase{
		PreCheck:                 func() { testAccPreCheck(t) },
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		Steps: []resource.TestStep{
			{
				Config: `
data "sutramx_regions" "all" {}
data "sutramx_plans" "all" {}
data "sutramx_maintenance_windows" "all" {}
data "sutramx_escalation_policies" "all" {}
`,
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttrSet("data.sutramx_regions.all", "regions.0.code"),
					resource.TestCheckResourceAttrSet("data.sutramx_plans.all", "plans.0.id"),
					resource.TestCheckResourceAttrSet("data.sutramx_maintenance_windows.all", "windows.#"),
					resource.TestCheckResourceAttrSet("data.sutramx_escalation_policies.all", "policies.#"),
				),
			},
		},
	})
}
