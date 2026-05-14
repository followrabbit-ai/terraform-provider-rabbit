package provider

import (
	"context"
	"fmt"

	"github.com/hashicorp/terraform-plugin-framework-validators/stringvalidator"
	"github.com/hashicorp/terraform-plugin-framework/datasource"
	"github.com/hashicorp/terraform-plugin-framework/datasource/schema"
	"github.com/hashicorp/terraform-plugin-framework/schema/validator"
	"github.com/hashicorp/terraform-plugin-framework/types"

	"github.com/followrabbit-ai/terraform-provider-rabbit/internal/client"
)

var _ datasource.DataSource = (*roleDataSource)(nil)
var _ datasource.DataSourceWithConfigure = (*roleDataSource)(nil)

type roleDataSource struct {
	data *ProviderData
}

func NewRoleDataSource() datasource.DataSource { return &roleDataSource{} }

func (d *roleDataSource) Metadata(_ context.Context, req datasource.MetadataRequest, resp *datasource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_role"
}

func (d *roleDataSource) Configure(_ context.Context, req datasource.ConfigureRequest, resp *datasource.ConfigureResponse) {
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

type roleDataSourceModel struct {
	ID           types.String `tfsdk:"id"`
	Name         types.String `tfsdk:"name"`
	ResourceType types.String `tfsdk:"resource_type"`
	Description  types.String `tfsdk:"description"`
}

func (d *roleDataSource) Schema(_ context.Context, _ datasource.SchemaRequest, resp *datasource.SchemaResponse) {
	resp.Schema = schema.Schema{
		Description: "Look up a Rabbit role by name or id.",
		Attributes: map[string]schema.Attribute{
			"id": schema.StringAttribute{
				Optional:    true,
				Computed:    true,
				Description: "Role ID (e.g. \"roles/domain.admin\"). Either id or name is required.",
			},
			"name": schema.StringAttribute{
				Optional:    true,
				Computed:    true,
				Description: "Human-readable role name. Either id or name is required.",
			},
			"resource_type": schema.StringAttribute{
				Optional:    true,
				Computed:    true,
				Description: "Restrict lookup to BASE or DOMAIN roles. Defaults to DOMAIN.",
				Validators: []validator.String{
					stringvalidator.OneOf("BASE", "DOMAIN"),
				},
			},
			"description": schema.StringAttribute{
				Computed:    true,
				Description: "Server-provided description.",
			},
		},
	}
}

func (d *roleDataSource) Read(ctx context.Context, req datasource.ReadRequest, resp *datasource.ReadResponse) {
	var cfg roleDataSourceModel
	resp.Diagnostics.Append(req.Config.Get(ctx, &cfg)...)
	if resp.Diagnostics.HasError() {
		return
	}

	rt := client.ResourceDomain
	if !cfg.ResourceType.IsNull() && cfg.ResourceType.ValueString() != "" {
		rt = client.ResourceType(cfg.ResourceType.ValueString())
	}

	roles, err := d.data.Client.ListRoles(ctx, rt)
	if err != nil {
		resp.Diagnostics.AddError("Failed to list roles", err.Error())
		return
	}

	wantID := cfg.ID.ValueString()
	wantName := cfg.Name.ValueString()
	if wantID == "" && wantName == "" {
		resp.Diagnostics.AddError("Missing lookup key", "Set either `id` or `name`.")
		return
	}

	var match *client.Role
	for i := range roles {
		r := roles[i]
		if wantID != "" && r.ID == wantID {
			match = &r
			break
		}
		if wantID == "" && wantName != "" && r.Name == wantName {
			match = &r
			break
		}
	}
	if match == nil {
		resp.Diagnostics.AddError("Role not found", fmt.Sprintf("No %s role matches id=%q name=%q.", rt, wantID, wantName))
		return
	}

	cfg.ID = types.StringValue(match.ID)
	cfg.Name = types.StringValue(match.Name)
	cfg.ResourceType = types.StringValue(string(match.ResourceType))
	cfg.Description = types.StringValue(match.Description)
	resp.Diagnostics.Append(resp.State.Set(ctx, &cfg)...)
}
