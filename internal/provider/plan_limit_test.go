package provider

import (
	"errors"
	"net/http/httptest"
	"regexp"
	"strings"
	"testing"

	"github.com/hashicorp/terraform-plugin-framework/diag"
	"github.com/hashicorp/terraform-plugin-testing/helper/resource"
	"github.com/hashicorp/terraform-plugin-testing/terraform"

	"github.com/sutramx/terraform-provider-sutramx/internal/client"
)

func TestAPIErrorDiagPlanLimit(t *testing.T) {
	var diags diag.Diagnostics
	apiErrorDiag(&diags, "Could not save the monitor", &client.Error{Status: 403, Code: "MONITOR_TOTAL_LIMIT_REACHED", Message: "Limit reached"})
	if len(diags) != 1 || !strings.Contains(diags[0].Summary(), "plan limit reached") || !strings.Contains(diags[0].Detail(), "will not help") {
		t.Fatalf("%+v", diags)
	}
	diags = nil
	apiErrorDiag(&diags, "Could not save the monitor", errors.New("boom"))
	if diags[0].Summary() != "Could not save the monitor" {
		t.Fatalf("%+v", diags)
	}
	diags = nil
	addAPIWarnings(&diags, []string{"Ignored unknown field: foo"})
	if diags.WarningsCount() != 1 || diags.HasError() {
		t.Fatalf("%+v", diags)
	}
}

// A plan limit fails apply with a clear message (not retried), writes with
// warnings still apply, and every POST carries an Idempotency-Key.
func TestFakeAPIPlanLimitWarningsAndIdempotencyKeys(t *testing.T) {
	api := newFakeAPI()
	api.monitorLimit = 1
	api.writeWarnings = []string{"Ignored unknown field: foo"}
	server := httptest.NewServer(api)
	defer server.Close()
	t.Setenv("SUTRAMX_API_URL", server.URL)
	t.Setenv("SUTRAMX_API_KEY", "sk_test")

	resource.UnitTest(t, resource.TestCase{
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		Steps: []resource.TestStep{
			{
				Config: `
resource "sutramx_monitor" "one" {
  key  = "one"
  name = "One"
  url  = "https://example.com"
}

resource "sutramx_status_page" "page" {
  title = "Status"
}
`,
				Check: func(*terraform.State) error {
					api.mu.Lock()
					defer api.mu.Unlock()
					if len(api.idempotencyKeys) != 1 || !strings.HasPrefix(api.idempotencyKeys[0], "tf-") {
						return errors.New("status page POST without Idempotency-Key: " + strings.Join(api.idempotencyKeys, ","))
					}
					return nil
				},
			},
			{
				Config: `
resource "sutramx_monitor" "one" {
  key  = "one"
  name = "One"
  url  = "https://example.com"
}

resource "sutramx_monitor" "two" {
  key  = "two"
  name = "Two"
  url  = "https://example.org"
}

resource "sutramx_status_page" "page" {
  title = "Status"
}
`,
				ExpectError: regexp.MustCompile(`(?s)plan limit reached.*ENTITLEMENT_LIMIT_REACHED.*will\s+not\s+help`),
			},
		},
	})
}
