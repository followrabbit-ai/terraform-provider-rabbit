package client

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"
)

const (
	// DefaultEndpoint is the public Rabbit API host (prod).
	DefaultEndpoint = "https://api.followrabbit.ai"
	// DefaultAudience is the OAuth2 client ID Rabbit prod validates ID tokens against.
	DefaultAudience = "611447165175-7s37rdup9oehlufkjid7etsk51b44sa2.apps.googleusercontent.com"
	// DefaultTimeout for a single API call.
	DefaultTimeout = 30 * time.Second

	defaultUserAgent = "terraform-provider-rabbit"
)

// Client is a thin HTTP wrapper around the Rabbit REST API.
type Client struct {
	endpoint  *url.URL
	http      *http.Client
	userAgent string
	domainID  string
}

// Config is what the provider's Configure() builds.
type Config struct {
	Endpoint  string
	UserAgent string
	DomainID  string
	HTTP      *http.Client
}

// New returns a configured client. The provided HTTP client should already
// inject the Authorization header (e.g. via oauth2.NewClient).
func New(cfg Config) (*Client, error) {
	if cfg.HTTP == nil {
		return nil, errors.New("client.New: HTTP client is required")
	}
	endpoint := cfg.Endpoint
	if endpoint == "" {
		endpoint = DefaultEndpoint
	}
	u, err := url.Parse(endpoint)
	if err != nil {
		return nil, fmt.Errorf("client.New: invalid endpoint %q: %w", endpoint, err)
	}
	ua := cfg.UserAgent
	if ua == "" {
		ua = defaultUserAgent
	}
	return &Client{
		endpoint:  u,
		http:      cfg.HTTP,
		userAgent: ua,
		domainID:  cfg.DomainID,
	}, nil
}

// DomainID returns the provider-level default domain, or empty if unset.
func (c *Client) DomainID() string { return c.domainID }

// Do issues a JSON request. If body is non-nil it is marshalled. If out is
// non-nil the JSON response body is decoded into it.
func (c *Client) Do(ctx context.Context, method, path string, body, out any) error {
	var reqBody io.Reader
	if body != nil {
		buf, err := json.Marshal(body)
		if err != nil {
			return fmt.Errorf("marshal request body: %w", err)
		}
		reqBody = bytes.NewReader(buf)
	}

	// Allow the caller to pass "?query=..." appended to path.
	ref, err := url.Parse(path)
	if err != nil {
		return fmt.Errorf("invalid path %q: %w", path, err)
	}
	u := *c.endpoint
	u.Path = strings.TrimRight(u.Path, "/") + "/" + strings.TrimLeft(ref.Path, "/")
	u.RawQuery = ref.RawQuery

	req, err := http.NewRequestWithContext(ctx, method, u.String(), reqBody)
	if err != nil {
		return fmt.Errorf("build request: %w", err)
	}
	req.Header.Set("Accept", "application/json")
	req.Header.Set("User-Agent", c.userAgent)
	if reqBody != nil {
		req.Header.Set("Content-Type", "application/json")
	}

	resp, err := c.http.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode >= 400 {
		return newAPIError(resp)
	}
	if out == nil || resp.StatusCode == http.StatusNoContent {
		// Drain so the connection can be reused.
		_, _ = io.Copy(io.Discard, resp.Body)
		return nil
	}
	if err := json.NewDecoder(resp.Body).Decode(out); err != nil && !errors.Is(err, io.EOF) {
		return fmt.Errorf("decode response: %w", err)
	}
	return nil
}
