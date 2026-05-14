package provider

import (
	"context"
	"fmt"
	"strings"

	"github.com/hashicorp/terraform-plugin-framework-validators/stringvalidator"
	"github.com/hashicorp/terraform-plugin-framework/attr"
	"github.com/hashicorp/terraform-plugin-framework/path"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/planmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/stringplanmodifier"
	"github.com/hashicorp/terraform-plugin-framework/schema/validator"
	"github.com/hashicorp/terraform-plugin-framework/types"

	"github.com/followrabbit-ai/terraform-provider-rabbit/internal/client"
)

var _ resource.Resource = (*groupResource)(nil)
var _ resource.ResourceWithImportState = (*groupResource)(nil)
var _ resource.ResourceWithConfigure = (*groupResource)(nil)

type groupResource struct {
	data *ProviderData
}

// NewGroupResource is the factory used by the provider.
func NewGroupResource() resource.Resource { return &groupResource{} }

func (r *groupResource) Metadata(_ context.Context, req resource.MetadataRequest, resp *resource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_group"
}

func (r *groupResource) Configure(_ context.Context, req resource.ConfigureRequest, resp *resource.ConfigureResponse) {
	if req.ProviderData == nil {
		return
	}
	pd, ok := req.ProviderData.(*ProviderData)
	if !ok {
		resp.Diagnostics.AddError("Unexpected ProviderData type", fmt.Sprintf("got %T", req.ProviderData))
		return
	}
	r.data = pd
}

// groupModel mirrors the Terraform state for the resource.
type groupModel struct {
	ID         types.String `tfsdk:"id"`
	DomainID   types.String `tfsdk:"domain_id"`
	Name       types.String `tfsdk:"name"`
	Roles      types.Set    `tfsdk:"roles"`
	Scope      types.Object `tfsdk:"scope"`
	Principals types.Set    `tfsdk:"principals"`
}

type scopeModel struct {
	Folders  types.Set `tfsdk:"folders"`
	Projects types.Set `tfsdk:"projects"`
}

func scopeAttrTypes() map[string]attr.Type {
	return map[string]attr.Type{
		"folders":  types.SetType{ElemType: types.StringType},
		"projects": types.SetType{ElemType: types.StringType},
	}
}

type principalModel struct {
	ID            types.String `tfsdk:"id"`
	Name          types.String `tfsdk:"name"`
	PrincipalType types.String `tfsdk:"principal_type"`
}

func principalAttrTypes() map[string]attr.Type {
	return map[string]attr.Type{
		"id":             types.StringType,
		"name":           types.StringType,
		"principal_type": types.StringType,
	}
}

func (r *groupResource) Schema(_ context.Context, _ resource.SchemaRequest, resp *resource.SchemaResponse) {
	resp.Schema = schema.Schema{
		Description: "A Rabbit access management group: name + roles + principals + GCP folder/project scope.",
		Attributes: map[string]schema.Attribute{
			"id": schema.StringAttribute{
				Computed:    true,
				Description: "Server-assigned group identifier.",
				PlanModifiers: []planmodifier.String{
					stringplanmodifier.UseStateForUnknown(),
				},
			},
			"domain_id": schema.StringAttribute{
				Optional:    true,
				Computed:    true,
				Description: "Rabbit domain that owns the group. Falls back to the provider's domain_id.",
				PlanModifiers: []planmodifier.String{
					stringplanmodifier.RequiresReplace(),
					stringplanmodifier.UseStateForUnknown(),
				},
			},
			"name": schema.StringAttribute{
				Required:    true,
				Description: "Display name for the group.",
				Validators: []validator.String{
					stringvalidator.LengthAtLeast(1),
				},
			},
			"roles": schema.SetAttribute{
				Required:    true,
				ElementType: types.StringType,
				Description: "Role identifiers (e.g. \"roles/domain.admin\") granted by this group.",
			},
			"scope": schema.SingleNestedAttribute{
				Optional:    true,
				Computed:    true,
				Description: "GCP folder and project IDs the group applies to. Empty = domain-wide.",
				Attributes: map[string]schema.Attribute{
					"folders": schema.SetAttribute{
						Optional:    true,
						Computed:    true,
						ElementType: types.StringType,
						Description: "GCP folder IDs (e.g. \"folders/123456789\").",
					},
					"projects": schema.SetAttribute{
						Optional:    true,
						Computed:    true,
						ElementType: types.StringType,
						Description: "GCP project IDs (e.g. \"projects/acme-prod\").",
					},
				},
			},
			"principals": schema.SetNestedAttribute{
				Required:    true,
				Description: "Members of the group.",
				NestedObject: schema.NestedAttributeObject{
					Attributes: map[string]schema.Attribute{
						"id": schema.StringAttribute{
							Computed:    true,
							Description: "Server-assigned principal id (stable across updates).",
						},
						"name": schema.StringAttribute{
							Required:    true,
							Description: "Principal name (email, SA email, group email, or domain).",
						},
						"principal_type": schema.StringAttribute{
							Required:    true,
							Description: "One of EMAIL, TRANSITIVE_EMAIL, SERVICE_ACCOUNT, EXTERNAL_GROUP, DOMAIN.",
							Validators: []validator.String{
								stringvalidator.OneOf(client.AllPrincipalTypes...),
							},
						},
					},
				},
			},
		},
	}
}

// resolveDomain returns the resource-level domain falling back to the provider default.
func (r *groupResource) resolveDomain(plan *groupModel, diag interface{ AddError(string, string) }) string {
	if !plan.DomainID.IsNull() && plan.DomainID.ValueString() != "" {
		return plan.DomainID.ValueString()
	}
	if r.data.DomainID != "" {
		return r.data.DomainID
	}
	diag.AddError("Missing domain_id", "Either set the resource's `domain_id` attribute or the provider's `domain_id`.")
	return ""
}

func (r *groupResource) Create(ctx context.Context, req resource.CreateRequest, resp *resource.CreateResponse) {
	var plan groupModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	if resp.Diagnostics.HasError() {
		return
	}
	domain := r.resolveDomain(&plan, &resp.Diagnostics)
	if resp.Diagnostics.HasError() {
		return
	}

	g, diag := planToGroup(ctx, &plan)
	resp.Diagnostics.Append(diag...)
	if resp.Diagnostics.HasError() {
		return
	}

	created, err := r.data.Client.CreateGroup(ctx, domain, g)
	if err != nil {
		resp.Diagnostics.AddError("Failed to create group", err.Error())
		return
	}

	resp.Diagnostics.Append(setStateFromGroup(ctx, &resp.State, created, domain)...)
}

func (r *groupResource) Read(ctx context.Context, req resource.ReadRequest, resp *resource.ReadResponse) {
	var state groupModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}
	domain := r.resolveDomain(&state, &resp.Diagnostics)
	if resp.Diagnostics.HasError() {
		return
	}

	g, err := r.data.Client.GetGroup(ctx, domain, state.ID.ValueString())
	if err != nil {
		if client.IsNotFound(err) {
			resp.State.RemoveResource(ctx)
			return
		}
		resp.Diagnostics.AddError("Failed to read group", err.Error())
		return
	}
	resp.Diagnostics.Append(setStateFromGroup(ctx, &resp.State, g, domain)...)
}

func (r *groupResource) Update(ctx context.Context, req resource.UpdateRequest, resp *resource.UpdateResponse) {
	var plan groupModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	if resp.Diagnostics.HasError() {
		return
	}
	var state groupModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}

	domain := r.resolveDomain(&plan, &resp.Diagnostics)
	if resp.Diagnostics.HasError() {
		return
	}

	g, diag := planToGroup(ctx, &plan)
	resp.Diagnostics.Append(diag...)
	if resp.Diagnostics.HasError() {
		return
	}
	g.ID = state.ID.ValueString()

	updated, err := r.data.Client.UpdateGroup(ctx, domain, state.ID.ValueString(), g)
	if err != nil {
		resp.Diagnostics.AddError("Failed to update group", err.Error())
		return
	}
	resp.Diagnostics.Append(setStateFromGroup(ctx, &resp.State, updated, domain)...)
}

func (r *groupResource) Delete(ctx context.Context, req resource.DeleteRequest, resp *resource.DeleteResponse) {
	var state groupModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}
	domain := r.resolveDomain(&state, &resp.Diagnostics)
	if resp.Diagnostics.HasError() {
		return
	}
	if err := r.data.Client.DeleteGroup(ctx, domain, state.ID.ValueString()); err != nil {
		if client.IsNotFound(err) {
			return
		}
		resp.Diagnostics.AddError("Failed to delete group", err.Error())
	}
}

// ImportState understands "<domain_id>/<group_id>" and bare "<group_id>"
// (using the provider's default domain).
func (r *groupResource) ImportState(ctx context.Context, req resource.ImportStateRequest, resp *resource.ImportStateResponse) {
	parts := strings.SplitN(req.ID, "/", 2)
	var domain, id string
	switch len(parts) {
	case 1:
		if r.data.DomainID == "" {
			resp.Diagnostics.AddError("Import requires a domain", "Use \"<domain_id>/<group_id>\" or set domain_id on the provider.")
			return
		}
		domain = r.data.DomainID
		id = parts[0]
	case 2:
		domain = parts[0]
		id = parts[1]
	}
	resp.Diagnostics.Append(resp.State.SetAttribute(ctx, path.Root("id"), id)...)
	resp.Diagnostics.Append(resp.State.SetAttribute(ctx, path.Root("domain_id"), domain)...)
}
