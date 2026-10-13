package provider

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math/rand/v2"
	"net/http"
	"net/url"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/FernSyntax/repoforge/internal/version"
)

const (
	// DefaultBaseURL targets a local gateway; remote operation is
	// explicit configuration (ADR-0016).
	DefaultBaseURL = "http://localhost:3001/v1"
	// DefaultRoute lets the gateway router pick the model.
	DefaultRoute = "auto"
	// EnvKey carries the unified gateway credential. The only
	// credential source: it is read at call time and never persisted.
	EnvKey = "FREELLMAPI_KEY"
	// EnvBaseURL overrides the gateway endpoint.
	EnvBaseURL = "FREELLMAPI_BASE_URL"

	defaultTimeout     = 60 * time.Second
	defaultMaxAttempts = 3
	defaultBackoff     = 500 * time.Millisecond
	maxRetryAfterWait  = 30 * time.Second
	maxBody            = 1 << 20
)

// Task is a logical unit of inference work. Callers name the task;
// Routes resolve it to a model route. New tasks are additive.
type Task string

// Logical tasks with a small, maintainable default routing policy:
// every task starts on the gateway router ("auto") until measured
// evidence justifies pinning it elsewhere.
const (
	TaskRequirementDiscovery Task = "requirement-discovery"
	TaskSpecDrafting         Task = "spec-drafting"
	TaskArchitectureReview   Task = "architecture-review"
	TaskSchemaProposal       Task = "schema-proposal"
	TaskCodeGeneration       Task = "code-generation"
	TaskTestGeneration       Task = "test-generation"
	TaskCodeReview           Task = "code-review"
	TaskSecurityReview       Task = "security-review"
	TaskDocumentation        Task = "documentation"
	// Review-mesh tasks for the LangGraph agent mesh (additive;
	// unlisted tasks resolve to DefaultRoute like all others).
	TaskDesign        Task = "design"
	TaskAccessibility Task = "accessibility"
	TaskSEO           Task = "seo"
	TaskPerformance   Task = "performance"
	TaskI18n          Task = "i18n"
)

// Message is one turn of a completion request.
type Message struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

// Request is a text-completion call. MaxTokens of 0 omits the cap.
type Request struct {
	Task      Task
	Messages  []Message
	MaxTokens int
}

// Result is a completed inference call with operational metadata. Token
// usage is recorded only when the gateway reports it; it is never
// fabricated.
type Result struct {
	Content string
	// Model is the serving model as reported by the gateway.
	Model string
	// RoutedVia names the provider hop from the gateway's
	// X-Routed-Via header, when present.
	RoutedVia string
	// Fallback reports gateway-side failover (X-Fallback-Attempts > 0).
	Fallback bool
	// Attempts is the total client attempts made.
	Attempts int
	// PromptTokens and CompletionTokens come from the gateway's usage
	// block when present; -1 means unreported.
	PromptTokens     int
	CompletionTokens int
}

// Client is an instance-based inference client. Construct with New;
// no global state is created or mutated.
type Client struct {
	http        *http.Client
	baseURL     string
	key         string
	routes      map[Task]string
	maxAttempts int
	backoff     time.Duration
	userAgent   string
}

// Option configures a Client.
type Option func(*Client)

// WithKey sets the gateway credential.
func WithKey(key string) Option {
	return func(c *Client) { c.key = key }
}

// WithBaseURL overrides the gateway endpoint.
func WithBaseURL(u string) Option {
	return func(c *Client) { c.baseURL = strings.TrimRight(u, "/") }
}

// WithRoutes overrides task-to-model routing. Unlisted tasks resolve
// to DefaultRoute.
func WithRoutes(r map[Task]string) Option {
	return func(c *Client) { c.routes = r }
}

// WithHTTPClient injects an HTTP client (tests). It should carry its
// own timeout.
func WithHTTPClient(h *http.Client) Option {
	return func(c *Client) { c.http = h }
}

// WithMaxAttempts bounds total attempts per call (failover included).
func WithMaxAttempts(n int) Option {
	return func(c *Client) {
		if n > 0 {
			c.maxAttempts = n
		}
	}
}

// WithBackoff overrides the base retry backoff (mainly for tests).
func WithBackoff(d time.Duration) Option {
	return func(c *Client) { c.backoff = d }
}

// New constructs a Client.
func New(opts ...Option) *Client {
	c := &Client{
		http:        &http.Client{Timeout: defaultTimeout},
		baseURL:     DefaultBaseURL,
		routes:      map[Task]string{},
		maxAttempts: defaultMaxAttempts,
		backoff:     defaultBackoff,
		userAgent:   "repoforge/" + version.Version,
	}
	for _, o := range opts {
		o(c)
	}
	return c
}

// ResolveConfig builds a Client from the environment: FREELLMAPI_KEY
// for the credential, FREELLMAPI_BASE_URL for the endpoint, defaults
// otherwise. A missing key is not an error here; the first call fails
// fast with KindInvalidConfig so misconfiguration surfaces at the
// boundary, not at startup.
func ResolveConfig() *Client {
	return New(
		WithKey(os.Getenv(EnvKey)),
		WithBaseURL(firstNonEmpty(os.Getenv(EnvBaseURL), DefaultBaseURL)),
	)
}

func firstNonEmpty(ss ...string) string {
	for _, s := range ss {
		if s != "" {
			return s
		}
	}
	return ""
}

// RouteFor resolves a task to its model route: the configured override,
// or DefaultRoute when unlisted.
func (c *Client) RouteFor(t Task) string {
	if r, ok := c.routes[t]; ok && r != "" {
		return r
	}
	return DefaultRoute
}

type chatRequest struct {
	Model     string    `json:"model"`
	Messages  []Message `json:"messages"`
	MaxTokens *int      `json:"max_tokens,omitempty"`
}

type chatResponse struct {
	Model   string `json:"model"`
	Choices []struct {
		Message struct {
			Role    string `json:"role"`
			Content string `json:"content"`
		} `json:"message"`
	} `json:"choices"`
	Usage *struct {
		PromptTokens     int `json:"prompt_tokens"`
		CompletionTokens int `json:"completion_tokens"`
	} `json:"usage"`
}

type modelsResponse struct {
	Data []struct {
		ID string `json:"id"`
	} `json:"data"`
}

// Complete performs one text-completion call with bounded, classified
// retries. Retryable failures (rate-limit, server, network, timeout)
// are attempted up to maxAttempts with exponential backoff and jitter;
// deterministic failures return immediately. A 429 streak exhausts as
// KindRateLimit so the caller pauses; there is no paid fallback.
func (c *Client) Complete(ctx context.Context, req Request) (Result, error) {
	if c.key == "" {
		return Result{}, &RequestError{Kind: KindInvalidConfig, Message: "missing gateway credential (set " + EnvKey + ")", Attempts: 1}
	}
	if c.baseURL == "" {
		return Result{}, &RequestError{Kind: KindInvalidConfig, Message: "missing gateway endpoint", Attempts: 1}
	}
	route := c.RouteFor(req.Task)
	if route == "" {
		return Result{}, &RequestError{Kind: KindNoEligibleRoute, Message: "no model route configured for task " + string(req.Task), Attempts: 1}
	}
	body := chatRequest{Model: route, Messages: req.Messages}
	if req.MaxTokens > 0 {
		body.MaxTokens = &req.MaxTokens
	}
	var last *RequestError
	for attempt := 1; attempt <= c.maxAttempts; attempt++ {
		res, rerr := c.attempt(ctx, body)
		if rerr == nil {
			res.Attempts = attempt
			return res, nil
		}
		rerr.Attempts = attempt
		last = rerr
		if !rerr.Kind.Retryable() || attempt == c.maxAttempts {
			return Result{}, rerr
		}
		if err := sleep(ctx, backoffFor(c.backoff, attempt, rerr.RetryAfter)); err != nil {
			return Result{}, &RequestError{Kind: KindTimeout, Message: "retry wait interrupted: " + err.Error(), Attempts: attempt}
		}
	}
	return Result{}, last
}

// backoffFor combines exponential backoff with the gateway's
// Retry-After hint (capped) and a small jitter. The hint wins when it
// asks for longer than the schedule, so a 429 pause is actually
// observed instead of hammered through.
func backoffFor(base time.Duration, attempt int, hint time.Duration) time.Duration {
	wait := base * time.Duration(1<<uint(attempt-1))
	if hint > 0 && hint < maxRetryAfterWait && hint > wait {
		wait = hint
	}
	if wait > maxRetryAfterWait {
		wait = maxRetryAfterWait
	}
	return wait + time.Duration(rand.Int64N(int64(100*time.Millisecond)))
}

func sleep(ctx context.Context, d time.Duration) error {
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-time.After(d):
		return nil
	}
}

func (c *Client) attempt(ctx context.Context, body chatRequest) (Result, *RequestError) {
	raw, err := json.Marshal(body)
	if err != nil {
		return Result{}, &RequestError{Kind: KindInvalidRequest, Message: "encoding request: " + err.Error()}
	}
	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, c.baseURL+"/chat/completions", bytes.NewReader(raw))
	if err != nil {
		return Result{}, &RequestError{Kind: KindInvalidRequest, Message: "building request: " + err.Error()}
	}
	httpReq.Header.Set("Content-Type", "application/json")
	httpReq.Header.Set("Authorization", "Bearer "+c.key)
	httpReq.Header.Set("User-Agent", c.userAgent)
	resp, err := c.http.Do(httpReq)
	if err != nil {
		return Result{}, classifyTransport(err)
	}
	defer drainAndClose(resp.Body)
	routedVia := resp.Header.Get("X-Routed-Via")
	fallback := fallbackAttempts(resp.Header.Get("X-Fallback-Attempts")) > 0
	if resp.StatusCode >= 400 {
		return Result{}, c.errorFor(resp)
	}
	payload, err := io.ReadAll(io.LimitReader(resp.Body, maxBody))
	if err != nil {
		return Result{}, &RequestError{Kind: KindNetwork, Message: "reading response: " + err.Error()}
	}
	var cr chatResponse
	if err := json.Unmarshal(payload, &cr); err != nil {
		return Result{}, &RequestError{Kind: KindInvalidOutput, StatusCode: resp.StatusCode, Message: "decoding response: " + err.Error()}
	}
	if len(cr.Choices) == 0 || strings.TrimSpace(cr.Choices[0].Message.Content) == "" {
		return Result{}, &RequestError{Kind: KindEmptyResponse, StatusCode: resp.StatusCode, Message: "gateway returned no usable content"}
	}
	res := Result{
		Content:          cr.Choices[0].Message.Content,
		Model:            cr.Model,
		RoutedVia:        routedVia,
		Fallback:         fallback,
		PromptTokens:     -1,
		CompletionTokens: -1,
	}
	if cr.Usage != nil {
		res.PromptTokens = cr.Usage.PromptTokens
		res.CompletionTokens = cr.Usage.CompletionTokens
	}
	return res, nil
}

func classifyTransport(err error) *RequestError {
	var urlErr *url.Error
	if errors.As(err, &urlErr) && urlErr.Timeout() {
		return &RequestError{Kind: KindTimeout, Message: "request timed out: " + redact(urlErr.Error(), "")}
	}
	if errors.Is(err, context.DeadlineExceeded) || errors.Is(err, context.Canceled) {
		return &RequestError{Kind: KindTimeout, Message: "request timed out: " + err.Error()}
	}
	return &RequestError{Kind: KindNetwork, Message: "request failed: " + err.Error()}
}

// errorFor classifies an HTTP error status with the gateway's message
// and Retry-After hint. The credential is redacted before embedding.
func (c *Client) errorFor(resp *http.Response) *RequestError {
	body, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<16))
	redacted := redact(string(body), c.key)
	rerr := &RequestError{StatusCode: resp.StatusCode}
	var eb struct {
		Message string `json:"message"`
		Error   any    `json:"error"`
	}
	if err := json.Unmarshal([]byte(redacted), &eb); err == nil {
		rerr.Message = eb.Message
		if rerr.Message == "" {
			rerr.Message = strings.TrimSpace(fmt.Sprint(eb.Error))
		}
	} else {
		rerr.Message = strings.TrimSpace(redacted)
	}
	if rerr.Message == "" || rerr.Message == "<nil>" {
		rerr.Message = http.StatusText(resp.StatusCode)
	}
	switch {
	case resp.StatusCode == 401 || resp.StatusCode == 403:
		rerr.Kind = KindAuth
	case resp.StatusCode == 429:
		rerr.Kind = KindRateLimit
		rerr.RetryAfter = parseRetryAfter(resp.Header.Get("Retry-After"))
	case resp.StatusCode >= 500:
		rerr.Kind = KindServer
	default:
		rerr.Kind = KindInvalidRequest
	}
	return rerr
}

func redact(body, key string) string {
	if key == "" {
		return body
	}
	return strings.ReplaceAll(body, key, "[REDACTED]")
}

func parseRetryAfter(v string) time.Duration {
	if v == "" {
		return 0
	}
	if secs, err := strconv.Atoi(strings.TrimSpace(v)); err == nil && secs >= 0 {
		return time.Duration(secs) * time.Second
	}
	return 0
}

func fallbackAttempts(v string) int {
	n, _ := strconv.Atoi(strings.TrimSpace(v))
	if n < 0 {
		return 0
	}
	return n
}

// ListReady returns the IDs of models the gateway reports as currently
// servable. Availability is discovered here, never assumed elsewhere.
func (c *Client) ListReady(ctx context.Context) ([]string, error) {
	if c.key == "" {
		return nil, &RequestError{Kind: KindInvalidConfig, Message: "missing gateway credential (set " + EnvKey + ")", Attempts: 1}
	}
	if c.baseURL == "" {
		return nil, &RequestError{Kind: KindInvalidConfig, Message: "missing gateway endpoint", Attempts: 1}
	}
	httpReq, err := http.NewRequestWithContext(ctx, http.MethodGet, c.baseURL+"/models?execution_status=ready", nil)
	if err != nil {
		return nil, &RequestError{Kind: KindInvalidRequest, Message: "building request: " + err.Error(), Attempts: 1}
	}
	httpReq.Header.Set("Authorization", "Bearer "+c.key)
	httpReq.Header.Set("User-Agent", c.userAgent)
	resp, err := c.http.Do(httpReq)
	if err != nil {
		rerr := classifyTransport(err)
		rerr.Attempts = 1
		return nil, rerr
	}
	defer drainAndClose(resp.Body)
	if resp.StatusCode >= 400 {
		rerr := c.errorFor(resp)
		rerr.Attempts = 1
		return nil, rerr
	}
	payload, err := io.ReadAll(io.LimitReader(resp.Body, maxBody))
	if err != nil {
		return nil, &RequestError{Kind: KindNetwork, Message: "reading response: " + err.Error(), Attempts: 1}
	}
	var mr modelsResponse
	if err := json.Unmarshal(payload, &mr); err != nil {
		return nil, &RequestError{Kind: KindInvalidOutput, StatusCode: resp.StatusCode, Message: "decoding response: " + err.Error(), Attempts: 1}
	}
	ids := make([]string, 0, len(mr.Data))
	for _, m := range mr.Data {
		if m.ID != "" {
			ids = append(ids, m.ID)
		}
	}
	return ids, nil
}

func drainAndClose(body io.ReadCloser) {
	_, _ = io.Copy(io.Discard, io.LimitReader(body, 1<<16))
	_ = body.Close()
}
