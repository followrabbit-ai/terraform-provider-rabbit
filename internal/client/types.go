package client

// PrincipalType mirrors ai.aliz.rabbit.security.authorization.model.PrincipalType.
type PrincipalType string

const (
	PrincipalEmail           PrincipalType = "EMAIL"
	PrincipalTransitiveEmail PrincipalType = "TRANSITIVE_EMAIL"
	PrincipalServiceAccount  PrincipalType = "SERVICE_ACCOUNT"
	PrincipalExternalGroup   PrincipalType = "EXTERNAL_GROUP"
	PrincipalDomain          PrincipalType = "DOMAIN"
)

// AllPrincipalTypes is the canonical list, used for schema validators.
var AllPrincipalTypes = []string{
	string(PrincipalEmail),
	string(PrincipalTransitiveEmail),
	string(PrincipalServiceAccount),
	string(PrincipalExternalGroup),
	string(PrincipalDomain),
}

// ResourceType mirrors ai.aliz.rabbit.security.authorization.model.ResourceType.
type ResourceType string

const (
	ResourceBase                          ResourceType = "BASE"
	ResourceDomain                        ResourceType = "DOMAIN"
	ResourceGrantedIfOtherPermissionExist ResourceType = "GRANTED_IF_OTHER_PERMISSION_EXISTS"
)

// Role mirrors ai.aliz.rabbit.dto.RoleDto.
type Role struct {
	ID           string       `json:"id"`
	Name         string       `json:"name,omitempty"`
	Description  string       `json:"description,omitempty"`
	ResourceType ResourceType `json:"resourceType,omitempty"`
}

// Principal mirrors ai.aliz.rabbit.dto.accessmanagement.PrincipalDto.
type Principal struct {
	ID            string        `json:"id,omitempty"`
	Name          string        `json:"name"`
	PrincipalType PrincipalType `json:"principalType,omitempty"`
}

// ResourceFolder mirrors ai.aliz.rabbit.dto.ResourceFolderDto.
type ResourceFolder struct {
	ID   string `json:"id,omitempty"`
	Name string `json:"name,omitempty"`
}

// ResourceProject mirrors ai.aliz.rabbit.dto.ResourceProjectDto.
type ResourceProject struct {
	ID   string `json:"id,omitempty"`
	Name string `json:"name,omitempty"`
}

// AccessScope mirrors ai.aliz.rabbit.dto.accessmanagement.AccessScopeDto.
type AccessScope struct {
	Folders  []ResourceFolder  `json:"folders"`
	Projects []ResourceProject `json:"projects"`
}

// Group mirrors ai.aliz.rabbit.dto.accessmanagement.GroupDto.
type Group struct {
	ID         string      `json:"id,omitempty"`
	Name       string      `json:"name"`
	Roles      []Role      `json:"roles"`
	Scope      AccessScope `json:"scope"`
	Principals []Principal `json:"principals"`
}

// page is Spring's Page<T> envelope used by the list endpoint.
type page[T any] struct {
	Content       []T  `json:"content"`
	TotalElements int  `json:"totalElements"`
	TotalPages    int  `json:"totalPages"`
	Number        int  `json:"number"`
	Size          int  `json:"size"`
	First         bool `json:"first"`
	Last          bool `json:"last"`
	Empty         bool `json:"empty"`
}
