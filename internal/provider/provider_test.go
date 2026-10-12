package provider

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

const testKey = "test-key-0123456789abcdef"

func testClient(t *testing.T, h http.Handler) (*Client, *int64) {
	t.Helper()
	var calls int64
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt64(&calls, 1)
		h.ServeHTTP(w, r)
	}))
	t.Cleanup(srv.Close)
	c := New(
		WithKey(testKey),
		WithBaseURL(srv.URL),
		WithBackoff(time.Millisecond),
		WithMaxAttempts(3),
	)
	return c, &calls
}

func okBody(content string) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.Header().Set("X-Routed-Via", "groq/llama-3.3-70b")
		_, _ = w.Write([]byte(`{"model":"llama-3.3-70b","choices":[{"message":{"role":"assistant","content":` + quote(content) + `}}],"usage":{"prompt_tokens":12,"completion_tokens":34}}`))
	})
}

func quote(s string) string {
	return `"` + s + `"`
}

func requestErr(t *testing.T, err error) *RequestError {
	t.Helper()
	if err == nil {
		t.Fatal("expected an error")
	}
	var rerr *RequestError
	if !errors.As(err, &rerr) {
		t.Fatalf("expected *RequestError, got %T: %v", err, err)
	}
	return rerr
}

// TestCompleteSuccess proves the happy path: auth header, request
// shape, and result metadata including gateway-reported usage.
func TestCompleteSuccess(t *testing.T) {
	var gotModel string
	var gotAuth string
	c, calls := testClient(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var body chatRequest
		if err := decodeJSON(r, &body); err != nil {
			t.Errorf("decoding request: %v", err)
		}
		gotModel = body.Model
		gotAuth = r.Header.Get("Authorization")
		okBody("hello").ServeHTTP(w, r)
	}))
	res, err := c.Complete(context.Background(), Request{
		Task:     TaskCodeReview,
		Messages: []Message{{Role: "user", Content: "review this"}},
	})
	if err != nil {
		t.Fatalf("success must not error: %v", err)
	}
	if gotAuth != "Bearer "+testKey {
		t.Fatalf("credential must travel as a Bearer header, got %q", gotAuth)
	}
	if gotModel != "auto" {
		t.Fatalf("unlisted task must route to auto, got %q", gotModel)
	}
	if res.Content != "hello" || res.Model != "llama-3.3-70b" {
		t.Fatalf("result content wrong: %+v", res)
	}
	if res.RoutedVia != "groq/llama-3.3-70b" || res.Attempts != 1 {
		t.Fatalf("result metadata wrong: %+v", res)
	}
	if res.PromptTokens != 12 || res.CompletionTokens != 34 {
		t.Fatalf("reported usage must be recorded: %+v", res)
	}
	if res.Fallback {
		t.Fatal("no fallback header means no fallback")
	}
	if atomic.LoadInt64(calls) != 1 {
		t.Fatalf("success must take exactly one attempt, took %d", *calls)
	}
}

// TestUnreportedUsageStaysUnreported proves token counts are never
// fabricated when the gateway omits them.
func TestUnreportedUsageStaysUnreported(t *testing.T) {
	c, _ := testClient(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"model":"m","choices":[{"message":{"role":"assistant","content":"hi"}}]}`))
	}))
	res, err := c.Complete(context.Background(), Request{Task: TaskDocumentation, Messages: []Message{{Role: "user", Content: "hi"}}})
	if err != nil {
		t.Fatal(err)
	}
	if res.PromptTokens != -1 || res.CompletionTokens != -1 {
		t.Fatalf("unreported usage must stay -1: %+v", res)
	}
}

// TestTaskRoutingHonorsOverrides proves configured routes win and
// unlisted tasks fall back to auto.
func TestTaskRoutingHonorsOverrides(t *testing.T) {
	var models []string
	c, _ := testClient(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var body chatRequest
		_ = decodeJSON(r, &body)
		models = append(models, body.Model)
		okBody("x").ServeHTTP(w, r)
	}))
	c.routes = map[Task]string{TaskCodeReview: "my-model"}
	req := Request{Messages: []Message{{Role: "user", Content: "x"}}}
	req.Task = TaskCodeReview
	if _, err := c.Complete(context.Background(), req); err != nil {
		t.Fatal(err)
	}
	req.Task = TaskDocumentation
	if _, err := c.Complete(context.Background(), req); err != nil {
		t.Fatal(err)
	}
	if len(models) != 2 || models[0] != "my-model" || models[1] != "auto" {
		t.Fatalf("routing wrong: %v", models)
	}
}

// TestAuthFailsFast proves 401 classifies as auth and never retries.
func TestAuthFailsFast(t *testing.T) {
	c, calls := testClient(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
	}))
	rerr := requestErr(t, mustComplete(t, c))
	if rerr.Kind != KindAuth {
		t.Fatalf("401 must be auth, got %v", rerr.Kind)
	}
	if atomic.LoadInt64(calls) != 1 {
		t.Fatalf("auth failure must not retry, took %d attempts", *calls)
	}
}

// TestInvalidRequestFailsFast proves 400 classifies without retry.
func TestInvalidRequestFailsFast(t *testing.T) {
	c, calls := testClient(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusBadRequest)
		_, _ = w.Write([]byte(`{"message":"unknown profile"}`))
	}))
	rerr := requestErr(t, mustComplete(t, c))
	if rerr.Kind != KindInvalidRequest {
		t.Fatalf("400 must be invalid-request, got %v", rerr.Kind)
	}
	if !strings.Contains(rerr.Message, "unknown profile") {
		t.Fatalf("gateway message must survive: %q", rerr.Message)
	}
	if atomic.LoadInt64(calls) != 1 {
		t.Fatalf("invalid request must not retry, took %d attempts", *calls)
	}
}

// TestRateLimitRecovers proves a 429 is retried within bounds and the
// retry succeeds.
func TestRateLimitRecovers(t *testing.T) {
	var n int64
	c, _ := testClient(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if atomic.AddInt64(&n, 1) == 1 {
			w.Header().Set("Retry-After", "0")
			w.WriteHeader(http.StatusTooManyRequests)
			return
		}
		okBody("recovered").ServeHTTP(w, r)
	}))
	res, err := c.Complete(context.Background(), Request{Task: TaskCodeGeneration, Messages: []Message{{Role: "user", Content: "x"}}})
	if err != nil {
		t.Fatalf("rate limit must recover within bounds: %v", err)
	}
	if res.Content != "recovered" || res.Attempts != 2 {
		t.Fatalf("result wrong after recovery: %+v", res)
	}
}

// TestRateLimitExhaustsAsRateLimit proves a 429 streak ends as
// rate-limit after exactly maxAttempts: the caller pauses, and there
// is no paid fallback to fall back to.
func TestRateLimitExhaustsAsRateLimit(t *testing.T) {
	c, calls := testClient(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusTooManyRequests)
	}))
	rerr := requestErr(t, mustComplete(t, c))
	if rerr.Kind != KindRateLimit {
		t.Fatalf("429 streak must be rate-limit, got %v", rerr.Kind)
	}
	if rerr.Attempts != 3 || atomic.LoadInt64(calls) != 3 {
		t.Fatalf("must stop at maxAttempts=3, attempts=%d calls=%d", rerr.Attempts, *calls)
	}
}

// TestServerErrorRetriesBounded proves 5xx retries stop at the bound.
func TestServerErrorRetriesBounded(t *testing.T) {
	c, calls := testClient(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusBadGateway)
	}))
	rerr := requestErr(t, mustComplete(t, c))
	if rerr.Kind != KindServer {
		t.Fatalf("502 must be server, got %v", rerr.Kind)
	}
	if atomic.LoadInt64(calls) != 3 {
		t.Fatalf("server errors must retry to the bound, took %d", *calls)
	}
}

// TestTimeoutClassifies proves a hung gateway becomes a timeout, not a
// network error and not a hang.
func TestTimeoutClassifies(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		time.Sleep(300 * time.Millisecond)
	}))
	t.Cleanup(srv.Close)
	c := New(WithKey(testKey), WithBaseURL(srv.URL),
		WithHTTPClient(&http.Client{Timeout: 50 * time.Millisecond}),
		WithBackoff(time.Millisecond), WithMaxAttempts(1))
	rerr := requestErr(t, mustComplete(t, c))
	if rerr.Kind != KindTimeout {
		t.Fatalf("hung gateway must be timeout, got %v", rerr.Kind)
	}
}

// TestNetworkFailureClassifies proves a refused connection is a
// network failure (retryable) with bounded attempts.
func TestNetworkFailureClassifies(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {}))
	url := srv.URL
	srv.Close()
	c := New(WithKey(testKey), WithBaseURL(url),
		WithBackoff(time.Millisecond), WithMaxAttempts(2))
	rerr := requestErr(t, mustComplete(t, c))
	if rerr.Kind != KindNetwork {
		t.Fatalf("refused connection must be network, got %v", rerr.Kind)
	}
	if rerr.Attempts != 2 {
		t.Fatalf("network failures must retry to the bound, got %d", rerr.Attempts)
	}
}

// TestInvalidOutputFailsFast proves a 2xx with a foreign body is
// invalid-output, never retried, never executed.
func TestInvalidOutputFailsFast(t *testing.T) {
	c, calls := testClient(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`not json`))
	}))
	rerr := requestErr(t, mustComplete(t, c))
	if rerr.Kind != KindInvalidOutput {
		t.Fatalf("foreign body must be invalid-output, got %v", rerr.Kind)
	}
	if atomic.LoadInt64(calls) != 1 {
		t.Fatalf("invalid output must not retry, took %d", *calls)
	}
}

// TestEmptyResponseFailsFast proves a well-formed but contentless 2xx
// is empty-response.
func TestEmptyResponseFailsFast(t *testing.T) {
	for _, body := range []string{`{"choices":[]}`, `{"choices":[{"message":{"role":"assistant","content":"  "}}]}`} {
		c, _ := testClient(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			_, _ = w.Write([]byte(body))
		}))
		rerr := requestErr(t, mustComplete(t, c))
		if rerr.Kind != KindEmptyResponse {
			t.Fatalf("contentless body %q must be empty-response, got %v", body, rerr.Kind)
		}
	}
}

// TestMissingKeyFailsWithoutRequest proves misconfiguration surfaces
// before any network call.
func TestMissingKeyFailsWithoutRequest(t *testing.T) {
	c, calls := testClient(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {}))
	c.key = ""
	rerr := requestErr(t, mustComplete(t, c))
	if rerr.Kind != KindInvalidConfig {
		t.Fatalf("missing key must be invalid-config, got %v", rerr.Kind)
	}
	if atomic.LoadInt64(calls) != 0 {
		t.Fatal("misconfigured client must make zero requests")
	}
}

// TestSecretRedacted proves the credential never appears in errors,
// even when the gateway echoes it.
func TestSecretRedacted(t *testing.T) {
	c, _ := testClient(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusBadRequest)
		_, _ = w.Write([]byte(`{"message":"bad key ` + testKey + `"}`))
	}))
	rerr := requestErr(t, mustComplete(t, c))
	if strings.Contains(rerr.Message, testKey) {
		t.Fatalf("credential leaked into error: %q", rerr.Message)
	}
	if !strings.Contains(rerr.Message, "[REDACTED]") {
		t.Fatalf("redaction marker missing: %q", rerr.Message)
	}
}

// TestListReadyReturnsIDs proves availability discovery parses the
// models shape.
func TestListReadyReturnsIDs(t *testing.T) {
	c, _ := testClient(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/models" || r.URL.Query().Get("execution_status") != "ready" {
			t.Errorf("must query ready models, got %s", r.URL)
		}
		_, _ = w.Write([]byte(`{"data":[{"id":"a"},{"id":"b"},{"id":""}]}`))
	}))
	ids, err := c.ListReady(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(ids) != 2 || ids[0] != "a" || ids[1] != "b" {
		t.Fatalf("ready IDs wrong: %v", ids)
	}
}

// TestListReadyAuthFails proves listing classifies auth failures.
func TestListReadyAuthFails(t *testing.T) {
	c, _ := testClient(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
	}))
	_, err := c.ListReady(context.Background())
	if rerr := requestErr(t, err); rerr.Kind != KindAuth {
		t.Fatalf("401 on listing must be auth, got %v", rerr.Kind)
	}
}

// TestResolveConfigReadsEnv proves env wiring with gateway defaults.
func TestResolveConfigReadsEnv(t *testing.T) {
	t.Setenv(EnvKey, "env-key")
	t.Setenv(EnvBaseURL, "http://gw:8080/v1")
	c := ResolveConfig()
	if c.key != "env-key" || c.baseURL != "http://gw:8080/v1" {
		t.Fatalf("env config wrong: %+v", c)
	}
}

// TestResolveConfigDefaults proves an empty environment yields the
// local-gateway default endpoint.
func TestResolveConfigDefaults(t *testing.T) {
	t.Setenv(EnvKey, "")
	t.Setenv(EnvBaseURL, "")
	c := ResolveConfig()
	if c.baseURL != DefaultBaseURL {
		t.Fatalf("default endpoint wrong: %q", c.baseURL)
	}
}

// TestParseRetryAfter proves the hint parser accepts seconds and
// rejects everything else without erroring.
func TestParseRetryAfter(t *testing.T) {
	if got := parseRetryAfter("7"); got != 7*time.Second {
		t.Fatalf("seconds must parse, got %v", got)
	}
	for _, v := range []string{"", "soon", "-3", "HTTP-date"} {
		if got := parseRetryAfter(v); got != 0 {
			t.Fatalf("hint %q must be ignored, got %v", v, got)
		}
	}
}

func mustComplete(t *testing.T, c *Client) error {
	t.Helper()
	_, err := c.Complete(context.Background(), Request{
		Task:     TaskCodeReview,
		Messages: []Message{{Role: "user", Content: "x"}},
	})
	return err
}

func decodeJSON(r *http.Request, out *chatRequest) error {
	defer r.Body.Close()
	return json.NewDecoder(r.Body).Decode(out)
}
