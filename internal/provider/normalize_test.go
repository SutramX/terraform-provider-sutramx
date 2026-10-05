package provider

import (
	"context"
	"strings"
	"testing"

	"github.com/hashicorp/terraform-plugin-framework/path"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema"
	"github.com/hashicorp/terraform-plugin-framework/schema/validator"
	"github.com/hashicorp/terraform-plugin-framework/types"
)

func validateNormalized(v normalizedStringValidator, value string) string {
	resp := &validator.StringResponse{}
	v.ValidateString(context.Background(), validator.StringRequest{Path: path.Root("x"), ConfigValue: types.StringValue(value)}, resp)
	if !resp.Diagnostics.HasError() {
		return ""
	}
	return resp.Diagnostics.Errors()[0].Detail()
}

// The API trims names, titles, descriptions and URLs (and collapses inner
// whitespace in alert channel names): a value it would rewrite is a
// plan-time error, never "inconsistent result after apply".
func TestNormalizedStringValidator(t *testing.T) {
	trimmed := normalizedStringValidator{what: "Name"}
	collapsed := normalizedStringValidator{what: "Name", collapseSpaces: true}
	cases := []struct {
		v     normalizedStringValidator
		value string
		want  string // substring of the error, "" for valid
	}{
		{trimmed, "Website", ""},
		{trimmed, "Web  site", ""}, // inner whitespace is kept by a plain trim
		{trimmed, "", ""},          // emptiness is the length validators' job
		{trimmed, " Website", `write it as "Website"`},
		{trimmed, "Website\n", `write it as "Website"`},
		{trimmed, "\uFEFFWebsite", `write it as "Website"`},
		{trimmed, "   ", "only whitespace"},
		{collapsed, "Ops alerts", ""},
		{collapsed, "Ops  alerts", `write it as "Ops alerts"`},
		{collapsed, "Ops\talerts", `write it as "Ops alerts"`},
		{collapsed, " Ops alerts ", `collapses inner whitespace`},
	}
	for _, c := range cases {
		got := validateNormalized(c.v, c.value)
		if c.want == "" && got != "" {
			t.Errorf("%q (collapse=%v): unexpected error %q", c.value, c.v.collapseSpaces, got)
		}
		if c.want != "" && !strings.Contains(got, c.want) {
			t.Errorf("%q (collapse=%v): error %q, want it to contain %q", c.value, c.v.collapseSpaces, got, c.want)
		}
	}
}

// Every attribute the API trims carries the validator.
func TestNormalizedAttributesHaveValidators(t *testing.T) {
	ctx := context.Background()
	check := func(r resource.Resource, attrs map[string]bool, nested func(schema.Schema) map[string]schema.Attribute) {
		t.Helper()
		var resp resource.SchemaResponse
		r.Schema(ctx, resource.SchemaRequest{}, &resp)
		all := resp.Schema.Attributes
		if nested != nil {
			all = nested(resp.Schema)
		}
		for name, collapse := range attrs {
			attribute, ok := all[name].(schema.StringAttribute)
			if !ok {
				t.Fatalf("%s is not a string attribute", name)
			}
			found := false
			for _, v := range attribute.Validators {
				if n, ok := v.(normalizedStringValidator); ok && n.collapseSpaces == collapse {
					found = true
				}
			}
			if !found {
				t.Errorf("%s: no normalizedStringValidator{collapseSpaces: %v}", name, collapse)
			}
		}
	}
	check(NewMonitorResource(), map[string]bool{"name": false, "url": false}, nil)
	check(NewAlertChannelResource(), map[string]bool{"name": true}, nil)
	check(NewStatusPageResource(), map[string]bool{"title": false, "description": false}, nil)
	check(NewStatusPageResource(), map[string]bool{"section": false}, func(s schema.Schema) map[string]schema.Attribute {
		return s.Attributes["monitors"].(schema.ListNestedAttribute).NestedObject.Attributes
	})
}
