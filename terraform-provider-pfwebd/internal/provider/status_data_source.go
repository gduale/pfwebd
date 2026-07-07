package provider

import (
	"context"

	"github.com/hashicorp/terraform-plugin-framework/datasource"
	"github.com/hashicorp/terraform-plugin-framework/datasource/schema"
	"github.com/hashicorp/terraform-plugin-framework/types"

	"github.com/gduale/terraform-provider-pfwebd/internal/client"
)

var (
	_ datasource.DataSource              = (*statusDataSource)(nil)
	_ datasource.DataSourceWithConfigure = (*statusDataSource)(nil)
)

func NewStatusDataSource() datasource.DataSource {
	return &statusDataSource{}
}

type statusDataSource struct {
	client *client.Client
}

type statusModel struct {
	ID      types.String `tfsdk:"id"`
	Enabled types.Bool   `tfsdk:"enabled"`
	Since   types.String `tfsdk:"since"`
}

func (d *statusDataSource) Metadata(_ context.Context, req datasource.MetadataRequest, resp *datasource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_status"
}

func (d *statusDataSource) Schema(_ context.Context, _ datasource.SchemaRequest, resp *datasource.SchemaResponse) {
	resp.Schema = schema.Schema{
		Description: "Reads the PF status from pfwebd (useful for sanity checks and outputs).",
		Attributes: map[string]schema.Attribute{
			"id": schema.StringAttribute{
				Computed:    true,
				Description: "Always \"pf\".",
			},
			"enabled": schema.BoolAttribute{
				Computed:    true,
				Description: "Whether PF is enabled.",
			},
			"since": schema.StringAttribute{
				Computed:    true,
				Description: "How long PF has been running (e.g. \"21 days 04:11:33\").",
			},
		},
	}
}

func (d *statusDataSource) Configure(_ context.Context, req datasource.ConfigureRequest, _ *datasource.ConfigureResponse) {
	if req.ProviderData == nil {
		return
	}
	d.client = req.ProviderData.(*client.Client)
}

func (d *statusDataSource) Read(ctx context.Context, _ datasource.ReadRequest, resp *datasource.ReadResponse) {
	st, err := d.client.GetStatus(ctx)
	if err != nil {
		resp.Diagnostics.AddError("Unable to read PF status", err.Error())
		return
	}
	model := statusModel{
		ID:      types.StringValue("pf"),
		Enabled: types.BoolValue(st.Enabled),
		Since:   types.StringValue(st.Since),
	}
	resp.Diagnostics.Append(resp.State.Set(ctx, &model)...)
}
