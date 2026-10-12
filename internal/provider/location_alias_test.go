package provider

import (
	"fmt"
	"net/http/httptest"
	"testing"

	"github.com/hashicorp/terraform-plugin-testing/helper/resource"
	"github.com/hashicorp/terraform-plugin-testing/terraform"
)

func TestSameLocationsMapsRenamedCodes(t *testing.T) {
	cases := []struct {
		a, b []string
		want bool
	}{
		{[]string{"fra1", "usa-az-probe"}, []string{"usa-az", "fra1"}, true},
		{[]string{"usa-az"}, []string{"usa-az-probe"}, true},
		{[]string{"usa-az-probe", "usa-az"}, []string{"usa-az"}, true},
		{[]string{"fra1"}, []string{"usa-az"}, false},
		{[]string{"fra1", "usa-az-probe"}, []string{"fra1"}, false},
		{[]string{"constructor"}, []string{"constructor"}, true},
	}
	for _, c := range cases {
		if got := sameLocations(c.a, c.b); got != c.want {
			t.Errorf("sameLocations(%v, %v) = %v, want %v", c.a, c.b, got, c.want)
		}
	}
	if canonicalLocationCode("usa-az-probe") != "usa-az" || canonicalLocationCode("fra1") != "fra1" {
		t.Fatal("canonicalLocationCode")
	}
}

// A configuration that still names the Arizona location by its old code
// applies, keeps that spelling in state and plans no changes, even though
// the API stores and returns usa-az. Switching the configuration to usa-az
// takes one in-place update (the spelling in state) and then settles.
func TestFakeAPIRenamedLocationCodePlansNoChanges(t *testing.T) {
	api := newFakeAPI()
	server := httptest.NewServer(api)
	defer server.Close()
	t.Setenv("SUTRAMX_API_URL", server.URL)
	t.Setenv("SUTRAMX_API_KEY", "sk_test")

	config := func(regions string) string {
		return `
resource "sutramx_monitor" "az" {
  key     = "web/az"
  name    = "Arizona"
  url     = "https://example.com"
  regions = ` + regions + `
}
`
	}
	apiStoresCurrentCode := func(_ *terraform.State) error {
		api.mu.Lock()
		defer api.mu.Unlock()
		monitor := api.monitorByKey("web/az")
		if monitor == nil {
			return fmt.Errorf("monitor not stored")
		}
		if !sameRegionSet(monitor["probe_regions"], []string{"fra1", "usa-az"}) {
			return fmt.Errorf("API stored %v, want [fra1 usa-az]", monitor["probe_regions"])
		}
		return nil
	}

	resource.UnitTest(t, resource.TestCase{
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		Steps: []resource.TestStep{
			{
				Config: config(`["fra1", "usa-az-probe"]`),
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttr("sutramx_monitor.az", "regions.#", "2"),
					resource.TestCheckTypeSetElemAttr("sutramx_monitor.az", "regions.*", "usa-az-probe"),
					resource.TestCheckTypeSetElemAttr("sutramx_monitor.az", "regions.*", "fra1"),
					apiStoresCurrentCode,
				),
			},
			// Refresh + plan with the old code: no changes.
			{Config: config(`["fra1", "usa-az-probe"]`), PlanOnly: true},
			{
				Config: config(`["usa-az", "fra1"]`),
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckTypeSetElemAttr("sutramx_monitor.az", "regions.*", "usa-az"),
					apiStoresCurrentCode,
				),
			},
			{Config: config(`["usa-az", "fra1"]`), PlanOnly: true},
		},
	})
}
