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
	_ resource.Resource                = (*tableResource)(nil)
	_ resource.ResourceWithConfigure   = (*tableResource)(nil)
	_ resource.ResourceWithImportState = (*tableResource)(nil)
)

func NewTableResource() resource.Resource {
	return &tableResource{}
}

// tableResource authoritatively manages the address set of one PF table.
// The table itself must be declared in pf.conf (table <name> persist)
// and listed in pfwebd's -rw-tables flag.
type tableResource struct {
	client *client.Client
}

type tableModel struct {
	ID        types.String   `tfsdk:"id"`
	Name      types.String   `tfsdk:"name"`
	Addresses []types.String `tfsdk:"addresses"`
}

func (r *tableResource) Metadata(_ context.Context, req resource.MetadataRequest, resp *resource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_table"
}

func (r *tableResource) Schema(_ context.Context, _ resource.SchemaRequest, resp *resource.SchemaResponse) {
	resp.Schema = schema.Schema{
		Description: "Authoritatively manages the addresses of a PF table. Addresses present in the " +
			"table but absent from the configuration are removed on apply. The table itself must be " +
			"declared in pf.conf (table <name> persist) and allowed in pfwebd's -rw-tables flag.",
		Attributes: map[string]schema.Attribute{
			"id": schema.StringAttribute{
				Computed:    true,
				Description: "Same as the table name.",
				PlanModifiers: []planmodifier.String{
					stringplanmodifier.UseStateForUnknown(),
				},
			},
			"name": schema.StringAttribute{
				Required:    true,
				Description: "PF table name (must exist in pf.conf and be listed in -rw-tables).",
				PlanModifiers: []planmodifier.String{
					stringplanmodifier.RequiresReplace(),
				},
			},
			"addresses": schema.SetAttribute{
				ElementType: types.StringType,
				Required:    true,
				Description: "Set of IP addresses and/or CIDR prefixes the table must contain.",
			},
		},
	}
}

func (r *tableResource) Configure(_ context.Context, req resource.ConfigureRequest, _ *resource.ConfigureResponse) {
	if req.ProviderData == nil {
		return
	}
	r.client = req.ProviderData.(*client.Client)
}

// reconcile makes the remote table contain exactly `want`.
func (r *tableResource) reconcile(ctx context.Context, name string, want []string) error {
	cur, err := r.client.GetTable(ctx, name)
	if err != nil {
		return fmt.Errorf("read table %q: %w", name, err)
	}
	wantSet := make(map[string]bool, len(want))
	for _, a := range want {
		wantSet[a] = true
	}
	curSet := make(map[string]bool, len(cur.Addresses))
	for _, a := range cur.Addresses {
		curSet[a] = true
	}
	for _, a := range want {
		if !curSet[a] {
			if err := r.client.TableAdd(ctx, name, a); err != nil {
				return fmt.Errorf("add %s: %w", a, err)
			}
		}
	}
	for _, a := range cur.Addresses {
		if !wantSet[a] {
			if err := r.client.TableDelete(ctx, name, a); err != nil {
				return fmt.Errorf("delete %s: %w", a, err)
			}
		}
	}
	return nil
}

func (r *tableResource) Create(ctx context.Context, req resource.CreateRequest, resp *resource.CreateResponse) {
	var plan tableModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	if resp.Diagnostics.HasError() {
		return
	}
	name := plan.Name.ValueString()
	if err := r.reconcile(ctx, name, fromTFStrings(plan.Addresses)); err != nil {
		resp.Diagnostics.AddError("Unable to create table content", err.Error())
		return
	}
	plan.ID = types.StringValue(name)
	resp.Diagnostics.Append(resp.State.Set(ctx, &plan)...)
}

func (r *tableResource) Read(ctx context.Context, req resource.ReadRequest, resp *resource.ReadResponse) {
	var state tableModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}
	name := state.Name.ValueString()
	if name == "" { // import by ID
		name = state.ID.ValueString()
	}
	t, err := r.client.GetTable(ctx, name)
	if err != nil {
		resp.Diagnostics.AddError("Unable to read table", err.Error())
		return
	}
	state.ID = types.StringValue(name)
	state.Name = types.StringValue(name)
	state.Addresses = toTFStrings(t.Addresses)
	resp.Diagnostics.Append(resp.State.Set(ctx, &state)...)
}

func (r *tableResource) Update(ctx context.Context, req resource.UpdateRequest, resp *resource.UpdateResponse) {
	var plan tableModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	if resp.Diagnostics.HasError() {
		return
	}
	name := plan.Name.ValueString()
	if err := r.reconcile(ctx, name, fromTFStrings(plan.Addresses)); err != nil {
		resp.Diagnostics.AddError("Unable to update table content", err.Error())
		return
	}
	plan.ID = types.StringValue(name)
	resp.Diagnostics.Append(resp.State.Set(ctx, &plan)...)
}

func (r *tableResource) Delete(ctx context.Context, req resource.DeleteRequest, resp *resource.DeleteResponse) {
	var state tableModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}
	if err := r.reconcile(ctx, state.Name.ValueString(), nil); err != nil {
		resp.Diagnostics.AddError("Unable to empty table", err.Error())
	}
}

func (r *tableResource) ImportState(ctx context.Context, req resource.ImportStateRequest, resp *resource.ImportStateResponse) {
	resource.ImportStatePassthroughID(ctx, path.Root("id"), req, resp)
}
