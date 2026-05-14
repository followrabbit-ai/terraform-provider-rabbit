package provider

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"sort"
	"strings"
	"sync"
	"testing"

	"github.com/hashicorp/terraform-plugin-framework/providerserver"
	"github.com/hashicorp/terraform-plugin-go/tfprotov6"

	"github.com/followrabbit-ai/terraform-provider-rabbit/internal/client"
)

// -----------------------------------------------------------------------------
// Safety configuration
//
// The acceptance suite refuses to run unless RABBIT_TEST_DOMAIN_ID matches
// one of the comma-separated values in RABBIT_TEST_ALLOWED_DOMAINS. This is
// the last line of defence against accidentally pointing the destructive
// half of the suite at a tenant that isn't a disposable test environment.
//
// Operators are expected to point this only at throwaway test tenants whose
// state can be modified freely.

func allowedTestDomains() map[string]bool {
	raw := os.Getenv("RABBIT_TEST_ALLOWED_DOMAINS")
	out := map[string]bool{}
	for _, d := range strings.Split(raw, ",") {
		d = strings.TrimSpace(d)
		if d != "" {
			out[d] = true
		}
	}
	return out
}

// resourcePrefix is set once in TestMain; every group/principal name the
// suite creates must begin with it. The safetyTransport enforces this on the
// outgoing HTTP layer.
var (
	resourcePrefix string
	preSnapshot    map[string]groupSnapshotEntry
)

type groupSnapshotEntry struct {
	Name           string
	PrincipalNames []string
	RoleIDs        []string
	FolderIDs      []string
	ProjectIDs     []string
}

// -----------------------------------------------------------------------------
// TestMain — pre/post snapshot + env validation.

func TestMain(m *testing.M) {
	if os.Getenv("TF_ACC") == "" {
		// `make test` should never wake the acc tests.
		os.Exit(m.Run())
	}

	domain := os.Getenv("RABBIT_TEST_DOMAIN_ID")
	if !allowedTestDomains()[domain] {
		fmt.Fprintf(os.Stderr, "RABBIT_TEST_DOMAIN_ID=%q is not in the test allow-list %v\n",
			domain, allowedTestDomainsList())
		os.Exit(1)
	}
	if os.Getenv("RABBIT_TEST_IMPERSONATE_SA_EMAIL") == "" || os.Getenv("RABBIT_TEST_IMPERSONATE_TARGET_EMAIL") == "" {
		fmt.Fprintln(os.Stderr, "RABBIT_TEST_IMPERSONATE_SA_EMAIL / RABBIT_TEST_IMPERSONATE_TARGET_EMAIL must be set")
		os.Exit(1)
	}
	if os.Getenv("RABBIT_ENDPOINT") == "" {
		fmt.Fprintln(os.Stderr, "RABBIT_ENDPOINT must be set")
		os.Exit(1)
	}

	// Random run id → unique resource prefix.
	buf := make([]byte, 4)
	if _, err := rand.Read(buf); err != nil {
		fmt.Fprintf(os.Stderr, "rand: %v\n", err)
		os.Exit(1)
	}
	resourcePrefix = "tfacc-" + hex.EncodeToString(buf) + "-"

	SetTestHTTPClientFactory(buildTestHTTPClient)

	c, err := newSnapshotClient(context.Background())
	if err != nil {
		fmt.Fprintf(os.Stderr, "snapshot client: %v\n", err)
		os.Exit(1)
	}
	preSnapshot, err = takeSnapshot(c, domain)
	if err != nil {
		fmt.Fprintf(os.Stderr, "pre-snapshot: %v\n", err)
		os.Exit(1)
	}
	fmt.Fprintf(os.Stderr, "[acc] domain=%s prefix=%s preSnapshot=%d existing groups\n",
		domain, resourcePrefix, len(preSnapshot))

	code := m.Run()

	post, err := takeSnapshot(c, domain)
	if err != nil {
		fmt.Fprintf(os.Stderr, "post-snapshot: %v\n", err)
		os.Exit(2)
	}
	if diff := diffSnapshots(preSnapshot, post); diff != "" {
		fmt.Fprintf(os.Stderr, "[acc] pre-existing groups changed:\n%s\n", diff)
		if code == 0 {
			code = 2
		}
	}
	for id, e := range post {
		if _, existed := preSnapshot[id]; !existed && strings.HasPrefix(e.Name, "tfacc-") {
			fmt.Fprintf(os.Stderr, "[acc] LEAKED group from this run: id=%s name=%s\n", id, e.Name)
			if code == 0 {
				code = 3
			}
		}
	}
	os.Exit(code)
}

func allowedTestDomainsList() []string {
	m := allowedTestDomains()
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}

// -----------------------------------------------------------------------------
// HTTP client factory + safety transport.

// buildTestHTTPClient is installed into the provider via SetTestHTTPClientFactory.
// Every request from any provider-driven operation flows through this.
func buildTestHTTPClient(ctx context.Context) (*http.Client, error) {
	authed, err := client.NewRabbitImpersonationHTTPClient(ctx, "", "")
	if err != nil {
		return nil, err
	}
	authed.Transport = &safetyTransport{base: authed.Transport}
	return authed, nil
}

// newSnapshotClient is a *direct* low-level client used by TestMain to capture
// the pre/post snapshots. It bypasses the safety transport because reads
// (GET) need to see *every* existing group, not just tfacc-owned ones.
func newSnapshotClient(ctx context.Context) (*client.Client, error) {
	endpoint := os.Getenv("RABBIT_ENDPOINT")
	if endpoint == "" {
		return nil, errors.New("RABBIT_ENDPOINT not set")
	}
	authed, err := client.NewRabbitImpersonationHTTPClient(ctx, "", "")
	if err != nil {
		return nil, err
	}
	return client.New(client.Config{
		Endpoint: endpoint,
		DomainID: os.Getenv("RABBIT_TEST_DOMAIN_ID"),
		HTTP:     authed,
	})
}

// safetyTransport is the last line of defence. It allows all GETs, and gates
// non-GET requests on:
//
//  1. Path: any DELETE / PUT against /api/v1/domains/.../groups/{id} must
//     target an id this test run created (recorded via the 201 Created
//     response).
//  2. Body: any POST / PUT body that has a `name` field must start with
//     resourcePrefix.
type safetyTransport struct {
	base http.RoundTripper
}

var (
	createdIDsMu sync.Mutex
	createdIDs   = map[string]bool{}
)

func rememberCreatedID(id string) {
	createdIDsMu.Lock()
	createdIDs[id] = true
	createdIDsMu.Unlock()
}

func ownsID(id string) bool {
	createdIDsMu.Lock()
	defer createdIDsMu.Unlock()
	return createdIDs[id]
}

func (s *safetyTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	if req.Method == http.MethodGet {
		return s.base.RoundTrip(req)
	}

	if id, ok := groupIDFromPath(req.URL.Path); ok {
		if !ownsID(id) {
			return nil, fmt.Errorf("safety: refusing %s %s — id %q was not created by this test run",
				req.Method, req.URL.Path, id)
		}
	}

	if req.Body != nil {
		buf, err := io.ReadAll(req.Body)
		if err != nil {
			return nil, fmt.Errorf("safety: read body: %w", err)
		}
		req.Body = io.NopCloser(strings.NewReader(string(buf)))
		req.ContentLength = int64(len(buf))
		if len(buf) > 0 {
			var probe struct {
				Name string `json:"name"`
			}
			if err := json.Unmarshal(buf, &probe); err == nil && probe.Name != "" {
				if !strings.HasPrefix(probe.Name, resourcePrefix) {
					return nil, fmt.Errorf("safety: refusing %s %s with name=%q (must start with %q)",
						req.Method, req.URL.Path, probe.Name, resourcePrefix)
				}
			}
		}
	}

	resp, err := s.base.RoundTrip(req)
	if err == nil && resp != nil && resp.StatusCode == http.StatusCreated {
		body, _ := io.ReadAll(resp.Body)
		resp.Body = io.NopCloser(strings.NewReader(string(body)))
		var g struct {
			ID string `json:"id"`
		}
		if json.Unmarshal(body, &g) == nil && g.ID != "" {
			rememberCreatedID(g.ID)
		}
	}
	return resp, err
}

func groupIDFromPath(p string) (string, bool) {
	u, err := url.Parse(p)
	if err != nil {
		return "", false
	}
	parts := strings.Split(strings.TrimPrefix(u.Path, "/"), "/")
	// /api/v1/domains/{d}/groups/{id}  → 6 parts after split.
	if len(parts) != 6 {
		return "", false
	}
	if parts[0] != "api" || parts[1] != "v1" || parts[2] != "domains" || parts[4] != "groups" {
		return "", false
	}
	return parts[5], true
}

// -----------------------------------------------------------------------------
// Snapshot helpers.

func takeSnapshot(c *client.Client, domain string) (map[string]groupSnapshotEntry, error) {
	groups, err := c.ListGroups(context.Background(), domain)
	if err != nil {
		return nil, err
	}
	out := make(map[string]groupSnapshotEntry, len(groups))
	for _, g := range groups {
		full, err := c.GetGroup(context.Background(), domain, g.ID)
		if err != nil {
			return nil, fmt.Errorf("snapshot get %s: %w", g.ID, err)
		}
		out[g.ID] = groupToSnapshot(full)
	}
	return out, nil
}

func groupToSnapshot(g *client.Group) groupSnapshotEntry {
	e := groupSnapshotEntry{Name: g.Name}
	for _, p := range g.Principals {
		e.PrincipalNames = append(e.PrincipalNames, string(p.PrincipalType)+":"+p.Name)
	}
	for _, r := range g.Roles {
		e.RoleIDs = append(e.RoleIDs, r.ID)
	}
	for _, f := range g.Scope.Folders {
		e.FolderIDs = append(e.FolderIDs, f.ID)
	}
	for _, p := range g.Scope.Projects {
		e.ProjectIDs = append(e.ProjectIDs, p.ID)
	}
	sort.Strings(e.PrincipalNames)
	sort.Strings(e.RoleIDs)
	sort.Strings(e.FolderIDs)
	sort.Strings(e.ProjectIDs)
	return e
}

func diffSnapshots(pre, post map[string]groupSnapshotEntry) string {
	var b strings.Builder
	for id, preEntry := range pre {
		postEntry, ok := post[id]
		if !ok {
			fmt.Fprintf(&b, "  - DISAPPEARED: id=%s name=%s\n", id, preEntry.Name)
			continue
		}
		if !snapshotEqual(preEntry, postEntry) {
			fmt.Fprintf(&b, "  - MODIFIED: id=%s\n      pre:  %+v\n      post: %+v\n", id, preEntry, postEntry)
		}
	}
	return b.String()
}

func snapshotEqual(a, b groupSnapshotEntry) bool {
	return a.Name == b.Name &&
		stringsEqual(a.PrincipalNames, b.PrincipalNames) &&
		stringsEqual(a.RoleIDs, b.RoleIDs) &&
		stringsEqual(a.FolderIDs, b.FolderIDs) &&
		stringsEqual(a.ProjectIDs, b.ProjectIDs)
}

func stringsEqual(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

// -----------------------------------------------------------------------------
// Test provider factory for terraform-plugin-testing.

var testProviderFactories = map[string]func() (tfprotov6.ProviderServer, error){
	"rabbit": providerserver.NewProtocol6WithError(New("test")()),
}

// preCheck is invoked by every TestAcc* via resource.Test{PreCheck: preCheck(t)}.
func preCheck(t *testing.T) func() {
	return func() {
		if os.Getenv("TF_ACC") == "" {
			t.Skip("TF_ACC not set")
		}
		if os.Getenv("RABBIT_ENDPOINT") == "" {
			t.Skip("RABBIT_ENDPOINT not set")
		}
		if !allowedTestDomains()[os.Getenv("RABBIT_TEST_DOMAIN_ID")] {
			t.Skipf("RABBIT_TEST_DOMAIN_ID=%q not in allow-list", os.Getenv("RABBIT_TEST_DOMAIN_ID"))
		}
		if os.Getenv("RABBIT_TEST_IMPERSONATE_SA_EMAIL") == "" || os.Getenv("RABBIT_TEST_IMPERSONATE_TARGET_EMAIL") == "" {
			t.Skip("RABBIT_TEST_IMPERSONATE_SA_EMAIL / TARGET_EMAIL not set")
		}
	}
}

// uniqueName produces a tfacc-prefixed name (required by safetyTransport).
func uniqueName(suffix string) string {
	buf := make([]byte, 3)
	_, _ = rand.Read(buf)
	return resourcePrefix + suffix + "-" + hex.EncodeToString(buf)
}

// providerConfigHCL is the HCL block every acceptance test prepends.
func providerConfigHCL() string {
	return fmt.Sprintf(`
provider "rabbit" {
  endpoint  = %q
  audience  = %q
  domain_id = %q
}
`, os.Getenv("RABBIT_ENDPOINT"), os.Getenv("RABBIT_AUDIENCE"), os.Getenv("RABBIT_TEST_DOMAIN_ID"))
}
