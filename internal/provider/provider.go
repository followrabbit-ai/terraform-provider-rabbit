package provider

import (
	"context"
	"net/http"
	"os"
	"time"

	"github.com/hashicorp/terraform-plugin-framework/datasource"
	"github.com/hashicorp/terraform-plugin-framework/path"
	"github.com/hashicorp/terraform-plugin-framework/provider"
	"github.com/hashicorp/terraform-plugin-framework/provider/schema"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/types"

	"github.com/followrabbit-ai/terraform-provider-rabbit/internal/client"
	"github.com/followrabbit-ai/terraform-provider-rabbit/internal/mutex"
)

// rabbitProvider satisfies provider.Provider.
type rabbitProvider struct {
	version string
}

// ProviderData is what Configure stuffs into req.ProviderData for resources.
type ProviderData struct {
	Client   *client.Client
	DomainID string
	Mutex    *mutex.KV
}

// New returns a constructor for the Plugin Framework runtime.
func New(version string) func() provider.Provider {
	return func() provider.Provider {
		return &rabbitProvider{version: version}
	}
}

func (p *rabbitProvider) Metadata(_ context.Context, _ provider.MetadataRequest, resp *provider.MetadataResponse) {
	resp.TypeName = "rabbit"
	resp.Version = p.version
}

type providerModel struct {
	Endpoint                  types.String `tfsdk:"endpoint"`
	Audience                  types.String `tfsdk:"audience"`
	DomainID                  types.String `tfsdk:"domain_id"`
	Credentials               types.String `tfsdk:"credentials"`
	ImpersonateServiceAccount types.String `tfsdk:"impersonate_service_account"`
	RequestTimeout            types.String `tfsdk:"request_timeout"`
}

func (p *rabbitProvider) Schema(_ context.Context, _ provider.SchemaRequest, resp *provider.SchemaResponse) {
	resp.Schema = schema.Schema{
		Description: "Manage Rabbit (followrabbit.ai) resources from Terraform.",
		Attributes: map[string]schema.Attribute{
			"endpoint": schema.StringAttribute{
				Optional:    true,
				Description: "Rabbit backend base URL. Defaults to the prod endpoint. Env: RABBIT_ENDPOINT.",
			},
			"audience": schema.StringAttribute{
				Optional:    true,
				Description: "OAuth2 client ID used as the Google ID token audience. Defaults to the prod client ID. Env: RABBIT_AUDIENCE.",
			},
			"domain_id": schema.StringAttribute{
				Optional:    true,
				Description: "Default Rabbit domain (e.g. \"acme.com\") to manage. Each resource may override. Env: RABBIT_DOMAIN_ID.",
			},
			"credentials": schema.StringAttribute{
				Optional:    true,
				Sensitive:   true,
				Description: "Inline service account JSON or a path to a service account JSON file. Env: GOOGLE_CREDENTIALS / GOOGLE_APPLICATION_CREDENTIALS.",
			},
			"impersonate_service_account": schema.StringAttribute{
				Optional:    true,
				Description: "Email of a service account to impersonate via Google IAM Credentials. Env: GOOGLE_IMPERSONATE_SERVICE_ACCOUNT.",
			},
			"request_timeout": schema.StringAttribute{
				Optional:    true,
				Description: "Per-request HTTP timeout (Go duration). Defaults to 30s.",
			},
		},
	}
}

func (p *rabbitProvider) Configure(ctx context.Context, req provider.ConfigureRequest, resp *provider.ConfigureResponse) {
	var cfg providerModel
	resp.Diagnostics.Append(req.Config.Get(ctx, &cfg)...)
	if resp.Diagnostics.HasError() {
		return
	}

	endpoint := firstNonEmpty(cfg.Endpoint.ValueString(), os.Getenv("RABBIT_ENDPOINT"), client.DefaultEndpoint)
	audience := firstNonEmpty(cfg.Audience.ValueString(), os.Getenv("RABBIT_AUDIENCE"), client.DefaultAudience)
	domainID := firstNonEmpty(cfg.DomainID.ValueString(), os.Getenv("RABBIT_DOMAIN_ID"))
	credentials := firstNonEmpty(cfg.Credentials.ValueString(), os.Getenv("GOOGLE_CREDENTIALS"), os.Getenv("GOOGLE_APPLICATION_CREDENTIALS"))
	impersonate := firstNonEmpty(cfg.ImpersonateServiceAccount.ValueString(), os.Getenv("GOOGLE_IMPERSONATE_SERVICE_ACCOUNT"))

	timeout := client.DefaultTimeout
	if s := cfg.RequestTimeout.ValueString(); s != "" {
		d, err := time.ParseDuration(s)
		if err != nil {
			resp.Diagnostics.AddAttributeError(path.Root("request_timeout"), "Invalid duration", err.Error())
			return
		}
		timeout = d
	}

	authCfg := client.AuthConfig{
		Audience:                  audience,
		ImpersonateServiceAccount: impersonate,
	}
	// `credentials` may be either inline JSON or a path. JSON starts with `{`;
	// anything else is treated as a path.
	if credentials != "" {
		if len(credentials) > 0 && credentials[0] == '{' {
			authCfg.CredentialsJSON = credentials
		} else {
			authCfg.CredentialsFile = credentials
		}
	}

	var httpClient *http.Client
	if testHTTPClientFactory != nil {
		var herr error
		httpClient, herr = testHTTPClientFactory(ctx)
		if herr != nil {
			resp.Diagnostics.AddError("Failed to build test HTTP client", herr.Error())
			return
		}
	} else {
		var herr error
		httpClient, herr = client.NewHTTPClient(ctx, authCfg)
		if herr != nil {
			resp.Diagnostics.AddError("Failed to build authenticated HTTP client", herr.Error())
			return
		}
	}
	httpClient.Timeout = timeout
	// Custom transport to set a recognisable User-Agent; wraps oauth2's transport.
	httpClient.Transport = &userAgentTransport{
		base:      httpClient.Transport,
		userAgent: "terraform-provider-rabbit/" + p.version,
	}

	apiClient, err := client.New(client.Config{
		Endpoint:  endpoint,
		UserAgent: "terraform-provider-rabbit/" + p.version,
		DomainID:  domainID,
		HTTP:      httpClient,
	})
	if err != nil {
		resp.Diagnostics.AddError("Failed to build Rabbit client", err.Error())
		return
	}

	data := &ProviderData{
		Client:   apiClient,
		DomainID: domainID,
		Mutex:    mutex.New(),
	}
	resp.ResourceData = data
	resp.DataSourceData = data
}

func (p *rabbitProvider) Resources(_ context.Context) []func() resource.Resource {
	return []func() resource.Resource{
		NewGroupResource,
		NewGroupMemberResource,
	}
}

func (p *rabbitProvider) DataSources(_ context.Context) []func() datasource.DataSource {
	return []func() datasource.DataSource{
		NewRoleDataSource,
		NewGroupDataSource,
	}
}

// testHTTPClientFactory is non-nil only in acceptance tests; it lets the
// suite inject an impersonation+safety-wrapped *http.Client into the
// provider without exposing it via the public schema.
var testHTTPClientFactory func(ctx context.Context) (*http.Client, error)

// SetTestHTTPClientFactory is exposed for *_test.go to install the factory.
func SetTestHTTPClientFactory(f func(ctx context.Context) (*http.Client, error)) {
	testHTTPClientFactory = f
}

func firstNonEmpty(vs ...string) string {
	for _, v := range vs {
		if v != "" {
			return v
		}
	}
	return ""
}

type userAgentTransport struct {
	base      http.RoundTripper
	userAgent string
}

func (t *userAgentTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	req.Header.Set("User-Agent", t.userAgent)
	base := t.base
	if base == nil {
		base = http.DefaultTransport
	}
	return base.RoundTrip(req)
}
