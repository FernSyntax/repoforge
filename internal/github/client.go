// Copyright the repoforge contributors.
//
// SPDX-License-Identifier: Apache-2.0

// Package github is the client boundary for the GitHub REST API, which
// is the source of truth for remote repository state (spec.md §7).
//
// The client is instance-based (no global mutable state), uses
// context.Context on every call, sets explicit timeouts, and never logs
// or exposes credentials: tokens are held unexported and redacted from
// all error output. Only read-only operations are implemented so far.
package github

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/FernSyntax/repoforge/internal/version"
)

const (
	// DefaultBaseURL is the public GitHub REST API endpoint.
	DefaultBaseURL = "https://api.github.com"
	// APIVersion is the pinned X-GitHub-Api-Version header value, per
	// current GitHub documentation at implementation time.
	APIVersion = "2026-03-10"

	acceptMediaType = "application/vnd.github+json"
	defaultTimeout  = 30 * time.Second
)

// Client is an instance-based GitHub REST client. Construct with New.
type Client struct {
	http         *http.Client
	baseURL      string
	token        string
	userAgent    string
	retryBackoff time.Duration
	maxRetries   int
}

// Option configures a Client.
type Option func(*Client)

// WithToken sets the credential. See ResolveToken for resolution order.
func WithToken(token string) Option {
	return func(c *Client) { c.token = token }
}

// WithHTTPClient injects an HTTP client (dependency injection for
// tests). The client should have its own timeout.
func WithHTTPClient(h *http.Client) Option {
	return func(c *Client) { c.http = h }
}

// WithBaseURL overrides the API endpoint (tests, GHES).
func WithBaseURL(u string) Option {
	return func(c *Client) { c.baseURL = strings.TrimRight(u, "/") }
}

// WithRetryBackoff overrides the base backoff between safe retries
// (mainly for tests).
func WithRetryBackoff(d time.Duration) Option {
	return func(c *Client) { c.retryBackoff = d }
}

// New constructs a Client. No global state is created or mutated.
func New(opts ...Option) *Client {
	c := &Client{
		http:         &http.Client{Timeout: defaultTimeout},
		baseURL:      DefaultBaseURL,
		userAgent:    "repoforge/" + version.Version,
		retryBackoff: 250 * time.Millisecond,
		maxRetries:   2,
	}
	for _, o := range opts {
		o(c)
	}
	return c
}

// RateLimit captures the GitHub rate-limit state from response headers.
type RateLimit struct {
	Limit     int
	Remaining int
	Reset     time.Time
}

// APIError is a structured GitHub API error. It never contains the
// credential: response bodies are redacted before being embedded.
type APIError struct {
	StatusCode int
	Message    string
	DocsURL    string
	// RateLimited reports a 403 caused by rate-limit exhaustion.
	RateLimited bool
	// Rate is the rate-limit state parsed from headers, when present.
	Rate *RateLimit
}

func (e *APIError) Error() string {
	if e.RateLimited {
		return fmt.Sprintf("github: rate limit exceeded (status %d, resets %s)", e.StatusCode, e.Rate.Reset.Format(time.RFC3339))
	}
	return fmt.Sprintf("github: status %d: %s", e.StatusCode, e.Message)
}

// IsNotFound reports whether err is a 404 from the API.
func IsNotFound(err error) bool {
	var ae *APIError
	if err != nil && errorAs(err, &ae) {
		return ae.StatusCode == 404
	}
	return false
}

// IsRateLimited reports whether err is a rate-limit rejection.
func IsRateLimited(err error) bool {
	var ae *APIError
	if err != nil && errorAs(err, &ae) {
		return ae.RateLimited
	}
	return false
}

func errorAs(err error, target **APIError) bool {
	for err != nil {
		if ae, ok := err.(*APIError); ok {
			*target = ae
			return true
		}
		if u, ok := err.(interface{ Unwrap() error }); ok {
			err = u.Unwrap()
			continue
		}
		break
	}
	return false
}

type errorBody struct {
	Message string `json:"message"`
	DocsURL string `json:"documentation_url"`
}

// get performs a GET request and decodes the JSON body into out.
func (c *Client) get(ctx context.Context, path string, out any) error {
	_, err := c.do(ctx, http.MethodGet, path, out)
	return err
}

// do performs the request with safe (GET-only, bounded) retries on
// transport errors and 502/503/504 responses.
func (c *Client) do(ctx context.Context, method, path string, out any) (*http.Response, error) {
	var lastErr error
	attempts := 1
	if method == http.MethodGet {
		attempts += c.maxRetries
	}
	for attempt := 0; attempt < attempts; attempt++ {
		if attempt > 0 {
			select {
			case <-ctx.Done():
				return nil, ctx.Err()
			case <-time.After(c.retryBackoff * time.Duration(attempt)):
			}
		}
		resp, err := c.roundTrip(ctx, method, path)
		if err != nil {
			lastErr = err
			continue // transport error: safe to retry GET
		}
		if retryableStatus(resp.StatusCode) && attempt+1 < attempts {
			drainAndClose(resp.Body)
			lastErr = &APIError{StatusCode: resp.StatusCode, Message: "server error"}
			continue
		}
		return c.decode(resp, out)
	}
	return nil, lastErr
}

func retryableStatus(code int) bool {
	return code == 502 || code == 503 || code == 504
}

func (c *Client) roundTrip(ctx context.Context, method, path string) (*http.Response, error) {
	req, err := http.NewRequestWithContext(ctx, method, c.baseURL+path, nil)
	if err != nil {
		return nil, fmt.Errorf("github: building request: %w", err)
	}
	req.Header.Set("Accept", acceptMediaType)
	req.Header.Set("X-GitHub-Api-Version", APIVersion)
	req.Header.Set("User-Agent", c.userAgent)
	if c.token != "" {
		req.Header.Set("Authorization", "Bearer "+c.token)
	}
	resp, err := c.http.Do(req)
	if err != nil {
		return nil, fmt.Errorf("github: request failed: %w", err)
	}
	return resp, nil
}

// decode handles status codes, rate-limit awareness, body decoding, and
// token redaction. A 2xx with nil out skips decoding (used for 204
// probes).
func (c *Client) decode(resp *http.Response, out any) (*http.Response, error) {
	defer drainAndClose(resp.Body)
	rate := parseRateLimit(resp.Header)
	if resp.StatusCode < 400 {
		if out != nil {
			if err := json.NewDecoder(resp.Body).Decode(out); err != nil {
				return resp, fmt.Errorf("github: decoding response: %w", err)
			}
		}
		return resp, nil
	}
	body, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<16))
	redacted := strings.ReplaceAll(string(body), c.token, "[REDACTED]")
	if c.token == "" {
		redacted = string(body)
	}
	ae := &APIError{StatusCode: resp.StatusCode, Rate: rate}
	var eb errorBody
	if err := json.Unmarshal([]byte(redacted), &eb); err == nil {
		ae.Message = eb.Message
		ae.DocsURL = eb.DocsURL
	} else {
		ae.Message = strings.TrimSpace(redacted)
	}
	if ae.Message == "" {
		ae.Message = http.StatusText(resp.StatusCode)
	}
	if resp.StatusCode == 403 && rate != nil && rate.Remaining == 0 {
		ae.RateLimited = true
	}
	return resp, ae
}

func drainAndClose(body io.ReadCloser) {
	_, _ = io.Copy(io.Discard, io.LimitReader(body, 1<<16))
	_ = body.Close()
}

func parseRateLimit(h http.Header) *RateLimit {
	remaining := h.Get("X-RateLimit-Remaining")
	if remaining == "" {
		return nil
	}
	r := &RateLimit{}
	r.Remaining, _ = strconv.Atoi(remaining)
	r.Limit, _ = strconv.Atoi(h.Get("X-RateLimit-Limit"))
	if reset, err := strconv.ParseInt(h.Get("X-RateLimit-Reset"), 10, 64); err == nil {
		r.Reset = time.Unix(reset, 0).UTC()
	}
	return r
}

// hasNextPage reports whether the Link header advertises a next page.
func hasNextPage(h http.Header) bool {
	for _, link := range h.Values("Link") {
		for _, part := range strings.Split(link, ",") {
			segs := strings.Split(part, ";")
			for _, s := range segs[1:] {
				if strings.TrimSpace(s) == `rel="next"` {
					return true
				}
			}
		}
	}
	return false
}

// getAll fetches every page of a list endpoint. Pagination is explicit
// (per_page=100&page=N), driven by the Link header's rel="next".
func getAll[T any](c *Client, ctx context.Context, path string) ([]T, error) {
	sep := "?"
	if strings.Contains(path, "?") {
		sep = "&"
	}
	var all []T
	for page := 1; ; page++ {
		paged := fmt.Sprintf("%s%sper_page=100&page=%d", path, sep, page)
		var items []T
		resp, err := c.do(ctx, http.MethodGet, paged, &items)
		if err != nil {
			return nil, err
		}
		all = append(all, items...)
		if !hasNextPage(resp.Header) {
			return all, nil
		}
	}
}
