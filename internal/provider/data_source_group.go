package provider

import (
	"context"
	"fmt"

	"github.com/hashicorp/terraform-plugin-framework/datasource"
	"github.com/hashicorp/terraform-plugin-framework/datasource/schema"
	"github.com/hashicorp/terraform-plugin-framework/types"

	"github.com/followrabbit-ai/terraform-provider-rabbit/internal/client"
)

var _ datasource.DataSource = (*groupDataSource)(nil)
var _ datasource.DataSourceWithConfigure = (*groupDataSource)(nil)

type groupDataSource struct {
	data *ProviderData
}

func NewGroupDataSource() datasource.DataSource { return &groupDataSource{} }

func (d *groupDataSource) Metadata(_ context.Context, req datasource.MetadataRequest, resp *datasource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_group"
}

func (d *groupDataSource) Configure(_ context.Context, req datasource.ConfigureRequest, resp *datasource.ConfigureResponse) {
	if req.ProviderData == nil {
		return
	}
	pd, ok := req.ProviderData.(*ProviderData)
	if !ok {
		resp.Diagnostics.AddError("Unexpected ProviderData type", fmt.Sprintf("got %T", req.ProviderData))
		return
	}
	d.data = pd
}

type groupDataSourceModel struct {
	ID         types.String `tfsdk:"id"`
	DomainID   types.String `tfsdk:"domain_id"`
	Name       types.String `tfsdk:"name"`
	Roles      types.Set    `tfsdk:"roles"`
	Scope      types.Object `tfsdk:"scope"`
	Principals types.Set    `tfsdk:"principals"`
}

func (d *groupDataSource) Schema(_ context.Context, _ datasource.SchemaRequest, resp *datasource.SchemaResponse) {
	resp.Schema = schema.Schema{
		Description: "Look up an existing Rabbit group by id or name.",
		Attributes: map[string]schema.Attribute{
			"id": schema.StringAttribute{
				Optional:    true,
				Computed:    true,
				Description: "Group ID. Either id or name is required.",
			},
			"domain_id": schema.StringAttribute{
				Optional:    true,
				Computed:    true,
				Description: "Rabbit domain. Falls back to the provider's domain_id.",
			},
			"name": schema.StringAttribute{
				Optional:    true,
				Computed:    true,
				Description: "Group name. Either id or name is required.",
			},
			"roles": schema.SetAttribute{
				Computed:    true,
				ElementType: types.StringType,
				Description: "Role IDs granted by the group.",
			},
			"scope": schema.SingleNestedAttribute{
				Computed:    true,
				Description: "GCP folder/project scope.",
				Attributes: map[string]schema.Attribute{
					"folders": schema.SetAttribute{
						Computed:    true,
						ElementType: types.StringType,
					},
					"projects": schema.SetAttribute{
						Computed:    true,
						ElementType: types.StringType,
					},
				},
			},
			"principals": schema.SetNestedAttribute{
				Computed: true,
				NestedObject: schema.NestedAttributeObject{
					Attributes: map[string]schema.Attribute{
						"id":             schema.StringAttribute{Computed: true},
						"name":           schema.StringAttribute{Computed: true},
						"principal_type": schema.StringAttribute{Computed: true},
					},
				},
			},
		},
	}
}

func (d *groupDataSource) Read(ctx context.Context, req datasource.ReadRequest, resp *datasource.ReadResponse) {
	var cfg groupDataSourceModel
	resp.Diagnostics.Append(req.Config.Get(ctx, &cfg)...)
	if resp.Diagnostics.HasError() {
		return
	}

	domain := cfg.DomainID.ValueString()
	if domain == "" {
		domain = d.data.DomainID
	}
	if domain == "" {
		resp.Diagnostics.AddError("Missing domain_id", "Either set the data source's `domain_id` or the provider's `domain_id`.")
		return
	}

	var g *client.Group
	var err error
	if id := cfg.ID.ValueString(); id != "" {
		g, err = d.data.Client.GetGroup(ctx, domain, id)
	} else if name := cfg.Name.ValueString(); name != "" {
		var all []client.Group
		all, err = d.data.Client.ListGroups(ctx, domain)
		if err == nil {
			for i := range all {
				if all[i].Name == name {
					g = &all[i]
					// Refetch by id to get principals/roles fully populated (list endpoints
					// sometimes truncate; safer to GET).
					full, ferr := d.data.Client.GetGroup(ctx, domain, all[i].ID)
					if ferr == nil {
						g = full
					}
					break
				}
			}
			if g == nil {
				err = fmt.Errorf("no group named %q in domain %q", name, domain)
			}
		}
	} else {
		resp.Diagnostics.AddError("Missing lookup key", "Set either `id` or `name`.")
		return
	}
	if err != nil {
		resp.Diagnostics.AddError("Failed to read group", err.Error())
		return
	}

	resp.Diagnostics.Append(setDataSourceGroupState(ctx, &resp.State, g, domain)...)
}
