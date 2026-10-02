package provider

import (
	"context"
	"os"
	"testing"

	"github.com/hashicorp/terraform-plugin-framework/datasource"
	fwprovider "github.com/hashicorp/terraform-plugin-framework/provider"
	"github.com/hashicorp/terraform-plugin-framework/providerserver"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-go/tfprotov6"
)

// testAccProtoV6ProviderFactories runs the provider in-process for acceptance tests.
var testAccProtoV6ProviderFactories = map[string]func() (tfprotov6.ProviderServer, error){
	"sutramx": providerserver.NewProtocol6WithError(New("test")()),
}

// testAccPreCheck: acceptance tests (TF_ACC=1) run against a real workspace.
// Use a disposable workspace: they create and delete monitors, status pages
// and (with SUTRAMX_ACC_ALERT_CHANNELS=1 and an automation key) alert channels.
func testAccPreCheck(t *testing.T) {
	t.Helper()
	if os.Getenv("SUTRAMX_API_KEY") == "" {
		t.Fatal("SUTRAMX_API_KEY must be set for acceptance tests")
	}
}

func TestProviderSchemaIsValid(t *testing.T) {
	ctx := context.Background()
	p := New("test")()

	var providerSchema fwprovider.SchemaResponse
	p.Schema(ctx, fwprovider.SchemaRequest{}, &providerSchema)
	if providerSchema.Diagnostics.HasError() {
		t.Fatalf("provider schema: %v", providerSchema.Diagnostics)
	}
	if diags := providerSchema.Schema.ValidateImplementation(ctx); diags.HasError() {
		t.Fatalf("provider schema implementation: %v", diags)
	}

	for _, factory := range p.Resources(ctx) {
		r := factory()
		var meta resource.MetadataResponse
		r.Metadata(ctx, resource.MetadataRequest{ProviderTypeName: "sutramx"}, &meta)
		var resp resource.SchemaResponse
		r.Schema(ctx, resource.SchemaRequest{}, &resp)
		if resp.Diagnostics.HasError() {
			t.Fatalf("%s schema: %v", meta.TypeName, resp.Diagnostics)
		}
		if diags := resp.Schema.ValidateImplementation(ctx); diags.HasError() {
			t.Fatalf("%s schema implementation: %v", meta.TypeName, diags)
		}
	}
	for _, factory := range p.DataSources(ctx) {
		d := factory()
		var meta datasource.MetadataResponse
		d.Metadata(ctx, datasource.MetadataRequest{ProviderTypeName: "sutramx"}, &meta)
		var resp datasource.SchemaResponse
		d.Schema(ctx, datasource.SchemaRequest{}, &resp)
		if resp.Diagnostics.HasError() {
			t.Fatalf("%s schema: %v", meta.TypeName, resp.Diagnostics)
		}
		if diags := resp.Schema.ValidateImplementation(ctx); diags.HasError() {
			t.Fatalf("%s schema implementation: %v", meta.TypeName, diags)
		}
	}
}

func TestJSONHelpers(t *testing.T) {
	if !jsonEqual(`{"b":1,"a":[1,2]}`, `{ "a": [1, 2], "b": 1 }`) {
		t.Fatal("key order and whitespace must not matter")
	}
	if jsonEqual(`{"a":[1,2]}`, `{"a":[2,1]}`) {
		t.Fatal("array order matters")
	}
	if got := withoutKeys([]byte(`{"timeout":5000,"notification_emails":["a@b.c"]}`), "notification_emails"); got != `{"timeout":5000}` {
		t.Fatalf("withoutKeys = %s", got)
	}
	if got := withoutKeys(nil); got != "{}" {
		t.Fatalf("withoutKeys(nil) = %s", got)
	}
	if key := randomKey("tf-"); len(key) != 15 || !monitorKeyPattern.MatchString(key) {
		t.Fatalf("randomKey = %q", key)
	}
}

func TestUndeclaredKeys(t *testing.T) {
	keys := []string{"notification_emails"}
	if got := undeclaredKeys(`{"timeout":1,"notification_emails":[]}`, keys); len(got) != 0 {
		t.Fatalf("declared key must be kept, got %v", got)
	}
	if got := undeclaredKeys(`{"timeout":1}`, keys); len(got) != 1 {
		t.Fatalf("undeclared key must be stripped, got %v", got)
	}
}
