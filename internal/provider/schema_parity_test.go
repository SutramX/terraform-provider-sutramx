package provider

import (
	"context"
	"strings"
	"testing"

	"github.com/hashicorp/terraform-plugin-go/tfprotov6"
	"github.com/hashicorp/terraform-plugin-go/tftypes"
)

// validateResourceErrors runs the provider's config validation for any
// resource type at the protocol level and returns the error summaries and
// details joined.
func validateResourceErrors(t *testing.T, typeName string, attrs map[string]tftypes.Value) string {
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
	objectType := schemas.ResourceSchemas[typeName].ValueType().(tftypes.Object)
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
	resp, err := server.ValidateResourceConfig(ctx, &tfprotov6.ValidateResourceConfigRequest{TypeName: typeName, Config: &config})
	if err != nil {
		t.Fatal(err)
	}
	var errs []string
	for _, d := range resp.Diagnostics {
		if d.Severity == tfprotov6.DiagnosticSeverityError {
			errs = append(errs, d.Summary+": "+d.Detail)
		}
	}
	return strings.Join(errs, "\n")
}

func expectResourceValidation(t *testing.T, typeName, name string, attrs map[string]tftypes.Value, want string) {
	t.Helper()
	got := validateResourceErrors(t, typeName, attrs)
	switch {
	case want == "" && got != "":
		t.Errorf("%s: unexpected errors: %s", name, got)
	case want != "" && !strings.Contains(got, want):
		t.Errorf("%s: errors %q, want one containing %q", name, got, want)
	}
}

var pageMonitorType = tftypes.Object{AttributeTypes: map[string]tftypes.Type{"monitor_id": tftypes.String, "section": tftypes.String}}

func pageMonitors(ids ...tftypes.Value) tftypes.Value {
	items := make([]tftypes.Value, 0, len(ids))
	for _, id := range ids {
		items = append(items, tftypes.NewValue(pageMonitorType, map[string]tftypes.Value{"monitor_id": id, "section": tftypes.NewValue(tftypes.String, nil)}))
	}
	return tftypes.NewValue(tftypes.List{ElementType: pageMonitorType}, items)
}

// Slug and logo_url follow the API's rules (statusPageState.validateSlug,
// the httpsUrl schema), and a monitor listed twice fails at plan time (the
// API keeps only the first entry).
func TestStatusPageValidationMatchesAPI(t *testing.T) {
	const page = "sutramx_status_page"
	title := str("Status")
	a, b := str("00000000-0000-4000-8000-000000000001"), str("00000000-0000-4000-8000-000000000002")
	unknown := tftypes.NewValue(tftypes.String, tftypes.UnknownValue)
	cases := []struct {
		name  string
		attrs map[string]tftypes.Value
		want  string
	}{
		{"good slug", map[string]tftypes.Value{"title": title, "slug": str("acme-status-2")}, ""},
		{"leading hyphen", map[string]tftypes.Value{"title": title, "slug": str("-acme")}, "single hyphens"},
		{"trailing hyphen", map[string]tftypes.Value{"title": title, "slug": str("acme-")}, "single hyphens"},
		{"double hyphen", map[string]tftypes.Value{"title": title, "slug": str("acme--status")}, "single hyphens"},
		{"upper case", map[string]tftypes.Value{"title": title, "slug": str("Acme")}, "single hyphens"},
		{"too short", map[string]tftypes.Value{"title": title, "slug": str("ab")}, "length"},
		{"too long", map[string]tftypes.Value{"title": title, "slug": str(strings.Repeat("a", 65))}, "length"},
		{"reserved", map[string]tftypes.Value{"title": title, "slug": str("admin")}, "admin"},
		{"logo https", map[string]tftypes.Value{"title": title, "logo_url": str("https://cdn.example.com/logo.png")}, ""},
		{"logo http", map[string]tftypes.Value{"title": title, "logo_url": str("http://cdn.example.com/logo.png")}, "https://"},
		{"logo credentials", map[string]tftypes.Value{"title": title, "logo_url": str("https://user:pw@cdn.example.com/logo.png")}, "user name or password"},
		{"logo no host", map[string]tftypes.Value{"title": title, "logo_url": str("https:///logo.png")}, "https://"},
		{"logo too long", map[string]tftypes.Value{"title": title, "logo_url": str("https://cdn.example.com/" + strings.Repeat("a", 2048))}, "2048"},
		{"logo whitespace", map[string]tftypes.Value{"title": title, "logo_url": str(" https://cdn.example.com/logo.png")}, "whitespace"},
		{"distinct monitors", map[string]tftypes.Value{"title": title, "monitors": pageMonitors(a, b)}, ""},
		{"duplicate monitor", map[string]tftypes.Value{"title": title, "monitors": pageMonitors(a, b, a)}, "already listed at monitors[0]"},
		{"unknown ids", map[string]tftypes.Value{"title": title, "monitors": pageMonitors(unknown, unknown)}, ""},
		{"non-uuid monitor id", map[string]tftypes.Value{"title": title, "monitors": pageMonitors(str("web"))}, "UUID"},
	}
	for _, tc := range cases {
		expectResourceValidation(t, page, tc.name, tc.attrs, tc.want)
	}
}

// The API refuses connection names over 60 characters (CONNECTION_NAME_MAX).
func TestAlertChannelNameLengthMatchesAPI(t *testing.T) {
	config := tftypes.NewValue(tftypes.Map{ElementType: tftypes.String}, map[string]tftypes.Value{"webhook_url": str("https://hooks.example.com/x")})
	base := func(name string) map[string]tftypes.Value {
		return map[string]tftypes.Value{"type": str("slack"), "name": str(name), "config": config}
	}
	expectResourceValidation(t, "sutramx_alert_channel", "60 characters", base(strings.Repeat("a", 60)), "")
	expectResourceValidation(t, "sutramx_alert_channel", "61 characters", base(strings.Repeat("a", 61)), "length")
}
