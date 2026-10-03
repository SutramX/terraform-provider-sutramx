package provider

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http/httptest"
	"regexp"
	"strings"
	"testing"

	"github.com/hashicorp/terraform-plugin-framework/diag"
	"github.com/hashicorp/terraform-plugin-framework/types"
	"github.com/hashicorp/terraform-plugin-testing/helper/resource"
	"github.com/hashicorp/terraform-plugin-testing/terraform"

	"github.com/sutramx/terraform-provider-sutramx/internal/client"
)

// DNS and multi-step monitors are configured through config_json. The API
// rewrites parts of their config (DNS host name and record type, multi-step
// secrets sealed away and listed in secret_names); none of that may cause an
// "inconsistent result" after apply or a perpetual diff.
func TestFakeAPIDNSAndMultistepMonitors(t *testing.T) {
	api := newFakeAPI()
	server := httptest.NewServer(api)
	defer server.Close()
	t.Setenv("SUTRAMX_API_URL", server.URL)
	t.Setenv("SUTRAMX_API_KEY", "sk_test")

	config := func(token string) string {
		return fmt.Sprintf(`
resource "sutramx_monitor" "mx" {
  key  = "dns/mx"
  name = "MX records"
  type = "dns"
  config_json = jsonencode({
    hostname        = "Example.com."
    record_type     = "mx"
    dns_mode        = "expected"
    expected_values = ["10 mail.example.com"]
  })
}

resource "sutramx_monitor" "apex" {
  key  = "dns/apex"
  name = "Apex A record"
  type = "dns"
  config_json = jsonencode({ hostname = "example.com" })
}

resource "sutramx_monitor" "checkout" {
  key  = "api/checkout"
  name = "Checkout flow"
  type = "multistep"
  config_json = jsonencode({
    steps = [
      {
        name    = "Log in"
        method  = "POST"
        url     = "https://api.example.com/login"
        headers = { Authorization = "Bearer {{secrets.TOKEN}}" }
        extract = [{ name = "session", source = "json", expression = "$.session" }]
      },
      { name = "Cart", url = "https://api.example.com/cart?s={{session}}", expected_status_codes = [200] },
    ]
    secrets = { TOKEN = %q }
  })
}
`, token)
	}
	resource.UnitTest(t, resource.TestCase{
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		Steps: []resource.TestStep{
			{
				Config: config("first"),
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttr("sutramx_monitor.mx", "type", "dns"),
					resource.TestCheckResourceAttr("sutramx_monitor.checkout", "type", "multistep"),
					resource.TestMatchResourceAttr("sutramx_monitor.checkout", "config_json", regexp.MustCompile(`"secrets":\{"TOKEN":"first"\}`)),
				),
			},
			{Config: config("first"), PlanOnly: true},
			// A new secret value is a change (and is sent on apply).
			{Config: config("second"), PlanOnly: true, ExpectNonEmptyPlan: true},
			{
				Config: config("second"),
				Check: func(_ *terraform.State) error {
					api.mu.Lock()
					defer api.mu.Unlock()
					for _, monitor := range api.monitors {
						if monitor["type"] != "multistep" {
							continue
						}
						stored := monitor["config"].(map[string]any)
						if _, leaked := stored["secrets"]; leaked {
							return fmt.Errorf("fake API stored secrets: %v", stored)
						}
						if names := fmt.Sprint(stored["secret_names"]); names != "[TOKEN]" {
							return fmt.Errorf("secret_names = %s", names)
						}
					}
					return nil
				},
			},
			{Config: config("second"), PlanOnly: true},
		},
	})
}

func TestApplyMonitorKeepsWriteOnlyAndDNSSpelling(t *testing.T) {
	ctx := context.Background()
	apply := func(monitorType, configured, remote string) string {
		t.Helper()
		var diags diag.Diagnostics
		model := monitorModel{ConfigJSON: types.StringValue(configured), Regions: types.SetNull(types.StringType)}
		applyMonitor(ctx, client.Monitor{ID: "id", Type: monitorType, Config: json.RawMessage(remote)}, &model, &diags)
		if diags.HasError() {
			t.Fatal(diags)
		}
		return model.ConfigJSON.ValueString()
	}

	// secrets kept, secret_names (server-managed) ignored.
	configured := `{"steps":[{"url":"https://a.example/{{secrets.T}}"}],"secrets":{"T":"x"}}`
	if got := apply("multistep", configured, `{"steps":[{"url":"https://a.example/{{secrets.T}}"}],"secret_names":["T"]}`); got != configured {
		t.Fatalf("multistep config_json = %s", got)
	}
	// A step changed outside Terraform is still drift.
	if got := apply("multistep", configured, `{"steps":[{"url":"https://b.example/"}],"secret_names":["T"]}`); !strings.Contains(got, "b.example") || !strings.Contains(got, `"secrets":{"T":"x"}`) {
		t.Fatalf("multistep drift = %s", got)
	}
	// secret_names declared in the configuration is compared as usual.
	if got := apply("multistep", `{"steps":[],"secret_names":[]}`, `{"steps":[],"secret_names":["T"]}`); got != `{"secret_names":["T"],"steps":[]}` {
		t.Fatalf("declared secret_names = %s", got)
	}

	// DNS: host name case, trailing dot and record type case are the API's normalization.
	configured = `{"hostname":"Example.com.","record_type":"mx"}`
	if got := apply("dns", configured, `{"hostname":"example.com","record_type":"MX"}`); got != configured {
		t.Fatalf("dns config_json = %s", got)
	}
	// record_type left out: the stored default A is not a diff, another type is.
	if got := apply("dns", `{"hostname":"example.com"}`, `{"hostname":"example.com","record_type":"A"}`); got != `{"hostname":"example.com"}` {
		t.Fatalf("dns default record type = %s", got)
	}
	if got := apply("dns", `{"hostname":"example.com"}`, `{"hostname":"example.com","record_type":"TXT"}`); !strings.Contains(got, "TXT") {
		t.Fatalf("dns record type drift = %s", got)
	}
	// A different host name is drift.
	if got := apply("dns", configured, `{"hostname":"example.org","record_type":"MX"}`); !strings.Contains(got, "example.org") {
		t.Fatalf("dns hostname drift = %s", got)
	}
	// Only DNS monitors get the DNS spelling tolerance.
	if got := apply("http", `{"hostname":"Example.com"}`, `{"hostname":"example.com"}`); got != `{"hostname":"example.com"}` {
		t.Fatalf("http config_json = %s", got)
	}
}

// Tags are stored lower-case and trimmed: anything else is a plan-time
// validation error, never a perpetual diff.
func TestMonitorTagValidation(t *testing.T) {
	api := newFakeAPI()
	server := httptest.NewServer(api)
	defer server.Close()
	t.Setenv("SUTRAMX_API_URL", server.URL)
	t.Setenv("SUTRAMX_API_KEY", "sk_test")

	cases := map[string]string{
		"Prod":                  `has upper-case letters`,
		"ÉQUIPE":                `has upper-case letters`,
		" prod":                 `leading or trailing spaces`,
		"":                      `must be 1-32 characters`,
		strings.Repeat("a", 33): `must be 1-32 characters`,
	}
	for tag, want := range cases {
		t.Run(fmt.Sprintf("%q", tag), func(t *testing.T) {
			resource.UnitTest(t, resource.TestCase{
				ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
				Steps: []resource.TestStep{{
					Config: fmt.Sprintf(`
resource "sutramx_monitor" "web" {
  name = "Website"
  url  = "https://example.com"
  tags = [%q]
}
`, tag),
					PlanOnly:    true,
					ExpectError: regexp.MustCompile(regexp.QuoteMeta(want)),
				}},
			})
		})
	}
}
