package provider

import (
	"context"

	"github.com/hashicorp/terraform-plugin-framework-validators/stringvalidator"
	"github.com/hashicorp/terraform-plugin-framework/datasource"
	"github.com/hashicorp/terraform-plugin-framework/datasource/schema"
	"github.com/hashicorp/terraform-plugin-framework/diag"
	"github.com/hashicorp/terraform-plugin-framework/schema/validator"
	"github.com/hashicorp/terraform-plugin-framework/types"

	"github.com/sutramx/terraform-provider-sutramx/internal/client"
)

// Maintenance windows and escalation policies silence or reroute alerting,
// so the API only lets a workspace owner's dashboard session create, change
// or delete them (requireWorkspaceOwner); API keys, including automation
// keys, can only list them. They are therefore read-only data sources here,
// not resources.

var (
	_ datasource.DataSourceWithConfigure = (*maintenanceWindowsDataSource)(nil)
	_ datasource.DataSourceWithConfigure = (*escalationPoliciesDataSource)(nil)
)

// workspaceDataSourceClient is dataSourceClient for workspace data, which
// needs an API key.
func workspaceDataSourceClient(req datasource.ConfigureRequest, resp *datasource.ConfigureResponse) *client.Client {
	if req.ProviderData == nil {
		return nil
	}
	c := dataSourceClient(req, resp)
	if data, ok := req.ProviderData.(*providerData); ok && !data.hasKey {
		resp.Diagnostics.AddError("Missing SutramX API key",
			"Set api_key in the provider block or the SUTRAMX_API_KEY environment variable. Create a key in SutramX → Settings → API keys.")
		return nil
	}
	return c
}

func int64ListValue(ctx context.Context, values []int64, diags *diag.Diagnostics) types.List {
	if values == nil {
		values = []int64{}
	}
	list, d := types.ListValueFrom(ctx, types.Int64Type, values)
	diags.Append(d...)
	return list
}

func nonNilStrings(values []string) []string {
	if values == nil {
		return []string{}
	}
	return values
}

// ---- sutramx_maintenance_windows -------------------------------------------

func NewMaintenanceWindowsDataSource() datasource.DataSource { return &maintenanceWindowsDataSource{} }

type maintenanceWindowsDataSource struct {
	client *client.Client
}

type maintenanceWindowModel struct {
	ID                 types.String `tfsdk:"id"`
	Title              types.String `tfsdk:"title"`
	Description        types.String `tfsdk:"description"`
	Status             types.String `tfsdk:"status"`
	EffectiveStatus    types.String `tfsdk:"effective_status"`
	StartTime          types.String `tfsdk:"start_time"`
	EndTime            types.String `tfsdk:"end_time"`
	Timezone           types.String `tfsdk:"timezone"`
	Impact             types.String `tfsdk:"impact"`
	ScopeType          types.String `tfsdk:"scope_type"`
	MonitorIDs         types.List   `tfsdk:"monitor_ids"`
	GroupIDs           types.List   `tfsdk:"group_ids"`
	AffectedServices   types.List   `tfsdk:"affected_services"`
	RecurrenceType     types.String `tfsdk:"recurrence_type"`
	RecurrenceWeekdays types.List   `tfsdk:"recurrence_weekdays"`
	RecurrenceUntil    types.String `tfsdk:"recurrence_until"`
}

type maintenanceWindowsModel struct {
	EffectiveStatus types.String             `tfsdk:"effective_status"`
	IDs             types.List               `tfsdk:"ids"`
	Windows         []maintenanceWindowModel `tfsdk:"windows"`
}

var maintenanceStatuses = []string{"scheduled", "ongoing", "completed", "cancelled"}

func (d *maintenanceWindowsDataSource) Metadata(_ context.Context, req datasource.MetadataRequest, resp *datasource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_maintenance_windows"
}

func (d *maintenanceWindowsDataSource) Schema(_ context.Context, _ datasource.SchemaRequest, resp *datasource.SchemaResponse) {
	computedString := func(description string) schema.StringAttribute {
		return schema.StringAttribute{Computed: true, MarkdownDescription: description}
	}
	resp.Schema = schema.Schema{
		MarkdownDescription: "Maintenance windows of the workspace, newest start first. Read-only: creating or changing a window silences alerts, " +
			"so the API only allows it from the workspace owner's dashboard session, not with API keys. Works with any API key, including read-only keys.",
		Attributes: map[string]schema.Attribute{
			"effective_status": schema.StringAttribute{
				Optional:            true,
				MarkdownDescription: "Only windows in this live state: `scheduled`, `ongoing`, `completed` or `cancelled`.",
				Validators:          []validator.String{stringvalidator.OneOf(maintenanceStatuses...)},
			},
			"ids": schema.ListAttribute{Computed: true, ElementType: types.StringType, MarkdownDescription: "Ids of the returned windows, in order."},
			"windows": schema.ListNestedAttribute{
				Computed: true,
				NestedObject: schema.NestedAttributeObject{
					Attributes: map[string]schema.Attribute{
						"id":                computedString(""),
						"title":             computedString(""),
						"description":       computedString(""),
						"status":            computedString("Stored status: `scheduled`, `ongoing`, `completed` or `cancelled`."),
						"effective_status":  computedString("Status computed from the current time (the stored status can lag behind)."),
						"start_time":        computedString("RFC 3339 start time (UTC)."),
						"end_time":          computedString("RFC 3339 end time (UTC)."),
						"timezone":          computedString("IANA time zone the window was scheduled in."),
						"impact":            computedString("`low`, `medium` or `high`."),
						"scope_type":        computedString("`global` (every monitor), `monitor` or `group`."),
						"monitor_ids":       schema.ListAttribute{Computed: true, ElementType: types.StringType, MarkdownDescription: "Monitors covered when `scope_type` is `monitor`."},
						"group_ids":         schema.ListAttribute{Computed: true, ElementType: types.StringType, MarkdownDescription: "Monitor groups covered when `scope_type` is `group`."},
						"affected_services": schema.ListAttribute{Computed: true, ElementType: types.StringType},
						"recurrence_type":   computedString("`none`, `daily` or `weekly`."),
						"recurrence_weekdays": schema.ListAttribute{
							Computed: true, ElementType: types.Int64Type,
							MarkdownDescription: "Weekly windows: days of the week (0 = Sunday).",
						},
						"recurrence_until": computedString("Last day a recurring window repeats; null when it repeats indefinitely or does not recur."),
					},
				},
			},
		},
	}
}

func (d *maintenanceWindowsDataSource) Configure(_ context.Context, req datasource.ConfigureRequest, resp *datasource.ConfigureResponse) {
	d.client = workspaceDataSourceClient(req, resp)
}

func (d *maintenanceWindowsDataSource) Read(ctx context.Context, req datasource.ReadRequest, resp *datasource.ReadResponse) {
	var state maintenanceWindowsModel
	resp.Diagnostics.Append(req.Config.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}
	var windows []client.MaintenanceWindow
	if err := d.client.Get(ctx, "/maintenance", &windows); err != nil {
		apiErrorDiag(&resp.Diagnostics, "Could not list maintenance windows", err)
		return
	}
	filter := state.EffectiveStatus.ValueString()
	state.Windows = []maintenanceWindowModel{}
	ids := []string{}
	for _, w := range windows {
		if filter != "" && w.EffectiveStatus != filter {
			continue
		}
		ids = append(ids, w.ID)
		state.Windows = append(state.Windows, maintenanceWindowModel{
			ID:                 types.StringValue(w.ID),
			Title:              types.StringValue(w.Title),
			Description:        stringOrNull(&w.Description),
			Status:             types.StringValue(w.Status),
			EffectiveStatus:    types.StringValue(w.EffectiveStatus),
			StartTime:          types.StringValue(w.StartTime),
			EndTime:            types.StringValue(w.EndTime),
			Timezone:           types.StringValue(w.Timezone),
			Impact:             types.StringValue(w.Impact),
			ScopeType:          types.StringValue(w.ScopeType),
			MonitorIDs:         stringListValue(ctx, nonNilStrings(w.MonitorIDs), &resp.Diagnostics),
			GroupIDs:           stringListValue(ctx, nonNilStrings(w.GroupIDs), &resp.Diagnostics),
			AffectedServices:   stringListValue(ctx, nonNilStrings(w.AffectedServices), &resp.Diagnostics),
			RecurrenceType:     types.StringValue(w.Recurrence.Type),
			RecurrenceWeekdays: int64ListValue(ctx, w.Recurrence.Weekdays, &resp.Diagnostics),
			RecurrenceUntil:    stringOrNull(w.Recurrence.Until),
		})
	}
	state.IDs = stringListValue(ctx, ids, &resp.Diagnostics)
	resp.Diagnostics.Append(resp.State.Set(ctx, &state)...)
}

// ---- sutramx_escalation_policies -------------------------------------------

func NewEscalationPoliciesDataSource() datasource.DataSource { return &escalationPoliciesDataSource{} }

type escalationPoliciesDataSource struct {
	client *client.Client
}

type escalationStepModel struct {
	ID           types.String `tfsdk:"id"`
	StepOrder    types.Int64  `tfsdk:"step_order"`
	DelayMinutes types.Int64  `tfsdk:"delay_minutes"`
	Channel      types.String `tfsdk:"channel"`
	TargetType   types.String `tfsdk:"target_type"`
	TargetValue  types.String `tfsdk:"target_value"`
	TargetLabel  types.String `tfsdk:"target_label"`
}

type escalationPolicyModel struct {
	ID       types.String          `tfsdk:"id"`
	Name     types.String          `tfsdk:"name"`
	Enabled  types.Bool            `tfsdk:"enabled"`
	MaxDepth types.Int64           `tfsdk:"max_depth"`
	Steps    []escalationStepModel `tfsdk:"steps"`
}

type escalationPoliciesModel struct {
	Name     types.String            `tfsdk:"name"`
	Policies []escalationPolicyModel `tfsdk:"policies"`
}

func (d *escalationPoliciesDataSource) Metadata(_ context.Context, req datasource.MetadataRequest, resp *datasource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_escalation_policies"
}

func (d *escalationPoliciesDataSource) Schema(_ context.Context, _ datasource.SchemaRequest, resp *datasource.SchemaResponse) {
	resp.Schema = schema.Schema{
		MarkdownDescription: "Escalation policies of the workspace, newest first. Read-only: policies reroute alerts, so the API only lets the workspace owner " +
			"create or change them from the dashboard, not with API keys. Creating policies needs a plan that includes escalation.",
		Attributes: map[string]schema.Attribute{
			"name": schema.StringAttribute{
				Optional:            true,
				MarkdownDescription: "Only policies with exactly this name.",
				Validators:          []validator.String{stringvalidator.LengthAtLeast(1)},
			},
			"policies": schema.ListNestedAttribute{
				Computed: true,
				NestedObject: schema.NestedAttributeObject{
					Attributes: map[string]schema.Attribute{
						"id":        schema.StringAttribute{Computed: true},
						"name":      schema.StringAttribute{Computed: true},
						"enabled":   schema.BoolAttribute{Computed: true},
						"max_depth": schema.Int64Attribute{Computed: true, MarkdownDescription: "How many steps an incident escalates through at most."},
						"steps": schema.ListNestedAttribute{
							Computed:            true,
							MarkdownDescription: "Steps in escalation order.",
							NestedObject: schema.NestedAttributeObject{
								Attributes: map[string]schema.Attribute{
									"id":            schema.StringAttribute{Computed: true},
									"step_order":    schema.Int64Attribute{Computed: true},
									"delay_minutes": schema.Int64Attribute{Computed: true, MarkdownDescription: "Minutes after the previous step before this one notifies."},
									"channel":       schema.StringAttribute{Computed: true, MarkdownDescription: "Delivery channel, e.g. `email`, `slack`, `webhook`."},
									"target_type":   schema.StringAttribute{Computed: true, MarkdownDescription: "`email`, `on_call_primary`, `phone` or `integration`."},
									"target_value":  schema.StringAttribute{Computed: true, MarkdownDescription: "Address, number or integration connection id the step notifies."},
									"target_label":  schema.StringAttribute{Computed: true, MarkdownDescription: "Human-readable target, as shown in the dashboard."},
								},
							},
						},
					},
				},
			},
		},
	}
}

func (d *escalationPoliciesDataSource) Configure(_ context.Context, req datasource.ConfigureRequest, resp *datasource.ConfigureResponse) {
	d.client = workspaceDataSourceClient(req, resp)
}

func (d *escalationPoliciesDataSource) Read(ctx context.Context, req datasource.ReadRequest, resp *datasource.ReadResponse) {
	var state escalationPoliciesModel
	resp.Diagnostics.Append(req.Config.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}
	var policies []client.EscalationPolicy
	if err := d.client.Get(ctx, "/settings/escalation-policies", &policies); err != nil {
		apiErrorDiag(&resp.Diagnostics, "Could not list escalation policies", err)
		return
	}
	name := state.Name.ValueString()
	state.Policies = []escalationPolicyModel{}
	for _, policy := range policies {
		if name != "" && policy.Name != name {
			continue
		}
		steps := make([]escalationStepModel, 0, len(policy.Steps))
		for _, step := range policy.Steps {
			steps = append(steps, escalationStepModel{
				ID:           types.StringValue(step.ID),
				StepOrder:    types.Int64Value(step.StepOrder),
				DelayMinutes: types.Int64Value(step.DelayMinutes),
				Channel:      stringOrNull(step.Channel),
				TargetType:   stringOrNull(step.TargetType),
				TargetValue:  types.StringValue(step.TargetValue),
				TargetLabel:  stringOrNull(step.TargetLabel),
			})
		}
		state.Policies = append(state.Policies, escalationPolicyModel{
			ID:       types.StringValue(policy.ID),
			Name:     types.StringValue(policy.Name),
			Enabled:  types.BoolValue(policy.IsEnabled),
			MaxDepth: types.Int64Value(policy.MaxDepth),
			Steps:    steps,
		})
	}
	resp.Diagnostics.Append(resp.State.Set(ctx, &state)...)
}
