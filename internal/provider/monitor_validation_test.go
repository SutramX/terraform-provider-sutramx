package provider

import (
	"context"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/hashicorp/terraform-plugin-framework/diag"
	"github.com/hashicorp/terraform-plugin-framework/types"
	"github.com/hashicorp/terraform-plugin-go/tfprotov6"
	"github.com/hashicorp/terraform-plugin-go/tftypes"

	"github.com/sutramx/terraform-provider-sutramx/internal/client"
)

// validateMonitorConfig runs the provider's config validation (attribute
// validators and ValidateConfig) for a sutramx_monitor configuration, at the
// protocol level so no Terraform CLI is needed. It returns the error details.
func validateMonitorConfig(t *testing.T, attrs map[string]tftypes.Value) []string {
	t.Helper()
	ctx := context.Background()
	server, err := testAccProtoV6ProviderFactories["sutramx"]()
	if err != nil {
		t.Fatal(err)
	}
	schemas, err := server.GetProviderSchema(ctx, &tfprotov6.GetProviderSchemaRequest{})
	if err != nil {
		t.Fatal(err)
	}
	objectType := schemas.ResourceSchemas["sutramx_monitor"].ValueType().(tftypes.Object)
	values := map[string]tftypes.Value{}
	for name, attributeType := range objectType.AttributeTypes {
		values[name] = tftypes.NewValue(attributeType, nil)
		if value, ok := attrs[name]; ok {
			values[name] = value
		}
	}
	config, err := tfprotov6.NewDynamicValue(objectType, tftypes.NewValue(objectType, values))
	if err != nil {
		t.Fatal(err)
	}
	resp, err := server.ValidateResourceConfig(ctx, &tfprotov6.ValidateResourceConfigRequest{TypeName: "sutramx_monitor", Config: &config})
	if err != nil {
		t.Fatal(err)
	}
	var errs []string
	for _, d := range resp.Diagnostics {
		if d.Severity == tfprotov6.DiagnosticSeverityError {
			errs = append(errs, d.Detail)
		}
	}
	return errs
}

func str(value string) tftypes.Value { return tftypes.NewValue(tftypes.String, value) }

func expectValidation(t *testing.T, name string, attrs map[string]tftypes.Value, want string) {
	t.Helper()
	errs := validateMonitorConfig(t, attrs)
	joined := strings.Join(errs, "\n")
	switch {
	case want == "" && len(errs) > 0:
		t.Errorf("%s: unexpected errors: %s", name, joined)
	case want != "" && !strings.Contains(joined, want):
		t.Errorf("%s: errors %q, want one containing %q", name, joined, want)
	}
}

// http and api monitors need a url (the API cannot store one without it);
// other types may leave it out.
func TestMonitorURLRequiredForHTTPAndAPI(t *testing.T) {
	expectValidation(t, "default type without url", map[string]tftypes.Value{"name": str("Web")}, "url is required for http monitors")
	expectValidation(t, "api without url", map[string]tftypes.Value{"name": str("API"), "type": str("api")}, "url is required for api monitors")
	expectValidation(t, "http with url", map[string]tftypes.Value{"name": str("Web"), "url": str("https://example.com")}, "")
	expectValidation(t, "port without url", map[string]tftypes.Value{"name": str("DB"), "type": str("port")}, "")
	expectValidation(t, "unknown type", map[string]tftypes.Value{"name": str("X"), "type": tftypes.NewValue(tftypes.String, tftypes.UnknownValue)}, "")
	expectValidation(t, "empty url", map[string]tftypes.Value{"name": str("Web"), "url": str("")}, "at least 1")
	expectValidation(t, "untrimmed url", map[string]tftypes.Value{"name": str("Web"), "url": str("https://example.com ")}, `write it as "https://example.com"`)
	expectValidation(t, "untrimmed name", map[string]tftypes.Value{"name": str(" Web"), "url": str("https://example.com")}, `write it as "Web"`)
}

// Removing url from a non-HTTP monitor clears it in SutramX: the keyed
// upsert leaves an omitted url alone, so the provider sends url "" through
// PUT /monitors/:id and stores the re-read monitor.
func TestMonitorUpsertClearsRemovedURL(t *testing.T) {
	api := newFakeAPI()
	server := httptest.NewServer(api)
	defer server.Close()
	api.monitors["00000000-0000-4000-8000-000000000099"] = map[string]any{
		"id": "00000000-0000-4000-8000-000000000099", "external_id": "db", "name": "DB", "type": "port",
		"url": "https://old.example.com", "interval_seconds": 120, "config": map[string]any{"host": "db.example.com", "port": 5432},
		"tags": []string{}, "probe_regions": nil, "is_active": true,
	}
	r := &monitorResource{client: client.New("sk_test", server.URL, "test")}
	ctx := context.Background()
	plan := monitorModel{
		Key: types.StringValue("db"), Name: types.StringValue("DB"), Type: types.StringValue("port"),
		URL: types.StringNull(), IntervalSeconds: types.Int64Unknown(), ConfigJSON: types.StringUnknown(),
		Tags: types.SetUnknown(types.StringType), Regions: types.SetNull(types.StringType), Paused: types.BoolValue(false),
	}
	var diags diag.Diagnostics
	monitor := r.upsert(ctx, "db", plan, types.StringNull(), &diags)
	if diags.HasError() {
		t.Fatal(diags)
	}
	if monitor.URL != nil && *monitor.URL != "" {
		t.Fatalf("url = %q, want none", *monitor.URL)
	}
	if stored := api.monitors["00000000-0000-4000-8000-000000000099"]["url"]; stored != nil {
		t.Fatalf("stored url = %v, want nil", stored)
	}
	applyMonitor(ctx, *monitor, &plan, &diags)
	if !plan.URL.IsNull() {
		t.Fatalf("state url = %s, want null", plan.URL)
	}

	// A configured url is sent through the upsert and not cleared.
	plan.URL = types.StringValue("https://new.example.com")
	monitor = r.upsert(ctx, "db", plan, types.StringNull(), &diags)
	if diags.HasError() || monitor.URL == nil || *monitor.URL != "https://new.example.com" {
		t.Fatalf("url = %v, diags %v", monitor.URL, diags)
	}
}
