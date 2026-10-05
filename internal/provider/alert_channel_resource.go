package provider

import (
	"context"
	"fmt"

	"github.com/hashicorp/terraform-plugin-framework-validators/setvalidator"
	"github.com/hashicorp/terraform-plugin-framework-validators/stringvalidator"
	"github.com/hashicorp/terraform-plugin-framework/diag"
	"github.com/hashicorp/terraform-plugin-framework/path"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/planmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/stringdefault"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/stringplanmodifier"
	"github.com/hashicorp/terraform-plugin-framework/schema/validator"
	"github.com/hashicorp/terraform-plugin-framework/types"

	"github.com/sutramx/terraform-provider-sutramx/internal/client"
)

var (
	_ resource.Resource                   = (*alertChannelResource)(nil)
	_ resource.ResourceWithConfigure      = (*alertChannelResource)(nil)
	_ resource.ResourceWithImportState    = (*alertChannelResource)(nil)
	_ resource.ResourceWithValidateConfig = (*alertChannelResource)(nil)
)

// Routing scopes of a connection (backend IntegrationRouting.scope).
const (
	routingScopeAll      = "all"
	routingScopeGroups   = "groups"
	routingScopeMonitors = "monitors"
)

// The API accepts at most this many group or monitor ids per connection.
const maxRoutingTargets = 500

func NewAlertChannelResource() resource.Resource { return &alertChannelResource{} }

type alertChannelResource struct {
	client *client.Client
}

type alertChannelModel struct {
	ID            types.String `tfsdk:"id"`
	Type          types.String `tfsdk:"type"`
	Name          types.String `tfsdk:"name"`
	Config        types.Map    `tfsdk:"config"`
	RoutingScope  types.String `tfsdk:"routing_scope"`
	GroupIDs      types.Set    `tfsdk:"group_ids"`
	MonitorIDs    types.Set    `tfsdk:"monitor_ids"`
	Status        types.String `tfsdk:"status"`
	SigningSecret types.String `tfsdk:"signing_secret"`
}

func (r *alertChannelResource) Metadata(_ context.Context, req resource.MetadataRequest, resp *resource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_alert_channel"
}

func (r *alertChannelResource) Schema(_ context.Context, _ resource.SchemaRequest, resp *resource.SchemaResponse) {
	resp.Schema = schema.Schema{
		MarkdownDescription: "An alert channel (integration connection) such as Slack, Discord, Microsoft Teams, a webhook, PagerDuty or Opsgenie. " +
			"Requires an API key created with **Automation access**. Which channel types are available depends on the plan.",
		Attributes: map[string]schema.Attribute{
			"id": schema.StringAttribute{
				Computed:      true,
				PlanModifiers: []planmodifier.String{stringplanmodifier.UseStateForUnknown()},
			},
			"type": schema.StringAttribute{
				MarkdownDescription: "Channel type as shown in the dashboard's integration list, e.g. `slack`, `discord`, `msteams`, `webhook`, `pagerduty`, `opsgenie`, `telegram`. Changing it replaces the channel.",
				Required:            true,
				PlanModifiers:       []planmodifier.String{stringplanmodifier.RequiresReplace()},
			},
			"name": schema.StringAttribute{
				MarkdownDescription: "Display name, unique among channels of the same type. 1-80 characters; SutramX trims it and collapses runs of whitespace to one space, so write it that way.",
				Required:            true,
				Validators:          []validator.String{stringvalidator.LengthBetween(1, 80), normalizedStringValidator{what: "Name", collapseSpaces: true}},
			},
			"config": schema.MapAttribute{
				MarkdownDescription: "Channel fields, e.g. `{ webhook_url = var.slack_webhook_url }` for Slack or `{ integration_key = ... }` for PagerDuty. " +
					"Stored encrypted by SutramX and never read back, so changes made outside Terraform are not detected.",
				ElementType: types.StringType,
				Required:    true,
				Sensitive:   true,
			},
			"routing_scope": schema.StringAttribute{
				MarkdownDescription: "Which monitors this channel alerts for: `all` (every monitor, default), `groups` (monitors in the monitor groups listed in `group_ids`) " +
					"or `monitors` (only the monitors listed in `monitor_ids`).",
				Optional:   true,
				Computed:   true,
				Default:    stringdefault.StaticString(routingScopeAll),
				Validators: []validator.String{stringvalidator.OneOf(routingScopeAll, routingScopeGroups, routingScopeMonitors)},
			},
			"group_ids": schema.SetAttribute{
				MarkdownDescription: "Monitor group ids (UUIDs, up to 500) this channel alerts for. Required when `routing_scope = \"groups\"`, not allowed otherwise. " +
					"Monitors that are not in a group never match. Every group must exist in the workspace.",
				ElementType: types.StringType,
				Optional:    true,
				Validators:  routingTargetValidators("monitor group ids"),
			},
			"monitor_ids": schema.SetAttribute{
				MarkdownDescription: "Monitor ids (UUIDs, up to 500) this channel alerts for. Required when `routing_scope = \"monitors\"`, not allowed otherwise. " +
					"Every monitor must exist in the workspace.",
				ElementType: types.StringType,
				Optional:    true,
				Validators:  routingTargetValidators("monitor ids"),
			},
			"status": schema.StringAttribute{
				MarkdownDescription: "Connection status reported by SutramX.",
				Computed:            true,
				PlanModifiers:       []planmodifier.String{stringplanmodifier.UseStateForUnknown()},
			},
			"signing_secret": schema.StringAttribute{
				MarkdownDescription: "For `webhook` channels: the secret SutramX signs payloads with (only known when Terraform created the channel).",
				Computed:            true,
				Sensitive:           true,
				PlanModifiers:       []planmodifier.String{stringplanmodifier.UseStateForUnknown()},
			},
		},
	}
}

func routingTargetValidators(what string) []validator.Set {
	return []validator.Set{
		setvalidator.SizeAtMost(maxRoutingTargets),
		setvalidator.ValueStringsAre(stringvalidator.RegexMatches(uuidPattern, what+" are UUIDs")),
	}
}

// ValidateConfig checks that the id list matching routing_scope is set and
// the other one is not: the API keeps only the list of the chosen scope, so
// any other combination could never be read back as configured.
func (r *alertChannelResource) ValidateConfig(ctx context.Context, req resource.ValidateConfigRequest, resp *resource.ValidateConfigResponse) {
	var config alertChannelModel
	resp.Diagnostics.Append(req.Config.Get(ctx, &config)...)
	if resp.Diagnostics.HasError() || config.RoutingScope.IsUnknown() {
		return
	}
	scope := routingScopeAll
	if !config.RoutingScope.IsNull() {
		scope = config.RoutingScope.ValueString()
	}
	for _, list := range []struct {
		attribute string
		scope     string
		value     types.Set
	}{
		{"group_ids", routingScopeGroups, config.GroupIDs},
		{"monitor_ids", routingScopeMonitors, config.MonitorIDs},
	} {
		switch {
		case scope == list.scope && list.value.IsNull():
			resp.Diagnostics.AddAttributeError(path.Root(list.attribute), "Missing "+list.attribute,
				fmt.Sprintf("%s is required when routing_scope = %q.", list.attribute, list.scope))
		case scope != list.scope && !list.value.IsNull():
			resp.Diagnostics.AddAttributeError(path.Root(list.attribute), "Unexpected "+list.attribute,
				fmt.Sprintf("%s can only be set when routing_scope = %q (routing_scope is %q).", list.attribute, list.scope, scope))
		}
	}
}

func (r *alertChannelResource) Configure(_ context.Context, req resource.ConfigureRequest, resp *resource.ConfigureResponse) {
	r.client = configureClient(req, resp)
}

func (r *alertChannelResource) body(ctx context.Context, plan alertChannelModel, diags *diag.Diagnostics) map[string]any {
	config := map[string]string{}
	diags.Append(plan.Config.ElementsAs(ctx, &config, false)...)
	body := map[string]any{}
	for key, value := range config {
		body[key] = value
	}
	body["name"] = plan.Name.ValueString()
	scope := plan.RoutingScope.ValueString()
	routing := map[string]any{"scope": scope}
	switch scope {
	case routingScopeGroups:
		routing["group_ids"] = routingTargets(ctx, plan.GroupIDs, diags)
	case routingScopeMonitors:
		routing["monitor_ids"] = routingTargets(ctx, plan.MonitorIDs, diags)
	}
	body["routing"] = routing
	return body
}

// routingTargets is the id list of a routing scope for the request body.
func routingTargets(ctx context.Context, set types.Set, diags *diag.Diagnostics) []string {
	ids := []string{}
	if !set.IsNull() && !set.IsUnknown() {
		diags.Append(set.ElementsAs(ctx, &ids, false)...)
	}
	return ids
}

// find returns the connection with the given id, or nil when it no longer exists.
func (r *alertChannelResource) find(ctx context.Context, id string, diags *diag.Diagnostics) *client.Connection {
	var data client.IntegrationsResponse
	if err := r.client.Get(ctx, "/integrations", &data); err != nil {
		apiErrorDiag(diags, "Could not list alert channels", err)
		return nil
	}
	for i := range data.Connections {
		if data.Connections[i].ID == id {
			return &data.Connections[i]
		}
	}
	return nil
}

func applyConnection(ctx context.Context, connection client.Connection, model *alertChannelModel, diags *diag.Diagnostics) {
	model.ID = types.StringValue(connection.ID)
	model.Type = types.StringValue(connection.IntegrationType)
	model.Name = types.StringValue(connection.Name)
	model.Status = types.StringValue(connection.Status)
	// The scope is stored as reported (not folded into "all"), so routing
	// changed in the dashboard shows up as drift instead of being
	// overwritten silently by the next apply.
	scope := connection.Routing.Scope
	if scope == "" {
		scope = routingScopeAll
	}
	model.RoutingScope = types.StringValue(scope)
	model.GroupIDs = types.SetNull(types.StringType)
	model.MonitorIDs = types.SetNull(types.StringType)
	switch scope {
	case routingScopeGroups:
		model.GroupIDs = stringSetValue(ctx, connection.Routing.GroupIDs, diags)
	case routingScopeMonitors:
		model.MonitorIDs = stringSetValue(ctx, connection.Routing.MonitorIDs, diags)
	}
	if model.Config.IsNull() || model.Config.IsUnknown() {
		// Imported: secrets cannot be read back; config must be set in HCL.
		empty, d := types.MapValueFrom(ctx, types.StringType, map[string]string{})
		diags.Append(d...)
		model.Config = empty
	}
	if model.SigningSecret.IsUnknown() {
		model.SigningSecret = types.StringNull()
	}
}

func (r *alertChannelResource) Create(ctx context.Context, req resource.CreateRequest, resp *resource.CreateResponse) {
	var plan alertChannelModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	if resp.Diagnostics.HasError() {
		return
	}
	body := r.body(ctx, plan, &resp.Diagnostics)
	if resp.Diagnostics.HasError() {
		return
	}
	var saved client.ConnectionSaveResponse
	if err := r.client.Post(ctx, "/integrations/"+client.PathEscape(plan.Type.ValueString())+"/connections", body, &saved); err != nil {
		apiErrorDiag(&resp.Diagnostics, "Could not create the alert channel", err)
		return
	}
	plan.SigningSecret = stringOrNull(saved.SigningSecret)
	connection := r.find(ctx, saved.ConnectionID, &resp.Diagnostics)
	if connection == nil {
		if !resp.Diagnostics.HasError() {
			resp.Diagnostics.AddError("Alert channel not found after create", "SutramX reported the channel as created but it is not listed; it may be a hidden channel type.")
		}
		return
	}
	applyConnection(ctx, *connection, &plan, &resp.Diagnostics)
	resp.Diagnostics.Append(resp.State.Set(ctx, &plan)...)
}

func (r *alertChannelResource) Read(ctx context.Context, req resource.ReadRequest, resp *resource.ReadResponse) {
	var state alertChannelModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}
	connection := r.find(ctx, state.ID.ValueString(), &resp.Diagnostics)
	if resp.Diagnostics.HasError() {
		return
	}
	if connection == nil {
		resp.State.RemoveResource(ctx)
		return
	}
	applyConnection(ctx, *connection, &state, &resp.Diagnostics)
	resp.Diagnostics.Append(resp.State.Set(ctx, &state)...)
}

func (r *alertChannelResource) Update(ctx context.Context, req resource.UpdateRequest, resp *resource.UpdateResponse) {
	var plan, state alertChannelModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}
	body := r.body(ctx, plan, &resp.Diagnostics)
	if resp.Diagnostics.HasError() {
		return
	}
	if err := r.client.Put(ctx, "/integrations/connections/"+client.PathEscape(state.ID.ValueString()), body, nil); err != nil {
		apiErrorDiag(&resp.Diagnostics, "Could not update the alert channel", err)
		return
	}
	plan.ID = state.ID
	plan.SigningSecret = state.SigningSecret
	connection := r.find(ctx, state.ID.ValueString(), &resp.Diagnostics)
	if connection == nil {
		if !resp.Diagnostics.HasError() {
			resp.Diagnostics.AddError("Alert channel not found after update", "The channel disappeared while it was being updated.")
		}
		return
	}
	applyConnection(ctx, *connection, &plan, &resp.Diagnostics)
	resp.Diagnostics.Append(resp.State.Set(ctx, &plan)...)
}

func (r *alertChannelResource) Delete(ctx context.Context, req resource.DeleteRequest, resp *resource.DeleteResponse) {
	var state alertChannelModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}
	if err := r.client.Delete(ctx, "/integrations/connections/"+client.PathEscape(state.ID.ValueString())); err != nil && !client.IsNotFound(err) {
		apiErrorDiag(&resp.Diagnostics, "Could not delete the alert channel", err)
	}
}

func (r *alertChannelResource) ImportState(ctx context.Context, req resource.ImportStateRequest, resp *resource.ImportStateResponse) {
	if !uuidPattern.MatchString(req.ID) {
		resp.Diagnostics.AddError("Invalid import ID", "Use the alert channel id (a UUID).")
		return
	}
	resource.ImportStatePassthroughID(ctx, path.Root("id"), req, resp)
}
