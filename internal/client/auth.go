package client

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"os"
	"sync"
	"time"

	"golang.org/x/oauth2"
	"google.golang.org/api/iamcredentials/v1"
	"google.golang.org/api/idtoken"
	"google.golang.org/api/option"
)

// DefaultAudienceProd is reused from client.go.
//
// AuthConfig captures every knob that controls how the provider's HTTP client
// authenticates. Exactly one happy path: end-user / SA ADC → Google ID token
// with the configured audience.
type AuthConfig struct {
	// Audience is the OAuth2 client ID Rabbit validates the ID token's `aud`
	// claim against. Required.
	Audience string

	// CredentialsJSON is a service account JSON blob (file contents, not path).
	// Mutually exclusive with CredentialsFile.
	CredentialsJSON string

	// CredentialsFile is a path to a service account JSON file.
	CredentialsFile string

	// ImpersonateServiceAccount, when set, exchanges base ADC for an ID token
	// minted by IAM Credentials' generateIdToken on the target SA.
	ImpersonateServiceAccount string
}

// NewHTTPClient builds an *http.Client that injects a fresh Google ID token on
// every request. The token source applies the standard ADC fallback chain
// (CredentialsFile → CredentialsJSON → impersonation → metadata server).
func NewHTTPClient(ctx context.Context, cfg AuthConfig) (*http.Client, error) {
	ts, err := newIDTokenSource(ctx, cfg)
	if err != nil {
		return nil, err
	}
	return oauth2.NewClient(ctx, ts), nil
}

func newIDTokenSource(ctx context.Context, cfg AuthConfig) (oauth2.TokenSource, error) {
	if cfg.Audience == "" {
		return nil, errors.New("auth: audience is required")
	}

	if cfg.ImpersonateServiceAccount != "" {
		return newImpersonatedIDTokenSource(ctx, cfg)
	}

	var opts []idtoken.ClientOption
	if cfg.CredentialsJSON != "" {
		opts = append(opts, idtoken.WithCredentialsJSON([]byte(cfg.CredentialsJSON)))
	} else if cfg.CredentialsFile != "" {
		opts = append(opts, idtoken.WithCredentialsFile(cfg.CredentialsFile))
	}

	return idtoken.NewTokenSource(ctx, cfg.Audience, opts...)
}

// newImpersonatedIDTokenSource mints an ID token for the target SA using IAM
// Credentials' generateIdToken (standard Google impersonation pattern).
func newImpersonatedIDTokenSource(ctx context.Context, cfg AuthConfig) (oauth2.TokenSource, error) {
	var opts []option.ClientOption
	if cfg.CredentialsFile != "" {
		opts = append(opts, option.WithCredentialsFile(cfg.CredentialsFile))
	}
	if cfg.CredentialsJSON != "" {
		opts = append(opts, option.WithCredentialsJSON([]byte(cfg.CredentialsJSON)))
	}
	svc, err := iamcredentials.NewService(ctx, opts...)
	if err != nil {
		return nil, fmt.Errorf("iamcredentials: %w", err)
	}
	return oauth2.ReuseTokenSource(nil, &generateIDTokenSource{
		svc:      svc,
		target:   cfg.ImpersonateServiceAccount,
		audience: cfg.Audience,
	}), nil
}

type generateIDTokenSource struct {
	svc      *iamcredentials.Service
	target   string
	audience string
}

func (s *generateIDTokenSource) Token() (*oauth2.Token, error) {
	name := fmt.Sprintf("projects/-/serviceAccounts/%s", s.target)
	resp, err := s.svc.Projects.ServiceAccounts.GenerateIdToken(name, &iamcredentials.GenerateIdTokenRequest{
		Audience:     s.audience,
		IncludeEmail: true,
	}).Do()
	if err != nil {
		return nil, fmt.Errorf("generateIdToken: %w", err)
	}
	return &oauth2.Token{
		AccessToken: resp.Token,
		TokenType:   "Bearer",
		Expiry:      time.Now().Add(55 * time.Minute),
	}, nil
}

// -----------------------------------------------------------------------------
// Test-only auth: Rabbit's bespoke impersonation mechanism.
//
// The backend (BaseWebSecurityConfig.java) accepts a JWT signed by the dev
// impersonation SA's system-managed key. The payload includes:
//
//   email:        the impersonation SA email
//   target-email: the user the test wants to act as
//
// Audience is intentionally not validated for this path. We use
// iamcredentials.signJwt so we don't need the SA's private key locally — only
// the IAM permission roles/iam.serviceAccountTokenCreator on the SA.
//
// This is NOT exposed via the public provider schema; it's wired only from
// internal/provider/*_test.go.

// NewRabbitImpersonationHTTPClient is a test-only helper that returns an
// *http.Client which signs every request with a fresh impersonation JWT
// targeting `targetEmail`. Honors `RABBIT_TEST_IMPERSONATE_SA_EMAIL` and
// `RABBIT_TEST_IMPERSONATE_TARGET_EMAIL` if their args are empty.
func NewRabbitImpersonationHTTPClient(ctx context.Context, saEmail, targetEmail string) (*http.Client, error) {
	if saEmail == "" {
		saEmail = os.Getenv("RABBIT_TEST_IMPERSONATE_SA_EMAIL")
	}
	if targetEmail == "" {
		targetEmail = os.Getenv("RABBIT_TEST_IMPERSONATE_TARGET_EMAIL")
	}
	if saEmail == "" || targetEmail == "" {
		return nil, errors.New("rabbit impersonation requires SA email and target email")
	}
	svc, err := iamcredentials.NewService(ctx)
	if err != nil {
		return nil, fmt.Errorf("iamcredentials: %w", err)
	}
	ts := oauth2.ReuseTokenSource(nil, &rabbitImpersonationSource{
		svc:         svc,
		saEmail:     saEmail,
		targetEmail: targetEmail,
	})
	return oauth2.NewClient(ctx, ts), nil
}

type rabbitImpersonationSource struct {
	svc         *iamcredentials.Service
	saEmail     string
	targetEmail string

	mu      sync.Mutex
	current *oauth2.Token
}

func (s *rabbitImpersonationSource) Token() (*oauth2.Token, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	payload, err := json.Marshal(map[string]string{
		"email":        s.saEmail,
		"target-email": s.targetEmail,
	})
	if err != nil {
		return nil, err
	}
	name := fmt.Sprintf("projects/-/serviceAccounts/%s", s.saEmail)
	resp, err := s.svc.Projects.ServiceAccounts.SignJwt(name, &iamcredentials.SignJwtRequest{
		Payload: string(payload),
	}).Do()
	if err != nil {
		return nil, fmt.Errorf("signJwt: %w", err)
	}
	tok := &oauth2.Token{
		AccessToken: resp.SignedJwt,
		TokenType:   "Bearer",
		// iamcredentials.signJwt sets exp to ~1h. Refresh a touch sooner.
		Expiry: time.Now().Add(50 * time.Minute),
	}
	s.current = tok
	return tok, nil
}
