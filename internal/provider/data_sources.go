package provider

import (
	"context"
	"fmt"

	"github.com/hashicorp/terraform-plugin-framework/datasource"
	"github.com/hashicorp/terraform-plugin-framework/datasource/schema"
	"github.com/hashicorp/terraform-plugin-framework/types"

	"github.com/sutramx/terraform-provider-sutramx/internal/client"
)

var (
	_ datasource.DataSourceWithConfigure = (*regionsDataSource)(nil)
	_ datasource.DataSourceWithConfigure = (*plansDataSource)(nil)
)

func dataSourceClient(req datasource.ConfigureRequest, resp *datasource.ConfigureResponse) *client.Client {
	if req.ProviderData == nil {
		return nil
	}
	data, ok := req.ProviderData.(*providerData)
	if !ok {
		resp.Diagnostics.AddError("Unexpected provider data", fmt.Sprintf("expected *providerData, got %T", req.ProviderData))
		return nil
	}
	return data.client
}

// ---- sutramx_regions --------------------------------------------------------

func NewRegionsDataSource() datasource.DataSource { return &regionsDataSource{} }

type regionsDataSource struct {
	client *client.Client
}

type regionModel struct {
	Code      types.String  `tfsdk:"code"`
	Name      types.String  `tfsdk:"name"`
	City      types.String  `tfsdk:"city"`
	Country   types.String  `tfsdk:"country"`
	Continent types.String  `tfsdk:"continent"`
	Latitude  types.Float64 `tfsdk:"latitude"`
	Longitude types.Float64 `tfsdk:"longitude"`
	Online    types.Bool    `tfsdk:"online"`
}

type regionsModel struct {
	OnlineOnly types.Bool    `tfsdk:"online_only"`
	Codes      types.List    `tfsdk:"codes"`
	Regions    []regionModel `tfsdk:"regions"`
}

func (d *regionsDataSource) Metadata(_ context.Context, req datasource.MetadataRequest, resp *datasource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_regions"
}

func (d *regionsDataSource) Schema(_ context.Context, _ datasource.SchemaRequest, resp *datasource.SchemaResponse) {
	resp.Schema = schema.Schema{
		MarkdownDescription: "SutramX probe locations. Use `code` in `sutramx_monitor.regions`. Which codes a monitor may use depends on the plan and billing market. Does not need an API key.",
		Attributes: map[string]schema.Attribute{
			"online_only": schema.BoolAttribute{Optional: true, MarkdownDescription: "Only locations whose probes are online now."},
			"codes":       schema.ListAttribute{Computed: true, ElementType: types.StringType, MarkdownDescription: "Location codes, in activation order."},
			"regions": schema.ListNestedAttribute{
				Computed: true,
				NestedObject: schema.NestedAttributeObject{
					Attributes: map[string]schema.Attribute{
						"code":      schema.StringAttribute{Computed: true},
						"name":      schema.StringAttribute{Computed: true},
						"city":      schema.StringAttribute{Computed: true},
						"country":   schema.StringAttribute{Computed: true, MarkdownDescription: "ISO 3166-1 alpha-2 code."},
						"continent": schema.StringAttribute{Computed: true},
						"latitude":  schema.Float64Attribute{Computed: true},
						"longitude": schema.Float64Attribute{Computed: true},
						"online":    schema.BoolAttribute{Computed: true},
					},
				},
			},
		},
	}
}

func (d *regionsDataSource) Configure(_ context.Context, req datasource.ConfigureRequest, resp *datasource.ConfigureResponse) {
	d.client = dataSourceClient(req, resp)
}

func float64OrNull(value *float64) types.Float64 {
	if value == nil {
		return types.Float64Null()
	}
	return types.Float64Value(*value)
}

func (d *regionsDataSource) Read(ctx context.Context, req datasource.ReadRequest, resp *datasource.ReadResponse) {
	var state regionsModel
	resp.Diagnostics.Append(req.Config.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}
	var data client.RegionsResponse
	if err := d.client.GetPublic(ctx, "/catalog/regions", &data); err != nil {
		apiErrorDiag(&resp.Diagnostics, "Could not load SutramX regions", err)
		return
	}
	onlineOnly := state.OnlineOnly.ValueBool()
	state.Regions = []regionModel{}
	codes := []string{}
	for _, region := range data.Regions {
		if onlineOnly && !region.Online {
			continue
		}
		codes = append(codes, region.Code)
		state.Regions = append(state.Regions, regionModel{
			Code:      types.StringValue(region.Code),
			Name:      types.StringValue(region.Name),
			City:      stringOrNull(region.City),
			Country:   stringOrNull(region.Country),
			Continent: stringOrNull(region.Continent),
			Latitude:  float64OrNull(region.Latitude),
			Longitude: float64OrNull(region.Longitude),
			Online:    types.BoolValue(region.Online),
		})
	}
	state.Codes = stringListValue(ctx, codes, &resp.Diagnostics)
	resp.Diagnostics.Append(resp.State.Set(ctx, &state)...)
}

// ---- sutramx_plans ----------------------------------------------------------

func NewPlansDataSource() datasource.DataSource { return &plansDataSource{} }

type plansDataSource struct {
	client *client.Client
}

type planModel struct {
	ID                 types.String  `tfsdk:"id"`
	Name               types.String  `tfsdk:"name"`
	Description        types.String  `tfsdk:"description"`
	Popular            types.Bool    `tfsdk:"popular"`
	SortOrder          types.Int64   `tfsdk:"sort_order"`
	Markets            types.List    `tfsdk:"markets"`
	PriceMonthlyUSD    types.Float64 `tfsdk:"price_monthly_usd"`
	PriceAnnualUSD     types.Float64 `tfsdk:"price_annual_usd"`
	PriceMonthlyINR    types.Float64 `tfsdk:"price_monthly_inr"`
	PriceAnnualINR     types.Float64 `tfsdk:"price_annual_inr"`
	Monitors           types.Int64   `tfsdk:"monitors"`
	MinIntervalSeconds types.Int64   `tfsdk:"min_interval_seconds"`
	ProbeLocations     types.Int64   `tfsdk:"probe_locations"`
	StatusPages        types.Int64   `tfsdk:"status_pages"`
	APIKeys            types.Int64   `tfsdk:"api_keys"`
}

type plansModel struct {
	Plans []planModel `tfsdk:"plans"`
}

func (d *plansDataSource) Metadata(_ context.Context, req datasource.MetadataRequest, resp *datasource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_plans"
}

func (d *plansDataSource) Schema(_ context.Context, _ datasource.SchemaRequest, resp *datasource.SchemaResponse) {
	int64Null := func(description string) schema.Int64Attribute {
		return schema.Int64Attribute{Computed: true, MarkdownDescription: description}
	}
	resp.Schema = schema.Schema{
		MarkdownDescription: "The current SutramX plans with prices and main limits, as listed on the pricing page. Does not need an API key.",
		Attributes: map[string]schema.Attribute{
			"plans": schema.ListNestedAttribute{
				Computed: true,
				NestedObject: schema.NestedAttributeObject{
					Attributes: map[string]schema.Attribute{
						"id":                   schema.StringAttribute{Computed: true},
						"name":                 schema.StringAttribute{Computed: true},
						"description":          schema.StringAttribute{Computed: true},
						"popular":              schema.BoolAttribute{Computed: true},
						"sort_order":           schema.Int64Attribute{Computed: true},
						"markets":              schema.ListAttribute{Computed: true, ElementType: types.StringType, MarkdownDescription: "`global` (USD) and/or `india` (INR)."},
						"price_monthly_usd":    schema.Float64Attribute{Computed: true},
						"price_annual_usd":     schema.Float64Attribute{Computed: true},
						"price_monthly_inr":    schema.Float64Attribute{Computed: true},
						"price_annual_inr":     schema.Float64Attribute{Computed: true},
						"monitors":             int64Null("Monitor limit."),
						"min_interval_seconds": int64Null("Fastest check interval."),
						"probe_locations":      int64Null("Locations per monitor; null means every active location."),
						"status_pages":         int64Null("Status page limit; null means unlimited."),
						"api_keys":             int64Null("API key limit; null means unlimited."),
					},
				},
			},
		},
	}
}

func (d *plansDataSource) Configure(_ context.Context, req datasource.ConfigureRequest, resp *datasource.ConfigureResponse) {
	d.client = dataSourceClient(req, resp)
}

func int64OrNull(value *int64) types.Int64 {
	if value == nil {
		return types.Int64Null()
	}
	return types.Int64Value(*value)
}

func price(plan client.Plan, market string, annual bool) types.Float64 {
	value, ok := plan.Pricing[market]
	if !ok {
		return types.Float64Null()
	}
	if annual {
		return types.Float64Value(value.Annual)
	}
	return types.Float64Value(value.Monthly)
}

func (d *plansDataSource) Read(ctx context.Context, _ datasource.ReadRequest, resp *datasource.ReadResponse) {
	var data client.PlansResponse
	if err := d.client.GetPublic(ctx, "/catalog/plans", &data); err != nil {
		apiErrorDiag(&resp.Diagnostics, "Could not load SutramX plans", err)
		return
	}
	state := plansModel{Plans: []planModel{}}
	for _, plan := range data.Plans {
		state.Plans = append(state.Plans, planModel{
			ID:                 types.StringValue(plan.ID),
			Name:               types.StringValue(plan.Name),
			Description:        types.StringValue(plan.Description),
			Popular:            types.BoolValue(plan.IsPopular),
			SortOrder:          types.Int64Value(plan.SortOrder),
			Markets:            stringListValue(ctx, plan.Markets, &resp.Diagnostics),
			PriceMonthlyUSD:    price(plan, "global", false),
			PriceAnnualUSD:     price(plan, "global", true),
			PriceMonthlyINR:    price(plan, "india", false),
			PriceAnnualINR:     price(plan, "india", true),
			Monitors:           int64OrNull(plan.Limits.Monitors),
			MinIntervalSeconds: int64OrNull(plan.Limits.MinIntervalSeconds),
			ProbeLocations:     int64OrNull(plan.Limits.ProbeLocations),
			StatusPages:        int64OrNull(plan.Limits.StatusPages),
			APIKeys:            int64OrNull(plan.Limits.APIKeys),
		})
	}
	resp.Diagnostics.Append(resp.State.Set(ctx, &state)...)
}
