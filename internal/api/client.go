// Package api is the HTTP client of the Centreon observability platform.
//
// It deals with transport concerns only: URLs, authentication, request and
// response payloads, and error mapping. Mapping to the domain lives in
// internal/source/remote, so consumers never depend on this package directly.
package api

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"regexp"
	"strings"
	"time"

	"github.com/CentreonLabs/lazyobs/internal/secret"
	"github.com/CentreonLabs/lazyobs/internal/version"
)

// DefaultBaseURL is the platform API endpoint.
const DefaultBaseURL = "https://api.euwest1.obs.mycentreon.com"

const (
	apiKeyHeader     = "X-Api-Key" //nolint:gosec // header name, not a credential
	defaultTimeout   = 30 * time.Second
	maxResponseBytes = 64 << 20
	maxErrorBytes    = 4 << 10
)

var orgPattern = regexp.MustCompile(`^[a-zA-Z0-9_-]{1,50}$`)

// Client calls the platform API on behalf of one organization.
type Client struct {
	baseURL   *url.URL
	org       string
	token     secret.String
	http      *http.Client
	userAgent string
}

// Option customizes a Client.
type Option func(*Client)

// WithHTTPClient replaces the underlying HTTP client (timeouts, proxies, tests).
func WithHTTPClient(h *http.Client) Option { return func(c *Client) { c.http = h } }

// New returns a client for the given organization. An empty baseURL selects DefaultBaseURL.
func New(baseURL, org string, token secret.String, opts ...Option) (*Client, error) {
	if baseURL == "" {
		baseURL = DefaultBaseURL
	}
	u, err := url.Parse(strings.TrimRight(baseURL, "/"))
	if err != nil || (u.Scheme != "https" && u.Scheme != "http") || u.Host == "" {
		return nil, fmt.Errorf("api: invalid base URL %q: must be an absolute http(s) URL", baseURL)
	}
	if !orgPattern.MatchString(org) {
		return nil, fmt.Errorf("api: invalid organization code %q: expected 1-50 letters, digits, '-' or '_'", org)
	}
	if token.IsZero() {
		return nil, errors.New("api: missing API token")
	}
	c := &Client{
		baseURL:   u,
		org:       org,
		token:     token,
		http:      &http.Client{Timeout: defaultTimeout},
		userAgent: "lazyobs/" + version.Version,
	}
	for _, opt := range opts {
		opt(c)
	}
	return c, nil
}

// Org returns the organization code the client is bound to.
func (c *Client) Org() string { return c.org }

// orgURL builds an organization-scoped endpoint URL, escaping each segment.
func (c *Client) orgURL(segments ...string) string {
	escaped := make([]string, 0, len(segments)+3)
	escaped = append(escaped, "v1", "orgs", url.PathEscape(c.org))
	for _, s := range segments {
		escaped = append(escaped, url.PathEscape(s))
	}
	return c.baseURL.JoinPath(escaped...).String()
}

// do sends a JSON request and decodes a JSON response into out (if non-nil).
func (c *Client) do(ctx context.Context, method, endpoint string, body, out any) error {
	var reqBody io.Reader
	if body != nil {
		b, err := json.Marshal(body)
		if err != nil {
			return fmt.Errorf("api: encode request: %w", err)
		}
		reqBody = bytes.NewReader(b)
	}
	req, err := http.NewRequestWithContext(ctx, method, endpoint, reqBody)
	if err != nil {
		return fmt.Errorf("api: build request: %w", err)
	}
	req.Header.Set(apiKeyHeader, c.token.Reveal())
	req.Header.Set("Accept", "application/json")
	req.Header.Set("User-Agent", c.userAgent)
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}

	resp, err := c.http.Do(req)
	if err != nil {
		// The error embeds the method and URL only; headers (and the token) are never included.
		return fmt.Errorf("api: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode < 200 || resp.StatusCode > 299 {
		return newAPIError(resp)
	}
	if out == nil {
		return nil
	}
	if err := json.NewDecoder(io.LimitReader(resp.Body, maxResponseBytes)).Decode(out); err != nil {
		return fmt.Errorf("api: decode %s response: %w", req.URL.Path, err)
	}
	return nil
}
