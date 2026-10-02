// Package provider implements the SutramX Terraform provider with the
// Terraform Plugin Framework.
package provider

import (
	"context"
	"os"

	"github.com/hashicorp/terraform-plugin-framework/datasource"
	"github.com/hashicorp/terraform-plugin-framework/path"
	"github.com/hashicorp/terraform-plugin-framework/provider"
	"github.com/hashicorp/terraform-plugin-framework/provider/schema"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/types"

	"github.com/sutramx/terraform-provider-sutramx/internal/client"
)

var _ provider.Provider = (*sutramxProvider)(nil)

type sutramxProvider struct {
	version string
}

type providerModel struct {
	APIKey types.String `tfsdk:"api_key"`
	APIURL types.String `tfsdk:"api_url"`
}

// New returns a provider factory.
func New(version string) func() provider.Provider {
	return func() provider.Provider {
		return &sutramxProvider{version: version}
	}
}

func (p *sutramxProvider) Metadata(_ context.Context, _ provider.MetadataRequest, resp *provider.MetadataResponse) {
	resp.TypeName = "sutramx"
	resp.Version = p.version
}

func (p *sutramxProvider) Schema(_ context.Context, _ provider.SchemaRequest, resp *provider.SchemaResponse) {
	resp.Schema = schema.Schema{
		MarkdownDescription: "Manage [SutramX](https://sutramx.com) uptime monitors, status pages and alert channels. " +
			"Authenticates with a workspace API key; every resource lives in the workspace the key belongs to.",
		Attributes: map[string]schema.Attribute{
			"api_key": schema.StringAttribute{
				MarkdownDescription: "SutramX API key (`sk_...`), from Settings → API keys. Can also be set with `SUTRAMX_API_KEY`. " +
					"`sutramx_alert_channel` needs a key created with **Automation access**.",
				Optional:  true,
				Sensitive: true,
			},
			"api_url": schema.StringAttribute{
				MarkdownDescription: "API base URL. Defaults to `https://api.sutramx.com`, or `SUTRAMX_API_URL`.",
				Optional:            true,
			},
		},
	}
}

func (p *sutramxProvider) Configure(ctx context.Context, req provider.ConfigureRequest, resp *provider.ConfigureResponse) {
	var config providerModel
	resp.Diagnostics.Append(req.Config.Get(ctx, &config)...)
	if resp.Diagnostics.HasError() {
		return
	}
	if config.APIKey.IsUnknown() {
		resp.Diagnostics.AddAttributeError(path.Root("api_key"), "Unknown SutramX API key",
			"The api_key value is not known until apply. Set it statically or with the SUTRAMX_API_KEY environment variable.")
		return
	}

	apiKey := os.Getenv("SUTRAMX_API_KEY")
	if !config.APIKey.IsNull() {
		apiKey = config.APIKey.ValueString()
	}
	apiURL := os.Getenv("SUTRAMX_API_URL")
	if !config.APIURL.IsNull() && !config.APIURL.IsUnknown() {
		apiURL = config.APIURL.ValueString()
	}

	// Data sources for public data (regions, plans) work without a key;
	// resources report a clear error when it is missing.
	c := client.New(apiKey, apiURL, "terraform-provider-sutramx/"+p.version)
	data := &providerData{client: c, hasKey: apiKey != ""}
	resp.DataSourceData = data
	resp.ResourceData = data
}

func (p *sutramxProvider) Resources(_ context.Context) []func() resource.Resource {
	return []func() resource.Resource{
		NewMonitorResource,
		NewStatusPageResource,
		NewAlertChannelResource,
	}
}

func (p *sutramxProvider) DataSources(_ context.Context) []func() datasource.DataSource {
	return []func() datasource.DataSource{
		NewRegionsDataSource,
		NewPlansDataSource,
	}
}

type providerData struct {
	client *client.Client
	hasKey bool
}
