package provider

import (
	"context"

	"github.com/hashicorp/terraform-plugin-framework/attr"
	"github.com/hashicorp/terraform-plugin-framework/diag"
	"github.com/hashicorp/terraform-plugin-framework/tfsdk"
	"github.com/hashicorp/terraform-plugin-framework/types"
	"github.com/hashicorp/terraform-plugin-framework/types/basetypes"

	"github.com/followrabbit-ai/terraform-provider-rabbit/internal/client"
)

// planToGroup converts a fully-known plan (Create or Update) into a Group payload.
func planToGroup(ctx context.Context, plan *groupModel) (*client.Group, diag.Diagnostics) {
	var diags diag.Diagnostics
	g := &client.Group{Name: plan.Name.ValueString()}

	if !plan.Roles.IsNull() && !plan.Roles.IsUnknown() {
		var ids []string
		diags.Append(plan.Roles.ElementsAs(ctx, &ids, false)...)
		for _, id := range ids {
			g.Roles = append(g.Roles, client.Role{ID: id})
		}
	}
	if g.Roles == nil {
		g.Roles = []client.Role{}
	}

	if !plan.Scope.IsNull() && !plan.Scope.IsUnknown() {
		var s scopeModel
		diags.Append(plan.Scope.As(ctx, &s, basetypes.ObjectAsOptions{})...)
		if !s.Folders.IsNull() && !s.Folders.IsUnknown() {
			var ids []string
			diags.Append(s.Folders.ElementsAs(ctx, &ids, false)...)
			for _, id := range ids {
				g.Scope.Folders = append(g.Scope.Folders, client.ResourceFolder{ID: id})
			}
		}
		if !s.Projects.IsNull() && !s.Projects.IsUnknown() {
			var ids []string
			diags.Append(s.Projects.ElementsAs(ctx, &ids, false)...)
			for _, id := range ids {
				g.Scope.Projects = append(g.Scope.Projects, client.ResourceProject{ID: id})
			}
		}
	}
	if g.Scope.Folders == nil {
		g.Scope.Folders = []client.ResourceFolder{}
	}
	if g.Scope.Projects == nil {
		g.Scope.Projects = []client.ResourceProject{}
	}

	if !plan.Principals.IsNull() && !plan.Principals.IsUnknown() {
		var ps []principalModel
		diags.Append(plan.Principals.ElementsAs(ctx, &ps, false)...)
		for _, p := range ps {
			g.Principals = append(g.Principals, client.Principal{
				Name:          p.Name.ValueString(),
				PrincipalType: client.PrincipalType(p.PrincipalType.ValueString()),
			})
		}
	}
	if g.Principals == nil {
		g.Principals = []client.Principal{}
	}
	return g, diags
}

// setStateFromGroup writes a Group back into Terraform state.
func setStateFromGroup(ctx context.Context, state *tfsdk.State, g *client.Group, domain string) diag.Diagnostics {
	var diags diag.Diagnostics

	roleIDs := make([]string, 0, len(g.Roles))
	for _, r := range g.Roles {
		roleIDs = append(roleIDs, r.ID)
	}
	rolesSet, d := types.SetValueFrom(ctx, types.StringType, roleIDs)
	diags.Append(d...)

	folderIDs := make([]string, 0, len(g.Scope.Folders))
	for _, f := range g.Scope.Folders {
		folderIDs = append(folderIDs, f.ID)
	}
	foldersSet, d := types.SetValueFrom(ctx, types.StringType, folderIDs)
	diags.Append(d...)

	projectIDs := make([]string, 0, len(g.Scope.Projects))
	for _, p := range g.Scope.Projects {
		projectIDs = append(projectIDs, p.ID)
	}
	projectsSet, d := types.SetValueFrom(ctx, types.StringType, projectIDs)
	diags.Append(d...)

	scopeObj, d := types.ObjectValue(scopeAttrTypes(), map[string]attr.Value{
		"folders":  foldersSet,
		"projects": projectsSet,
	})
	diags.Append(d...)

	principalObjs := make([]attr.Value, 0, len(g.Principals))
	for _, p := range g.Principals {
		obj, d := types.ObjectValue(principalAttrTypes(), map[string]attr.Value{
			"name":           types.StringValue(p.Name),
			"principal_type": types.StringValue(string(p.PrincipalType)),
		})
		diags.Append(d...)
		principalObjs = append(principalObjs, obj)
	}
	principalsSet, d := types.SetValue(types.ObjectType{AttrTypes: principalAttrTypes()}, principalObjs)
	diags.Append(d...)

	m := groupModel{
		ID:         types.StringValue(g.ID),
		DomainID:   types.StringValue(domain),
		Name:       types.StringValue(g.Name),
		Roles:      rolesSet,
		Scope:      scopeObj,
		Principals: principalsSet,
	}
	diags.Append(state.Set(ctx, &m)...)
	return diags
}

// setDataSourceGroupState mirrors setStateFromGroup for the data source.
func setDataSourceGroupState(ctx context.Context, state *tfsdk.State, g *client.Group, domain string) diag.Diagnostics {
	var diags diag.Diagnostics

	roleIDs := make([]string, 0, len(g.Roles))
	for _, r := range g.Roles {
		roleIDs = append(roleIDs, r.ID)
	}
	rolesSet, d := types.SetValueFrom(ctx, types.StringType, roleIDs)
	diags.Append(d...)

	folderIDs := make([]string, 0, len(g.Scope.Folders))
	for _, f := range g.Scope.Folders {
		folderIDs = append(folderIDs, f.ID)
	}
	foldersSet, d := types.SetValueFrom(ctx, types.StringType, folderIDs)
	diags.Append(d...)

	projectIDs := make([]string, 0, len(g.Scope.Projects))
	for _, p := range g.Scope.Projects {
		projectIDs = append(projectIDs, p.ID)
	}
	projectsSet, d := types.SetValueFrom(ctx, types.StringType, projectIDs)
	diags.Append(d...)

	scopeObj, d := types.ObjectValue(scopeAttrTypes(), map[string]attr.Value{
		"folders":  foldersSet,
		"projects": projectsSet,
	})
	diags.Append(d...)

	principalObjs := make([]attr.Value, 0, len(g.Principals))
	for _, p := range g.Principals {
		obj, d := types.ObjectValue(principalAttrTypes(), map[string]attr.Value{
			"name":           types.StringValue(p.Name),
			"principal_type": types.StringValue(string(p.PrincipalType)),
		})
		diags.Append(d...)
		principalObjs = append(principalObjs, obj)
	}
	principalsSet, d := types.SetValue(types.ObjectType{AttrTypes: principalAttrTypes()}, principalObjs)
	diags.Append(d...)

	m := groupDataSourceModel{
		ID:         types.StringValue(g.ID),
		DomainID:   types.StringValue(domain),
		Name:       types.StringValue(g.Name),
		Roles:      rolesSet,
		Scope:      scopeObj,
		Principals: principalsSet,
	}
	diags.Append(state.Set(ctx, &m)...)
	return diags
}

// silence unused import warning if basetypes ends up unused.
var _ = basetypes.ObjectAsOptions{}
