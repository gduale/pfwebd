// Package provider implements the Terraform provider for pfwebd.
package provider

import (
	"context"
	"os"

	"github.com/hashicorp/terraform-plugin-framework/datasource"
	"github.com/hashicorp/terraform-plugin-framework/provider"
	"github.com/hashicorp/terraform-plugin-framework/provider/schema"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/types"

	"github.com/gduale/terraform-provider-pfwebd/internal/client"
)

var _ provider.Provider = (*pfwebdProvider)(nil)

// New returns the provider factory used by main and by acceptance tests.
func New(version string) func() provider.Provider {
	return func() provider.Provider {
		return &pfwebdProvider{version: version}
	}
}

type pfwebdProvider struct {
	version string
}

type providerModel struct {
	Endpoint types.String `tfsdk:"endpoint"`
	Token    types.String `tfsdk:"token"`
}

func (p *pfwebdProvider) Metadata(_ context.Context, _ provider.MetadataRequest, resp *provider.MetadataResponse) {
	resp.TypeName = "pfwebd"
	resp.Version = p.version
}

func (p *pfwebdProvider) Schema(_ context.Context, _ provider.SchemaRequest, resp *provider.SchemaResponse) {
	resp.Schema = schema.Schema{
		Description: "Manages an OpenBSD PF firewall through the pfwebd REST API.",
		Attributes: map[string]schema.Attribute{
			"endpoint": schema.StringAttribute{
				Optional: true,
				Description: "Base URL of the pfwebd API (e.g. https://fw.example.org:8080). " +
					"Defaults to the PFWEBD_ENDPOINT environment variable, then http://127.0.0.1:8080.",
			},
			"token": schema.StringAttribute{
				Optional:  true,
				Sensitive: true,
				Description: "API token (sent as a Bearer token). Required for all writes, since " +
					"pfwebd is always read-only without it. Defaults to the PFWEBD_TOKEN environment variable.",
			},
		},
	}
}

func (p *pfwebdProvider) Configure(ctx context.Context, req provider.ConfigureRequest, resp *provider.ConfigureResponse) {
	var cfg providerModel
	resp.Diagnostics.Append(req.Config.Get(ctx, &cfg)...)
	if resp.Diagnostics.HasError() {
		return
	}

	endpoint := os.Getenv("PFWEBD_ENDPOINT")
	if !cfg.Endpoint.IsNull() && cfg.Endpoint.ValueString() != "" {
		endpoint = cfg.Endpoint.ValueString()
	}
	if endpoint == "" {
		endpoint = "http://127.0.0.1:8080"
	}

	token := os.Getenv("PFWEBD_TOKEN")
	if !cfg.Token.IsNull() && cfg.Token.ValueString() != "" {
		token = cfg.Token.ValueString()
	}

	c := client.New(endpoint, token)
	resp.ResourceData = c
	resp.DataSourceData = c
}

func (p *pfwebdProvider) Resources(context.Context) []func() resource.Resource {
	return []func() resource.Resource{
		NewAnchorRulesetResource,
		NewTableResource,
	}
}

func (p *pfwebdProvider) DataSources(context.Context) []func() datasource.DataSource {
	return []func() datasource.DataSource{
		NewStatusDataSource,
	}
}

// --- shared helpers ---

func toTFStrings(in []string) []types.String {
	out := make([]types.String, 0, len(in))
	for _, s := range in {
		out = append(out, types.StringValue(s))
	}
	return out
}

func fromTFStrings(in []types.String) []string {
	out := make([]string, 0, len(in))
	for _, s := range in {
		out = append(out, s.ValueString())
	}
	return out
}
