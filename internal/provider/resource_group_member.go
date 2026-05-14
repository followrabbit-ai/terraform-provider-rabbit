package provider

import (
	"context"
	"fmt"
	"strings"

	"github.com/hashicorp/terraform-plugin-framework-validators/stringvalidator"
	"github.com/hashicorp/terraform-plugin-framework/path"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/planmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/stringplanmodifier"
	"github.com/hashicorp/terraform-plugin-framework/schema/validator"
	"github.com/hashicorp/terraform-plugin-framework/types"

	"github.com/followrabbit-ai/terraform-provider-rabbit/internal/client"
)

var _ resource.Resource = (*groupMemberResource)(nil)
var _ resource.ResourceWithImportState = (*groupMemberResource)(nil)
var _ resource.ResourceWithConfigure = (*groupMemberResource)(nil)

type groupMemberResource struct {
	data *ProviderData
}

func NewGroupMemberResource() resource.Resource { return &groupMemberResource{} }

func (r *groupMemberResource) Metadata(_ context.Context, req resource.MetadataRequest, resp *resource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_group_member"
}

func (r *groupMemberResource) Configure(_ context.Context, req resource.ConfigureRequest, resp *resource.ConfigureResponse) {
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

type groupMemberModel struct {
	ID            types.String `tfsdk:"id"`
	DomainID      types.String `tfsdk:"domain_id"`
	GroupID       types.String `tfsdk:"group_id"`
	Name          types.String `tfsdk:"name"`
	PrincipalType types.String `tfsdk:"principal_type"`
	PrincipalID   types.String `tfsdk:"principal_id"`
}

func (r *groupMemberResource) Schema(_ context.Context, _ resource.SchemaRequest, resp *resource.SchemaResponse) {
	resp.Schema = schema.Schema{
		Description: "Additive: adds a single principal to a Rabbit group without taking authoritative " +
			"control over the group's full principal list. Concurrent applies against the same group are " +
			"serialised through a per-group mutex to safely implement read-modify-write on top of the " +
			"backend's full-PUT semantics.",
		Attributes: map[string]schema.Attribute{
			"id": schema.StringAttribute{
				Computed:    true,
				Description: "Composite id: <domain>/<group_id>/<principal_type>/<name>.",
				PlanModifiers: []planmodifier.String{
					stringplanmodifier.UseStateForUnknown(),
				},
			},
			"domain_id": schema.StringAttribute{
				Optional:    true,
				Computed:    true,
				Description: "Rabbit domain. Falls back to the provider's domain_id.",
				PlanModifiers: []planmodifier.String{
					stringplanmodifier.RequiresReplace(),
					stringplanmodifier.UseStateForUnknown(),
				},
			},
			"group_id": schema.StringAttribute{
				Required:    true,
				Description: "ID of the group to add the principal to.",
				PlanModifiers: []planmodifier.String{
					stringplanmodifier.RequiresReplace(),
				},
			},
			"name": schema.StringAttribute{
				Required:    true,
				Description: "Principal name (email, SA email, group email, or domain).",
				PlanModifiers: []planmodifier.String{
					stringplanmodifier.RequiresReplace(),
				},
			},
			"principal_type": schema.StringAttribute{
				Required:    true,
				Description: "One of EMAIL, TRANSITIVE_EMAIL, SERVICE_ACCOUNT, EXTERNAL_GROUP, DOMAIN.",
				Validators: []validator.String{
					stringvalidator.OneOf(client.AllPrincipalTypes...),
				},
				PlanModifiers: []planmodifier.String{
					stringplanmodifier.RequiresReplace(),
				},
			},
			"principal_id": schema.StringAttribute{
				Computed:    true,
				Description: "Server-assigned principal id after the group is updated.",
				PlanModifiers: []planmodifier.String{
					stringplanmodifier.UseStateForUnknown(),
				},
			},
		},
	}
}

func (r *groupMemberResource) resolveDomain(m *groupMemberModel, diag interface{ AddError(string, string) }) string {
	if !m.DomainID.IsNull() && m.DomainID.ValueString() != "" {
		return m.DomainID.ValueString()
	}
	if r.data.DomainID != "" {
		return r.data.DomainID
	}
	diag.AddError("Missing domain_id", "Either set the resource's `domain_id` attribute or the provider's `domain_id`.")
	return ""
}

func (r *groupMemberResource) Create(ctx context.Context, req resource.CreateRequest, resp *resource.CreateResponse) {
	var plan groupMemberModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	if resp.Diagnostics.HasError() {
		return
	}
	domain := r.resolveDomain(&plan, &resp.Diagnostics)
	if resp.Diagnostics.HasError() {
		return
	}

	key := mutexKey(domain, plan.GroupID.ValueString())
	r.data.Mutex.Lock(key)
	defer r.data.Mutex.Unlock(key)

	g, err := r.data.Client.GetGroup(ctx, domain, plan.GroupID.ValueString())
	if err != nil {
		resp.Diagnostics.AddError("Failed to read group before adding member", err.Error())
		return
	}

	pName := plan.Name.ValueString()
	pType := client.PrincipalType(plan.PrincipalType.ValueString())
	if existing := findPrincipal(g, pName, pType); existing != nil {
		// Already present (e.g. from a prior run). Reconcile state without re-PUT.
		plan.PrincipalID = types.StringValue(existing.ID)
		plan.DomainID = types.StringValue(domain)
		plan.ID = types.StringValue(memberCompositeID(domain, g.ID, pType, pName))
		resp.Diagnostics.Append(resp.State.Set(ctx, &plan)...)
		return
	}

	g.Principals = append(g.Principals, client.Principal{Name: pName, PrincipalType: pType})
	updated, err := r.data.Client.UpdateGroup(ctx, domain, g.ID, g)
	if err != nil {
		resp.Diagnostics.AddError("Failed to add member to group", err.Error())
		return
	}

	added := findPrincipal(updated, pName, pType)
	if added == nil {
		resp.Diagnostics.AddError("Principal not present after update", "Server response did not include the new principal.")
		return
	}
	plan.PrincipalID = types.StringValue(added.ID)
	plan.DomainID = types.StringValue(domain)
	plan.ID = types.StringValue(memberCompositeID(domain, g.ID, pType, pName))
	resp.Diagnostics.Append(resp.State.Set(ctx, &plan)...)
}

func (r *groupMemberResource) Read(ctx context.Context, req resource.ReadRequest, resp *resource.ReadResponse) {
	var state groupMemberModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}
	domain := r.resolveDomain(&state, &resp.Diagnostics)
	if resp.Diagnostics.HasError() {
		return
	}

	g, err := r.data.Client.GetGroup(ctx, domain, state.GroupID.ValueString())
	if err != nil {
		if client.IsNotFound(err) {
			resp.State.RemoveResource(ctx)
			return
		}
		resp.Diagnostics.AddError("Failed to read group", err.Error())
		return
	}
	found := findPrincipal(g, state.Name.ValueString(), client.PrincipalType(state.PrincipalType.ValueString()))
	if found == nil {
		resp.State.RemoveResource(ctx)
		return
	}
	state.PrincipalID = types.StringValue(found.ID)
	state.DomainID = types.StringValue(domain)
	resp.Diagnostics.Append(resp.State.Set(ctx, &state)...)
}

// Update is unreachable in practice because every meaningful attribute
// is RequiresReplace. We implement it as a no-op refresh for completeness.
func (r *groupMemberResource) Update(ctx context.Context, req resource.UpdateRequest, resp *resource.UpdateResponse) {
	var plan groupMemberModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	if resp.Diagnostics.HasError() {
		return
	}
	resp.Diagnostics.Append(resp.State.Set(ctx, &plan)...)
}

func (r *groupMemberResource) Delete(ctx context.Context, req resource.DeleteRequest, resp *resource.DeleteResponse) {
	var state groupMemberModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}
	domain := r.resolveDomain(&state, &resp.Diagnostics)
	if resp.Diagnostics.HasError() {
		return
	}

	key := mutexKey(domain, state.GroupID.ValueString())
	r.data.Mutex.Lock(key)
	defer r.data.Mutex.Unlock(key)

	g, err := r.data.Client.GetGroup(ctx, domain, state.GroupID.ValueString())
	if err != nil {
		if client.IsNotFound(err) {
			return
		}
		resp.Diagnostics.AddError("Failed to read group before removing member", err.Error())
		return
	}
	name := state.Name.ValueString()
	pType := client.PrincipalType(state.PrincipalType.ValueString())
	kept := make([]client.Principal, 0, len(g.Principals))
	removed := false
	for _, p := range g.Principals {
		if !removed && p.Name == name && p.PrincipalType == pType {
			removed = true
			continue
		}
		kept = append(kept, p)
	}
	if !removed {
		// Already gone — nothing to do.
		return
	}
	g.Principals = kept
	if _, err := r.data.Client.UpdateGroup(ctx, domain, g.ID, g); err != nil {
		resp.Diagnostics.AddError("Failed to remove member from group", err.Error())
		return
	}
}

// ImportState accepts "<domain>/<group_id>/<principal_type>/<name>".
func (r *groupMemberResource) ImportState(ctx context.Context, req resource.ImportStateRequest, resp *resource.ImportStateResponse) {
	parts := strings.SplitN(req.ID, "/", 4)
	if len(parts) != 4 {
		resp.Diagnostics.AddError("Invalid import id", "Expected \"<domain>/<group_id>/<principal_type>/<name>\".")
		return
	}
	domain, groupID, pType, name := parts[0], parts[1], parts[2], parts[3]
	resp.Diagnostics.Append(resp.State.SetAttribute(ctx, path.Root("domain_id"), domain)...)
	resp.Diagnostics.Append(resp.State.SetAttribute(ctx, path.Root("group_id"), groupID)...)
	resp.Diagnostics.Append(resp.State.SetAttribute(ctx, path.Root("principal_type"), pType)...)
	resp.Diagnostics.Append(resp.State.SetAttribute(ctx, path.Root("name"), name)...)
	resp.Diagnostics.Append(resp.State.SetAttribute(ctx, path.Root("id"), memberCompositeID(domain, groupID, client.PrincipalType(pType), name))...)
}

func findPrincipal(g *client.Group, name string, pt client.PrincipalType) *client.Principal {
	for i := range g.Principals {
		if g.Principals[i].Name == name && g.Principals[i].PrincipalType == pt {
			return &g.Principals[i]
		}
	}
	return nil
}

func memberCompositeID(domain, group string, pt client.PrincipalType, name string) string {
	return fmt.Sprintf("%s/%s/%s/%s", domain, group, pt, name)
}

func mutexKey(domain, group string) string {
	return "group:" + domain + ":" + group
}
