package provider

import (
	"context"
	"regexp"

	"github.com/hashicorp/terraform-plugin-framework-validators/stringvalidator"
	"github.com/hashicorp/terraform-plugin-framework/attr"
	"github.com/hashicorp/terraform-plugin-framework/diag"
	"github.com/hashicorp/terraform-plugin-framework/path"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/boolplanmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/listplanmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/planmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/stringplanmodifier"
	"github.com/hashicorp/terraform-plugin-framework/schema/validator"
	"github.com/hashicorp/terraform-plugin-framework/types"

	"github.com/sutramx/terraform-provider-sutramx/internal/client"
)

var (
	_ resource.Resource                = (*statusPageResource)(nil)
	_ resource.ResourceWithConfigure   = (*statusPageResource)(nil)
	_ resource.ResourceWithImportState = (*statusPageResource)(nil)
)

func NewStatusPageResource() resource.Resource { return &statusPageResource{} }

type statusPageResource struct {
	client *client.Client
}

type statusPageModel struct {
	ID                types.String `tfsdk:"id"`
	Title             types.String `tfsdk:"title"`
	Slug              types.String `tfsdk:"slug"`
	Description       types.String `tfsdk:"description"`
	IsPublic          types.Bool   `tfsdk:"is_public"`
	LogoURL           types.String `tfsdk:"logo_url"`
	AccentColor       types.String `tfsdk:"accent_color"`
	ShowResponseTimes types.Bool   `tfsdk:"show_response_times"`
	HidePoweredBy     types.Bool   `tfsdk:"hide_powered_by"`
	Monitors          types.List   `tfsdk:"monitors"`
}

type statusPageMonitorModel struct {
	MonitorID types.String `tfsdk:"monitor_id"`
	Section   types.String `tfsdk:"section"`
}

var statusPageMonitorAttrTypes = map[string]attr.Type{
	"monitor_id": types.StringType,
	"section":    types.StringType,
}

func (r *statusPageResource) Metadata(_ context.Context, req resource.MetadataRequest, resp *resource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_status_page"
}

func (r *statusPageResource) Schema(_ context.Context, _ resource.SchemaRequest, resp *resource.SchemaResponse) {
	resp.Schema = schema.Schema{
		MarkdownDescription: "A public status page showing the state of selected monitors. Counts against the plan's status page limit. " +
			"Custom domains are set up in the dashboard by the workspace owner.",
		Attributes: map[string]schema.Attribute{
			"id": schema.StringAttribute{
				Computed:      true,
				PlanModifiers: []planmodifier.String{stringplanmodifier.UseStateForUnknown()},
			},
			"title": schema.StringAttribute{
				Required:   true,
				Validators: []validator.String{stringvalidator.LengthBetween(1, 255)},
			},
			"slug": schema.StringAttribute{
				MarkdownDescription: "URL slug (lower-case letters, digits, hyphens). Generated from the title when not set.",
				Optional:            true,
				Computed:            true,
				PlanModifiers:       []planmodifier.String{stringplanmodifier.UseStateForUnknown()},
				Validators:          []validator.String{stringvalidator.RegexMatches(regexp.MustCompile(`^[a-z0-9-]{3,64}$`), "3-64 lower-case letters, digits or hyphens")},
			},
			"description": schema.StringAttribute{Optional: true, Validators: []validator.String{stringvalidator.LengthAtMost(1000)}},
			"is_public": schema.BoolAttribute{
				MarkdownDescription: "Whether the page is published.",
				Optional:            true,
				Computed:            true,
				PlanModifiers:       []planmodifier.Bool{boolplanmodifier.UseStateForUnknown()},
			},
			"logo_url":     schema.StringAttribute{Optional: true, MarkdownDescription: "https:// image URL.", Validators: []validator.String{stringvalidator.RegexMatches(regexp.MustCompile(`^https://\S+$`), "must be an https:// URL")}},
			"accent_color": schema.StringAttribute{Optional: true, MarkdownDescription: "Hex colour like `#0d9488`.", Validators: []validator.String{stringvalidator.RegexMatches(regexp.MustCompile(`^#[0-9a-fA-F]{6}$`), "use a hex colour like #0d9488")}},
			"show_response_times": schema.BoolAttribute{
				Optional:      true,
				Computed:      true,
				PlanModifiers: []planmodifier.Bool{boolplanmodifier.UseStateForUnknown()},
			},
			"hide_powered_by": schema.BoolAttribute{
				MarkdownDescription: "Remove SutramX branding (plans with white-label status pages).",
				Optional:            true,
				Computed:            true,
				PlanModifiers:       []planmodifier.Bool{boolplanmodifier.UseStateForUnknown()},
			},
			"monitors": schema.ListNestedAttribute{
				MarkdownDescription: "Monitors shown on the page, in display order. Omit to manage them in the dashboard (the attribute then shows the current list).",
				Optional:            true,
				Computed:            true,
				PlanModifiers:       []planmodifier.List{listplanmodifier.UseStateForUnknown()},
				NestedObject: schema.NestedAttributeObject{
					Attributes: map[string]schema.Attribute{
						"monitor_id": schema.StringAttribute{Required: true, MarkdownDescription: "`sutramx_monitor.<name>.id`"},
						"section":    schema.StringAttribute{Optional: true, MarkdownDescription: "Optional heading the monitor is grouped under."},
					},
				},
			},
		},
	}
}

func (r *statusPageResource) Configure(_ context.Context, req resource.ConfigureRequest, resp *resource.ConfigureResponse) {
	r.client = configureClient(req, resp)
}

func optionalString(value types.String) any {
	if value.IsNull() {
		return nil
	}
	return value.ValueString()
}

// patchBody holds the settings PATCH /status/pages/:id accepts.
func (r *statusPageResource) patchBody(plan statusPageModel, includeSlug bool) map[string]any {
	body := map[string]any{
		"title":        plan.Title.ValueString(),
		"description":  optionalString(plan.Description),
		"logo_url":     optionalString(plan.LogoURL),
		"accent_color": optionalString(plan.AccentColor),
	}
	if includeSlug && !plan.Slug.IsNull() && !plan.Slug.IsUnknown() {
		body["slug"] = plan.Slug.ValueString()
	}
	if !plan.IsPublic.IsNull() && !plan.IsPublic.IsUnknown() {
		body["is_public"] = plan.IsPublic.ValueBool()
	}
	if !plan.ShowResponseTimes.IsNull() && !plan.ShowResponseTimes.IsUnknown() {
		body["show_response_times"] = plan.ShowResponseTimes.ValueBool()
	}
	if !plan.HidePoweredBy.IsNull() && !plan.HidePoweredBy.IsUnknown() {
		body["hide_powered_by"] = plan.HidePoweredBy.ValueBool()
	}
	return body
}

// setMonitors replaces the page's monitor list with the configured one. A
// configuration without monitors leaves the list alone (the plan then holds
// the current list, computed).
func (r *statusPageResource) setMonitors(ctx context.Context, id string, configured types.List, diags *diag.Diagnostics) {
	if configured.IsNull() || configured.IsUnknown() {
		return
	}
	var entries []statusPageMonitorModel
	diags.Append(configured.ElementsAs(ctx, &entries, false)...)
	if diags.HasError() {
		return
	}
	items := make([]map[string]any, 0, len(entries))
	for _, entry := range entries {
		items = append(items, map[string]any{"monitor_id": entry.MonitorID.ValueString(), "section": optionalString(entry.Section)})
	}
	if err := r.client.Put(ctx, "/status/pages/"+client.PathEscape(id)+"/monitors", map[string]any{"monitors": items}, nil); err != nil {
		apiErrorDiag(diags, "Could not set the status page monitors", err)
	}
}

func applyStatusPage(ctx context.Context, page client.StatusPage, model *statusPageModel, diags *diag.Diagnostics) {
	model.ID = types.StringValue(page.ID)
	model.Title = types.StringValue(page.Title)
	model.Slug = types.StringValue(page.Slug)
	model.Description = stringOrNull(page.Description)
	model.IsPublic = types.BoolValue(page.IsPublic)
	model.LogoURL = stringOrNull(page.LogoURL)
	model.AccentColor = stringOrNull(page.AccentColor)
	model.ShowResponseTimes = types.BoolValue(page.ShowResponseTimes)
	model.HidePoweredBy = types.BoolValue(page.IsWhitelabel)
	// Always filled in (also when imported or not managed), so a
	// configuration listing the page's monitors plans no changes.
	values := make([]attr.Value, 0, len(page.Monitors))
	for _, monitor := range page.Monitors {
		object, d := types.ObjectValue(statusPageMonitorAttrTypes, map[string]attr.Value{
			"monitor_id": types.StringValue(monitor.ID),
			"section":    stringOrNull(monitor.Section),
		})
		diags.Append(d...)
		values = append(values, object)
	}
	list, d := types.ListValue(types.ObjectType{AttrTypes: statusPageMonitorAttrTypes}, values)
	diags.Append(d...)
	model.Monitors = list
}

func (r *statusPageResource) read(ctx context.Context, id string, model *statusPageModel, diags *diag.Diagnostics) bool {
	var page client.StatusPage
	if err := r.client.Get(ctx, "/status/pages/"+client.PathEscape(id), &page); err != nil {
		if client.IsNotFound(err) {
			return false
		}
		apiErrorDiag(diags, "Could not read the status page", err)
		return true
	}
	applyStatusPage(ctx, page, model, diags)
	return true
}

func (r *statusPageResource) Create(ctx context.Context, req resource.CreateRequest, resp *resource.CreateResponse) {
	var plan statusPageModel
	var configuredMonitors types.List
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	resp.Diagnostics.Append(req.Config.GetAttribute(ctx, path.Root("monitors"), &configuredMonitors)...)
	if resp.Diagnostics.HasError() {
		return
	}
	createBody := map[string]any{"title": plan.Title.ValueString()}
	if !plan.Description.IsNull() {
		createBody["description"] = plan.Description.ValueString()
	}
	if !plan.IsPublic.IsNull() && !plan.IsPublic.IsUnknown() {
		createBody["is_public"] = plan.IsPublic.ValueBool()
	}
	var created client.StatusPage
	if err := r.client.Post(ctx, "/status/pages", createBody, &created); err != nil {
		apiErrorDiag(&resp.Diagnostics, "Could not create the status page", err)
		return
	}
	if err := r.client.Patch(ctx, "/status/pages/"+client.PathEscape(created.ID), r.patchBody(plan, true), nil); err != nil {
		// Do not leave a half-configured page behind (e.g. the slug is taken).
		_ = r.client.Delete(ctx, "/status/pages/"+client.PathEscape(created.ID))
		apiErrorDiag(&resp.Diagnostics, "Could not configure the status page", err)
		return
	}
	r.setMonitors(ctx, created.ID, configuredMonitors, &resp.Diagnostics)
	if resp.Diagnostics.HasError() {
		_ = r.client.Delete(ctx, "/status/pages/"+client.PathEscape(created.ID))
		return
	}
	r.read(ctx, created.ID, &plan, &resp.Diagnostics)
	resp.Diagnostics.Append(resp.State.Set(ctx, &plan)...)
}

func (r *statusPageResource) Read(ctx context.Context, req resource.ReadRequest, resp *resource.ReadResponse) {
	var state statusPageModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}
	if !r.read(ctx, state.ID.ValueString(), &state, &resp.Diagnostics) {
		resp.State.RemoveResource(ctx)
		return
	}
	resp.Diagnostics.Append(resp.State.Set(ctx, &state)...)
}

func (r *statusPageResource) Update(ctx context.Context, req resource.UpdateRequest, resp *resource.UpdateResponse) {
	var plan, state statusPageModel
	var configuredMonitors types.List
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	resp.Diagnostics.Append(req.Config.GetAttribute(ctx, path.Root("monitors"), &configuredMonitors)...)
	if resp.Diagnostics.HasError() {
		return
	}
	id := state.ID.ValueString()
	includeSlug := !plan.Slug.IsUnknown() && plan.Slug.ValueString() != state.Slug.ValueString()
	if err := r.client.Patch(ctx, "/status/pages/"+client.PathEscape(id), r.patchBody(plan, includeSlug), nil); err != nil {
		apiErrorDiag(&resp.Diagnostics, "Could not update the status page", err)
		return
	}
	r.setMonitors(ctx, id, configuredMonitors, &resp.Diagnostics)
	if resp.Diagnostics.HasError() {
		return
	}
	r.read(ctx, id, &plan, &resp.Diagnostics)
	resp.Diagnostics.Append(resp.State.Set(ctx, &plan)...)
}

func (r *statusPageResource) Delete(ctx context.Context, req resource.DeleteRequest, resp *resource.DeleteResponse) {
	var state statusPageModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}
	if err := r.client.Delete(ctx, "/status/pages/"+client.PathEscape(state.ID.ValueString())); err != nil && !client.IsNotFound(err) {
		apiErrorDiag(&resp.Diagnostics, "Could not delete the status page", err)
	}
}

func (r *statusPageResource) ImportState(ctx context.Context, req resource.ImportStateRequest, resp *resource.ImportStateResponse) {
	if !uuidPattern.MatchString(req.ID) {
		resp.Diagnostics.AddError("Invalid import ID", "Use the status page id (a UUID).")
		return
	}
	resource.ImportStatePassthroughID(ctx, path.Root("id"), req, resp)
}
