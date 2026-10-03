package provider

import (
	"context"
	"encoding/json"
	"fmt"
	"regexp"

	"github.com/hashicorp/terraform-plugin-framework-validators/int64validator"
	"github.com/hashicorp/terraform-plugin-framework-validators/setvalidator"
	"github.com/hashicorp/terraform-plugin-framework-validators/stringvalidator"
	"github.com/hashicorp/terraform-plugin-framework/diag"
	"github.com/hashicorp/terraform-plugin-framework/path"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/booldefault"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/int64planmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/planmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/setplanmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/stringdefault"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/stringplanmodifier"
	"github.com/hashicorp/terraform-plugin-framework/schema/validator"
	"github.com/hashicorp/terraform-plugin-framework/types"

	"github.com/sutramx/terraform-provider-sutramx/internal/client"
)

var (
	_ resource.Resource                = (*monitorResource)(nil)
	_ resource.ResourceWithConfigure   = (*monitorResource)(nil)
	_ resource.ResourceWithImportState = (*monitorResource)(nil)
)

var (
	monitorKeyPattern = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._:/-]{0,127}$`)
	uuidPattern       = regexp.MustCompile(`^[0-9a-fA-F]{8}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{12}$`)
)

// Server-managed config keys (owner-only per-monitor recipients): ignored in
// config_json unless the configuration sets them (which needs an owner or
// automation-access key).
var serverManagedConfigKeys = []string{"notification_emails"}

func NewMonitorResource() resource.Resource { return &monitorResource{} }

type monitorResource struct {
	client *client.Client
}

type monitorModel struct {
	ID              types.String `tfsdk:"id"`
	Key             types.String `tfsdk:"key"`
	Name            types.String `tfsdk:"name"`
	Type            types.String `tfsdk:"type"`
	URL             types.String `tfsdk:"url"`
	IntervalSeconds types.Int64  `tfsdk:"interval_seconds"`
	ConfigJSON      types.String `tfsdk:"config_json"`
	Tags            types.Set    `tfsdk:"tags"`
	Regions         types.Set    `tfsdk:"regions"`
	Paused          types.Bool   `tfsdk:"paused"`
	HeartbeatURL    types.String `tfsdk:"heartbeat_url"`
}

func (r *monitorResource) Metadata(_ context.Context, req resource.MetadataRequest, resp *resource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_monitor"
}

func (r *monitorResource) Schema(_ context.Context, _ resource.SchemaRequest, resp *resource.SchemaResponse) {
	resp.Schema = schema.Schema{
		MarkdownDescription: "An uptime monitor. Checks run from SutramX probe regions and incidents open only when several regions agree. " +
			"Plan limits (monitor count, minimum interval, locations) are enforced by the API.",
		Attributes: map[string]schema.Attribute{
			"id": schema.StringAttribute{
				MarkdownDescription: "Monitor id (UUID).",
				Computed:            true,
				PlanModifiers:       []planmodifier.String{stringplanmodifier.UseStateForUnknown()},
			},
			"key": schema.StringAttribute{
				MarkdownDescription: "Stable key, unique in the workspace (the same `key` sutramx.yml uses). Creates are idempotent by key. " +
					"Generated (`tf-...`) when not set. Letters, digits and `. _ : / -`, up to 128 characters.",
				Optional: true,
				Computed: true,
				// Non-null only: an imported dashboard monitor has no key until the
				// first update gives it one, so a null key must stay "known after apply".
				PlanModifiers: []planmodifier.String{stringplanmodifier.UseNonNullStateForUnknown()},
				Validators:    []validator.String{stringvalidator.RegexMatches(monitorKeyPattern, "must be 1-128 letters, digits or . _ : / -, starting with a letter or digit")},
			},
			"name": schema.StringAttribute{
				MarkdownDescription: "Display name.",
				Required:            true,
				Validators:          []validator.String{stringvalidator.LengthBetween(1, 255)},
			},
			"type": schema.StringAttribute{
				MarkdownDescription: "Monitor type: `http`, `api`, `ping`, `port`, `udp` or `cron` (or a newer type the account supports). Changing it replaces the monitor.",
				Optional:            true,
				Computed:            true,
				Default:             stringdefault.StaticString("http"),
				PlanModifiers:       []planmodifier.String{stringplanmodifier.RequiresReplace()},
			},
			"url": schema.StringAttribute{
				MarkdownDescription: "Target URL for `http` and `api` monitors. Ping, port and UDP monitors use `host` in `config_json`.",
				Optional:            true,
			},
			"interval_seconds": schema.Int64Attribute{
				MarkdownDescription: "Seconds between checks (15-900, not below the plan minimum). Defaults to the plan default.",
				Optional:            true,
				Computed:            true,
				PlanModifiers:       []planmodifier.Int64{int64planmodifier.UseStateForUnknown()},
				Validators:          []validator.Int64{int64validator.Between(15, 900)},
			},
			"config_json": schema.StringAttribute{
				MarkdownDescription: "Type-specific settings as a JSON object, e.g. `jsonencode({ timeout = 10000, expected_status_codes = [200] })`. " +
					"Managed as a whole when set; left untouched when omitted (it then shows the stored settings, in `jsonencode` form). " +
					"Cron monitors need `cron_expression`; ping/port/UDP monitors need `host` (and `port`). " +
					"Stored credentials (sensitive headers, tokens, passwords) are read back masked as `[REDACTED]`.",
				Optional:      true,
				Computed:      true,
				PlanModifiers: []planmodifier.String{stringplanmodifier.UseStateForUnknown()},
			},
			"tags": schema.SetAttribute{
				MarkdownDescription: "Tags (stored lower-case). Left untouched when omitted.",
				ElementType:         types.StringType,
				Optional:            true,
				Computed:            true,
				PlanModifiers:       []planmodifier.Set{setplanmodifier.UseStateForUnknown()},
				Validators: []validator.Set{
					setvalidator.SizeAtMost(20),
					setvalidator.ValueStringsAre(stringvalidator.RegexMatches(regexp.MustCompile(`^[^A-Z]{1,32}$`), "tags are stored lower-case: write them in lower case (1-32 characters)")),
				},
			},
			"regions": schema.SetAttribute{
				MarkdownDescription: "Probe location codes (see the `sutramx_regions` data source). Order does not matter. Omit to use the plan's default locations.",
				ElementType:         types.StringType,
				Optional:            true,
				Validators: []validator.Set{
					setvalidator.SizeAtLeast(1),
					setvalidator.ValueStringsAre(stringvalidator.RegexMatches(regexp.MustCompile(`^[a-z0-9][a-z0-9-]{0,19}$`), "region codes are lower-case, e.g. \"bom\"")),
				},
			},
			"paused": schema.BoolAttribute{
				MarkdownDescription: "Pause checks. Defaults to `false`.",
				Optional:            true,
				Computed:            true,
				Default:             booldefault.StaticBool(false),
			},
			"heartbeat_url": schema.StringAttribute{
				MarkdownDescription: "For `cron` monitors: the URL your job calls on each run.",
				Computed:            true,
				Sensitive:           true,
				PlanModifiers:       []planmodifier.String{stringplanmodifier.UseStateForUnknown()},
			},
		},
	}
}

func (r *monitorResource) Configure(_ context.Context, req resource.ConfigureRequest, resp *resource.ConfigureResponse) {
	r.client = configureClient(req, resp)
}

// spec builds the PUT /automation/monitors/:key body. Omitted optional
// fields are not managed by the API (they keep their value). config is sent
// only when the configuration sets config_json: when it is omitted the plan
// carries the stored settings (computed), which must not be written back.
func (r *monitorResource) spec(ctx context.Context, plan monitorModel, configured types.String, diags *diag.Diagnostics) map[string]any {
	body := map[string]any{
		"name":   plan.Name.ValueString(),
		"type":   plan.Type.ValueString(),
		"paused": plan.Paused.ValueBool(),
	}
	if !plan.URL.IsNull() && !plan.URL.IsUnknown() {
		body["url"] = plan.URL.ValueString()
	}
	if !plan.IntervalSeconds.IsNull() && !plan.IntervalSeconds.IsUnknown() {
		body["interval_seconds"] = plan.IntervalSeconds.ValueInt64()
	}
	if !configured.IsNull() && !plan.ConfigJSON.IsNull() && !plan.ConfigJSON.IsUnknown() {
		var config map[string]any
		if err := json.Unmarshal([]byte(plan.ConfigJSON.ValueString()), &config); err != nil {
			diags.AddAttributeError(path.Root("config_json"), "Invalid config_json", "config_json must be a JSON object: "+err.Error())
			return nil
		}
		body["config"] = config
	}
	if !plan.Tags.IsNull() && !plan.Tags.IsUnknown() {
		var tags []string
		diags.Append(plan.Tags.ElementsAs(ctx, &tags, false)...)
		body["tags"] = tags
	}
	if plan.Regions.IsNull() {
		body["regions"] = nil
	} else if !plan.Regions.IsUnknown() {
		var regions []string
		diags.Append(plan.Regions.ElementsAs(ctx, &regions, false)...)
		body["regions"] = regions
	}
	return body
}

// applyMonitor copies the API object into the model, keeping the user's
// config_json formatting when it is semantically unchanged. Every attribute
// is filled in, in the form a configuration writes it (config_json as
// jsonencode() renders it, regions as a set), so an imported monitor plans
// no changes against a configuration that describes it.
func applyMonitor(ctx context.Context, monitor client.Monitor, model *monitorModel, diags *diag.Diagnostics) {
	model.ID = types.StringValue(monitor.ID)
	if monitor.ExternalID != nil && *monitor.ExternalID != "" {
		model.Key = types.StringValue(*monitor.ExternalID)
	} else {
		model.Key = types.StringNull()
	}
	model.Name = types.StringValue(monitor.Name)
	model.Type = types.StringValue(monitor.Type)
	if monitor.URL != nil && *monitor.URL != "" {
		// The API lower-cases only the scheme and host, and masks a URL
		// password ([REDACTED]); keep the user's spelling and credentials.
		if model.URL.IsNull() || model.URL.IsUnknown() || !keepPriorURL(model.URL.ValueString(), *monitor.URL) {
			model.URL = types.StringValue(*monitor.URL)
		}
	} else {
		model.URL = types.StringNull()
	}
	model.IntervalSeconds = types.Int64Value(monitor.IntervalSeconds)
	model.Paused = types.BoolValue(!monitor.IsActive)
	model.Tags = stringSetValue(ctx, monitor.Tags, diags)
	if len(monitor.ProbeRegions) > 0 {
		model.Regions = stringSetValue(ctx, monitor.ProbeRegions, diags)
	} else {
		model.Regions = types.SetNull(types.StringType)
	}
	if !model.ConfigJSON.IsNull() && !model.ConfigJSON.IsUnknown() {
		remoteConfig := withoutKeys(monitor.Config, undeclaredKeys(model.ConfigJSON.ValueString(), serverManagedConfigKeys)...)
		// Stored credentials come back masked: keep the configured values there.
		remoteConfig = keepPriorSecrets(remoteConfig, model.ConfigJSON.ValueString())
		if !jsonEqual(model.ConfigJSON.ValueString(), remoteConfig) {
			model.ConfigJSON = types.StringValue(canonicalJSON(remoteConfig))
		}
	} else {
		// Not known yet (create, import): the stored settings without the
		// owner-managed keys, which a configuration only sets deliberately.
		model.ConfigJSON = types.StringValue(canonicalJSON(withoutKeys(monitor.Config, serverManagedConfigKeys...)))
	}
	model.HeartbeatURL = stringOrNull(monitor.HeartbeatURL)
}

func (r *monitorResource) upsert(ctx context.Context, key string, plan monitorModel, configured types.String, diags *diag.Diagnostics) *client.Monitor {
	body := r.spec(ctx, plan, configured, diags)
	if diags.HasError() {
		return nil
	}
	var out client.MonitorUpsertResponse
	if err := r.client.Put(ctx, "/automation/monitors/"+client.PathEscape(key), body, &out); err != nil {
		apiErrorDiag(diags, "Could not save the monitor", err)
		return nil
	}
	return &out.Monitor
}

func (r *monitorResource) Create(ctx context.Context, req resource.CreateRequest, resp *resource.CreateResponse) {
	var plan monitorModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	if resp.Diagnostics.HasError() {
		return
	}
	var configured types.String
	resp.Diagnostics.Append(req.Config.GetAttribute(ctx, path.Root("config_json"), &configured)...)
	if resp.Diagnostics.HasError() {
		return
	}
	key := plan.Key.ValueString()
	if plan.Key.IsUnknown() || plan.Key.IsNull() || key == "" {
		key = randomKey("tf-")
	}
	monitor := r.upsert(ctx, key, plan, configured, &resp.Diagnostics)
	if monitor == nil {
		return
	}
	applyMonitor(ctx, *monitor, &plan, &resp.Diagnostics)
	resp.Diagnostics.Append(resp.State.Set(ctx, &plan)...)
}

func (r *monitorResource) Read(ctx context.Context, req resource.ReadRequest, resp *resource.ReadResponse) {
	var state monitorModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}
	var monitor client.Monitor
	if err := r.client.Get(ctx, "/monitors/"+client.PathEscape(state.ID.ValueString()), &monitor); err != nil {
		if client.IsNotFound(err) {
			resp.State.RemoveResource(ctx)
			return
		}
		apiErrorDiag(&resp.Diagnostics, "Could not read the monitor", err)
		return
	}
	applyMonitor(ctx, monitor, &state, &resp.Diagnostics)
	resp.Diagnostics.Append(resp.State.Set(ctx, &state)...)
}

func (r *monitorResource) Update(ctx context.Context, req resource.UpdateRequest, resp *resource.UpdateResponse) {
	var plan, state monitorModel
	var configured types.String
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	resp.Diagnostics.Append(req.Config.GetAttribute(ctx, path.Root("config_json"), &configured)...)
	if resp.Diagnostics.HasError() {
		return
	}
	key := plan.Key.ValueString()
	if plan.Key.IsUnknown() || plan.Key.IsNull() || key == "" {
		key = state.Key.ValueString()
	}
	if key == "" {
		// Imported dashboard monitor without a key yet.
		key = "tf-" + state.ID.ValueString()[:8]
	}
	// Re-key (or first key) the existing monitor before the upsert, so the
	// upsert finds it instead of creating a second one.
	if state.Key.ValueString() != key {
		var monitor client.Monitor
		if err := r.client.Put(ctx, fmt.Sprintf("/automation/monitors/by-id/%s/key", client.PathEscape(state.ID.ValueString())), map[string]any{"key": key}, &monitor); err != nil {
			apiErrorDiag(&resp.Diagnostics, "Could not set the monitor key", err)
			return
		}
	}
	monitor := r.upsert(ctx, key, plan, configured, &resp.Diagnostics)
	if monitor == nil {
		return
	}
	applyMonitor(ctx, *monitor, &plan, &resp.Diagnostics)
	resp.Diagnostics.Append(resp.State.Set(ctx, &plan)...)
}

func (r *monitorResource) Delete(ctx context.Context, req resource.DeleteRequest, resp *resource.DeleteResponse) {
	var state monitorModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}
	if err := r.client.Delete(ctx, "/monitors/"+client.PathEscape(state.ID.ValueString())); err != nil && !client.IsNotFound(err) {
		apiErrorDiag(&resp.Diagnostics, "Could not delete the monitor", err)
	}
}

// ImportState accepts a monitor id (UUID) or a monitor key. Read then fills
// in every attribute (config_json included) in configuration form.
func (r *monitorResource) ImportState(ctx context.Context, req resource.ImportStateRequest, resp *resource.ImportStateResponse) {
	var monitor client.Monitor
	if !uuidPattern.MatchString(req.ID) && !monitorKeyPattern.MatchString(req.ID) {
		resp.Diagnostics.AddError("Invalid import ID", "Use a monitor id (UUID) or a monitor key (1-128 letters, digits or . _ : / -, starting with a letter or digit).")
		return
	}
	lookup := "/monitors/" + client.PathEscape(req.ID)
	if !uuidPattern.MatchString(req.ID) {
		lookup = "/automation/monitors/" + client.PathEscape(req.ID)
	}
	if err := r.client.Get(ctx, lookup, &monitor); err != nil {
		apiErrorDiag(&resp.Diagnostics, "Could not find a monitor with that id or key", err)
		return
	}
	resp.Diagnostics.Append(resp.State.SetAttribute(ctx, path.Root("id"), monitor.ID)...)
}
