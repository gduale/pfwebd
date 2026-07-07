package provider

import (
	"context"
	"fmt"

	"github.com/hashicorp/terraform-plugin-framework/path"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/planmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/stringplanmodifier"
	"github.com/hashicorp/terraform-plugin-framework/types"

	"github.com/gduale/terraform-provider-pfwebd/internal/client"
)

var (
	_ resource.Resource                = (*anchorRulesetResource)(nil)
	_ resource.ResourceWithConfigure   = (*anchorRulesetResource)(nil)
	_ resource.ResourceWithImportState = (*anchorRulesetResource)(nil)
)

func NewAnchorRulesetResource() resource.Resource {
	return &anchorRulesetResource{}
}

// anchorRulesetResource manages the full ordered ruleset of the "pfwebd"
// PF anchor. It is a singleton: declare at most one per pfwebd server.
type anchorRulesetResource struct {
	client *client.Client
}

type anchorRulesetModel struct {
	ID    types.String   `tfsdk:"id"`
	Rules []types.String `tfsdk:"rules"`
}

func (r *anchorRulesetResource) Metadata(_ context.Context, req resource.MetadataRequest, resp *resource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_anchor_ruleset"
}

func (r *anchorRulesetResource) Schema(_ context.Context, _ resource.SchemaRequest, resp *resource.SchemaResponse) {
	resp.Schema = schema.Schema{
		Description: "Manages the complete, ordered ruleset of the PF anchor dedicated to pfwebd " +
			"(anchor \"pfwebd\"). Rules are validated by pfctl, applied, then confirmed; if the new " +
			"ruleset cuts the provider's connectivity, the confirmation never reaches the firewall " +
			"and pfwebd rolls back automatically. Declare at most ONE instance of this resource " +
			"per pfwebd endpoint.",
		Attributes: map[string]schema.Attribute{
			"id": schema.StringAttribute{
				Computed:    true,
				Description: "Always \"pfwebd\" (the managed anchor name).",
				PlanModifiers: []planmodifier.String{
					stringplanmodifier.UseStateForUnknown(),
				},
			},
			"rules": schema.ListAttribute{
				ElementType: types.StringType,
				Required:    true,
				Description: "Ordered list of PF rules (one rule per element, pass/block/match only). " +
					"Rules are stored trimmed; do not include comments or empty lines.",
			},
		},
	}
}

func (r *anchorRulesetResource) Configure(_ context.Context, req resource.ConfigureRequest, _ *resource.ConfigureResponse) {
	if req.ProviderData == nil {
		return
	}
	r.client = req.ProviderData.(*client.Client)
}

// applyAndConfirm runs the full anti-lockout cycle. If the confirmation
// fails (e.g. connectivity lost), the pending change is cancelled or
// will expire server-side, restoring the previous ruleset.
func (r *anchorRulesetResource) applyAndConfirm(ctx context.Context, rules []string) error {
	if _, err := r.client.ApplyAnchor(ctx, rules); err != nil {
		return fmt.Errorf("apply: %w", err)
	}
	if _, err := r.client.ConfirmAnchor(ctx); err != nil {
		// Best effort: do not leave a pending change behind.
		_ = r.client.CancelAnchor(ctx)
		return fmt.Errorf("confirm: %w", err)
	}
	return nil
}

func (r *anchorRulesetResource) Create(ctx context.Context, req resource.CreateRequest, resp *resource.CreateResponse) {
	var plan anchorRulesetModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	if resp.Diagnostics.HasError() {
		return
	}
	if err := r.applyAndConfirm(ctx, fromTFStrings(plan.Rules)); err != nil {
		resp.Diagnostics.AddError("Unable to apply anchor ruleset", err.Error())
		return
	}
	plan.ID = types.StringValue("pfwebd")
	resp.Diagnostics.Append(resp.State.Set(ctx, &plan)...)
}

func (r *anchorRulesetResource) Read(ctx context.Context, req resource.ReadRequest, resp *resource.ReadResponse) {
	var state anchorRulesetModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}
	st, err := r.client.GetAnchor(ctx)
	if err != nil {
		resp.Diagnostics.AddError("Unable to read anchor ruleset", err.Error())
		return
	}
	state.ID = types.StringValue("pfwebd")
	state.Rules = toTFStrings(st.Active)
	resp.Diagnostics.Append(resp.State.Set(ctx, &state)...)
}

func (r *anchorRulesetResource) Update(ctx context.Context, req resource.UpdateRequest, resp *resource.UpdateResponse) {
	var plan anchorRulesetModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	if resp.Diagnostics.HasError() {
		return
	}
	if err := r.applyAndConfirm(ctx, fromTFStrings(plan.Rules)); err != nil {
		resp.Diagnostics.AddError("Unable to update anchor ruleset", err.Error())
		return
	}
	plan.ID = types.StringValue("pfwebd")
	resp.Diagnostics.Append(resp.State.Set(ctx, &plan)...)
}

func (r *anchorRulesetResource) Delete(ctx context.Context, _ resource.DeleteRequest, resp *resource.DeleteResponse) {
	if err := r.applyAndConfirm(ctx, nil); err != nil {
		resp.Diagnostics.AddError("Unable to flush anchor ruleset", err.Error())
	}
}

func (r *anchorRulesetResource) ImportState(ctx context.Context, req resource.ImportStateRequest, resp *resource.ImportStateResponse) {
	resource.ImportStatePassthroughID(ctx, path.Root("id"), req, resp)
}
