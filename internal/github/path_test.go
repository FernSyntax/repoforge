package github

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
)

// TestPathComponentSafety is the table for the path-segment contract.
// A component that is empty, a relative reference, or that carries a
// character with meaning in a URL path must never reach a request line.
func TestPathComponentSafety(t *testing.T) {
	safe := []string{
		"owner", "repo", "my-repo", "my_repo", "a.b", "A1", "repo-123",
		"main", "release-tags", "repoforge:main",
		"type:bug", "café", "日本語", "a+b", "a=b", "a;b", "a:b", "a@b",
		"a,b", "a!b", "a~b", "a(b)", "a[b]", "a'b", "a&b",
	}
	unsafe := []string{
		"", ".", "..", "/", "a/b", "a//b", "/abs", "./a", "../a", "a/../b",
		"a?b", "a#b", "a%b", "a b", "a\tb", "a\x00b", "a\nb",
		"%2e%2e", "..%2f", "a\rb",
	}
	for _, c := range safe {
		if !isSafeComponent(c) {
			t.Errorf("isSafeComponent(%q) = false, want true", c)
		}
	}
	for _, c := range unsafe {
		if isSafeComponent(c) {
			t.Errorf("isSafeComponent(%q) = true, want false", c)
		}
	}
}

// TestRepoPathRejectsUnsafeComponents proves the builder refuses a slug
// that would change the shape or target of the request.
func TestRepoPathRejectsUnsafeComponents(t *testing.T) {
	cases := []struct{ name, owner, repo string }{
		{"slash in repo", "owner", "repo/with-slash"},
		{"dotdot owner", "..", "repo"},
		{"dotdot repo", "owner", ".."},
		{"dot owner", ".", "repo"},
		{"empty repo", "owner", ""},
		{"empty owner", "", "repo"},
		{"query in repo", "owner", "repo?admin=1"},
		{"fragment in repo", "owner", "repo#frag"},
		{"percent in repo", "owner", "%2e%2e%2fescape"},
		{"space in repo", "owner", "repo with spaces"},
		{"double slash", "owner", "a//b"},
		{"absolute repo", "owner", "/etc"},
		{"newline in repo", "owner", "repo\nx"},
	}
	for _, c := range cases {
		_, err := repoPath(c.owner, c.repo)
		if err == nil {
			t.Errorf("%s: repoPath(%q, %q) must be refused", c.name, c.owner, c.repo)
			continue
		}
		if !strings.Contains(err.Error(), "unsafe path component") {
			t.Errorf("%s: error lacks actionable context: %v", c.name, err)
		}
	}
}

// TestRepoPathBuildsExpectedURLs proves the builder produces exactly
// the documented endpoint shapes, with escaping where it matters.
func TestRepoPathBuildsExpectedURLs(t *testing.T) {
	cases := []struct {
		owner, repo string
		elems       []string
		want        string
	}{
		{"o", "r", nil, "/repos/o/r"},
		{"o", "r", []string{"topics"}, "/repos/o/r/topics"},
		{"o", "r", []string{"rulesets"}, "/repos/o/r/rulesets"},
		{"o", "r", []string{"rulesets", "101"}, "/repos/o/r/rulesets/101"},
		{"o", "r", []string{"labels", "type:bug"}, "/repos/o/r/labels/type:bug"},
		{"o", "r", []string{"branches", "feature"}, "/repos/o/r/branches/feature"},
		{"a+b", "r", nil, "/repos/a+b/r"},
		{"o", "r", []string{"code-scanning", "default-setup"}, "/repos/o/r/code-scanning/default-setup"},
		{"o", "r", []string{"vulnerability-alerts"}, "/repos/o/r/vulnerability-alerts"},
	}
	for _, c := range cases {
		got, err := repoPath(c.owner, c.repo, c.elems...)
		if err != nil {
			t.Errorf("repoPath(%q, %q, %v): %v", c.owner, c.repo, c.elems, err)
			continue
		}
		if got != c.want {
			t.Errorf("repoPath(%q, %q, %v) = %q, want %q", c.owner, c.repo, c.elems, got, c.want)
		}
		if strings.ContainsAny(got, " \t\n") {
			t.Errorf("repoPath(%q, %q, %v) contains whitespace: %q", c.owner, c.repo, c.elems, got)
		}
		for _, seg := range strings.Split(got, "/") {
			if seg == ".." {
				t.Errorf("repoPath(%q, %q, %v) contains a .. segment: %q", c.owner, c.repo, c.elems, got)
			}
		}
	}
}

// TestRepoPathRefusesStructuralCharacters proves a "/" is refused
// rather than escaped: refusing is the only safe answer, because an
// escaped "%2F" is decoded by proxies before routing and a literal "/"
// would change the path shape.
func TestRepoPathRefusesStructuralCharacters(t *testing.T) {
	for _, c := range []struct{ owner, repo string }{
		{"o", "a/b"},
		{"o", "a b"},
		{"a?b", "r"},
		{"o", "c#d"},
		{"o", "a%2Fb"},
	} {
		if got, err := repoPath(c.owner, c.repo); err == nil {
			t.Errorf("repoPath(%q, %q) = %q, want refusal", c.owner, c.repo, got)
		}
	}
}

// TestBranchPathAndLabelPath cover the two data-bearing segment paths,
// where the segment is a branch or a label name chosen by an operator.
func TestBranchPathAndLabelPath(t *testing.T) {
	got, err := branchPath("o", "r", "main", "trunk")
	if err != nil || got != "/repos/o/r/branches/main/rename" {
		t.Errorf("branchPath = %q, %v", got, err)
	}
	// Ref names legitimately carry a hierarchy.
	if got, err := branchPath("o", "r", "feature/topic", "trunk"); err != nil || got != "/repos/o/r/branches/feature/topic/rename" {
		t.Errorf("branchPath(feature/topic) = %q, %v", got, err)
	}
	for _, bad := range []string{"../escape", "..", "feature//x", "feature/../x", ""} {
		if _, err := branchPath("o", "r", bad, "trunk"); err == nil {
			t.Errorf("branchPath must refuse branch %q", bad)
		}
	}

	got, err = labelPath("o", "r", "type:bug")
	if err != nil || got != "/repos/o/r/labels/type:bug" {
		t.Errorf("labelPath = %q, %v", got, err)
	}
	if _, err := labelPath("o", "r", "../escape"); err == nil {
		t.Error("labelPath must refuse an unsafe label name")
	}
	if _, err := labelPath("o", "r", "a/b"); err == nil {
		t.Error("labelPath must refuse a label name containing a separator")
	}
}

// --- fake transport -------------------------------------------------------

// recordingTransport records every request (method, URL path, body) and
// replies from a canned function. It is what lets the mutation tests
// assert the exact wire request rather than an in-Go approximation.
type recordingTransport struct {
	mu    sync.Mutex
	reqs  []recordedRequest
	reply func(*http.Request) *http.Response
}

type recordedRequest struct {
	method string
	path   string
	body   string
}

func (rt *recordingTransport) RoundTrip(r *http.Request) (*http.Response, error) {
	rt.mu.Lock()
	defer rt.mu.Unlock()
	var body []byte
	if r.Body != nil {
		body, _ = io.ReadAll(r.Body)
	}
	rt.reqs = append(rt.reqs, recordedRequest{method: r.Method, path: r.URL.Path, body: string(body)})
	return rt.reply(r), nil
}

func (rt *recordingTransport) last() recordedRequest {
	rt.mu.Lock()
	defer rt.mu.Unlock()
	if len(rt.reqs) == 0 {
		return recordedRequest{}
	}
	return rt.reqs[len(rt.reqs)-1]
}

func (rt *recordingTransport) count() int {
	rt.mu.Lock()
	defer rt.mu.Unlock()
	return len(rt.reqs)
}

func newRecording() (*Client, *recordingTransport) {
	rt := &recordingTransport{reply: func(*http.Request) *http.Response {
		return jsonResponse(200, map[string]string{})
	}}
	return New(WithHTTPClient(&http.Client{Transport: rt}), WithToken("t")), rt
}

func jsonResponse(status int, v any) *http.Response {
	data, _ := json.Marshal(v)
	return &http.Response{
		StatusCode: status,
		Header:     http.Header{"Content-Type": []string{"application/json"}},
		Body:       io.NopCloser(strings.NewReader(string(data))),
	}
}

func assertRequest(t *testing.T, rt *recordingTransport, method, path string) {
	t.Helper()
	got := rt.last()
	if got.method != method {
		t.Errorf("method = %s, want %s", got.method, method)
	}
	if got.path != path {
		t.Errorf("path = %q, want %q", got.path, path)
	}
}

// TestMutationsSendValidatedPaths asserts the real HTTP method, URL and
// body for every mutation, so a request can never be shaped by an
// unvalidated slug.
func TestMutationsSendValidatedPaths(t *testing.T) {
	t.Run("UpdateRepository", func(t *testing.T) {
		c, rt := newRecording()
		if err := c.UpdateRepository(context.Background(), "o", "r", RepoSettingsPatch{}); err != nil {
			t.Fatal(err)
		}
		assertRequest(t, rt, http.MethodPatch, "/repos/o/r")
	})
	t.Run("ReplaceTopics", func(t *testing.T) {
		c, rt := newRecording()
		if err := c.ReplaceTopics(context.Background(), "o", "r", []string{"a"}); err != nil {
			t.Fatal(err)
		}
		assertRequest(t, rt, http.MethodPut, "/repos/o/r/topics")
	})
	t.Run("RenameBranch", func(t *testing.T) {
		c, rt := newRecording()
		if err := c.RenameBranch(context.Background(), "o", "r", "main", "trunk"); err != nil {
			t.Fatal(err)
		}
		assertRequest(t, rt, http.MethodPost, "/repos/o/r/branches/main/rename")
		if !strings.Contains(rt.last().body, "trunk") {
			t.Errorf("body = %q, want new_name trunk", rt.last().body)
		}
	})
	t.Run("CreateRuleset", func(t *testing.T) {
		c, rt := newRecording()
		if _, err := c.CreateRuleset(context.Background(), "o", "r", RulesetPayload{}); err != nil {
			t.Fatal(err)
		}
		assertRequest(t, rt, http.MethodPost, "/repos/o/r/rulesets")
	})
	t.Run("UpdateRuleset", func(t *testing.T) {
		c, rt := newRecording()
		if err := c.UpdateRuleset(context.Background(), "o", "r", 101, RulesetPayload{}); err != nil {
			t.Fatal(err)
		}
		assertRequest(t, rt, http.MethodPut, "/repos/o/r/rulesets/101")
	})
	t.Run("CreateLabel", func(t *testing.T) {
		c, rt := newRecording()
		if err := c.CreateLabel(context.Background(), "o", "r", LabelPayload{}); err != nil {
			t.Fatal(err)
		}
		assertRequest(t, rt, http.MethodPost, "/repos/o/r/labels")
	})
	t.Run("UpdateLabel", func(t *testing.T) {
		c, rt := newRecording()
		if err := c.UpdateLabel(context.Background(), "o", "r", "type:bug", LabelPayload{}); err != nil {
			t.Fatal(err)
		}
		assertRequest(t, rt, http.MethodPatch, "/repos/o/r/labels/type:bug")
	})
	t.Run("SetVulnerabilityAlerts", func(t *testing.T) {
		c, rt := newRecording()
		if err := c.SetVulnerabilityAlerts(context.Background(), "o", "r", true); err != nil {
			t.Fatal(err)
		}
		assertRequest(t, rt, http.MethodPut, "/repos/o/r/vulnerability-alerts")
	})
	t.Run("SetAutomatedSecurityFixes", func(t *testing.T) {
		c, rt := newRecording()
		if err := c.SetAutomatedSecurityFixes(context.Background(), "o", "r", false); err != nil {
			t.Fatal(err)
		}
		assertRequest(t, rt, http.MethodDelete, "/repos/o/r/automated-security-fixes")
	})
	t.Run("SetPrivateVulnerabilityReporting", func(t *testing.T) {
		c, rt := newRecording()
		if err := c.SetPrivateVulnerabilityReporting(context.Background(), "o", "r", true); err != nil {
			t.Fatal(err)
		}
		assertRequest(t, rt, http.MethodPut, "/repos/o/r/private-vulnerability-reporting")
	})
	t.Run("SetSecretScanning", func(t *testing.T) {
		c, rt := newRecording()
		if err := c.SetSecretScanning(context.Background(), "o", "r", true, true); err != nil {
			t.Fatal(err)
		}
		assertRequest(t, rt, http.MethodPatch, "/repos/o/r")
		if !strings.Contains(rt.last().body, "secret_scanning") {
			t.Errorf("body = %q, want a security_and_analysis object", rt.last().body)
		}
	})
	t.Run("ConfigureCodeScanningDefaultSetup", func(t *testing.T) {
		c, rt := newRecording()
		if err := c.ConfigureCodeScanningDefaultSetup(context.Background(), "o", "r", []string{"go"}); err != nil {
			t.Fatal(err)
		}
		assertRequest(t, rt, http.MethodPatch, "/repos/o/r/code-scanning/default-setup")
	})

	// An unsafe slug must never produce a request at all.
	t.Run("unsafeRepoNeverRequests", func(t *testing.T) {
		c, rt := newRecording()
		if err := c.UpdateRepository(context.Background(), "o", "a/b", RepoSettingsPatch{}); err == nil {
			t.Error("an unsafe repository name must be refused")
		}
		if rt.count() != 0 {
			t.Errorf("a refused mutation still issued %d request(s)", rt.count())
		}
	})
}

// failingTransport returns a transport error, which is distinct from an
// HTTP error status: no response was ever read, so the outcome of a
// mutation is genuinely unknown.
type failingTransport struct{}

func (failingTransport) RoundTrip(*http.Request) (*http.Response, error) {
	return nil, http.ErrHandlerTimeout
}

// TestMutationFailureReportsUncertainty asserts that an ambiguous
// transport failure is not presented as a clean success or a clean
// failure: the operator is told the outcome is unknown and to re-read
// the actual state before doing anything else.
func TestMutationFailureReportsUncertainty(t *testing.T) {
	c := New(WithHTTPClient(&http.Client{Transport: failingTransport{}}))
	err := c.UpdateRepository(context.Background(), "o", "r", RepoSettingsPatch{})
	if err == nil {
		t.Fatal("a transport failure must not look like success")
	}
	msg := err.Error()
	for _, want := range []string{"UNKNOWN", "doctor"} {
		if !strings.Contains(msg, want) {
			t.Errorf("error %q does not contain %q", msg, want)
		}
	}
	if strings.Contains(strings.ToLower(msg), "retry") {
		t.Errorf("an uncertain mutation must never advise a retry: %q", msg)
	}
}

// TestSecretScanningPatchBody asserts the body sent for the
// security_and_analysis toggle — the object whose hard-coded `true`
// burned the project.
func TestSecretScanningPatchBody(t *testing.T) {
	cases := []struct {
		scanning, push bool
		wantScanning   string
		wantPush       string
	}{
		{true, true, "enabled", "enabled"},
		{true, false, "enabled", "disabled"},
		{false, true, "disabled", "enabled"},
		{false, false, "disabled", "disabled"},
	}
	for _, tc := range cases {
		c, rt := newRecording()
		if err := c.SetSecretScanning(context.Background(), "o", "r", tc.scanning, tc.push); err != nil {
			t.Fatal(err)
		}
		var body struct {
			SecurityAndAnalysis map[string]map[string]string `json:"security_and_analysis"`
		}
		if err := json.Unmarshal([]byte(rt.last().body), &body); err != nil {
			t.Fatalf("body %q: %v", rt.last().body, err)
		}
		if got := body.SecurityAndAnalysis["secret_scanning"]["status"]; got != tc.wantScanning {
			t.Errorf("scan(%v,%v): secret_scanning = %q, want %q",
				tc.scanning, tc.push, got, tc.wantScanning)
		}
		if got := body.SecurityAndAnalysis["secret_scanning_push_protection"]["status"]; got != tc.wantPush {
			t.Errorf("scan(%v,%v): secret_scanning_push_protection = %q, want %q",
				tc.scanning, tc.push, got, tc.wantPush)
		}
	}
}

// TestSecurityProbesUseValidatedPaths checks the read side too: every
// probe path must be built through the same validated builder.
func TestSecurityProbesUseValidatedPaths(t *testing.T) {
	var mu sync.Mutex
	var paths []string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		paths = append(paths, r.URL.Path)
		mu.Unlock()
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(200)
		_, _ = w.Write([]byte(`{"enabled":false}`))
	}))
	defer srv.Close()

	c := New(WithBaseURL(srv.URL))
	if _, err := c.GetSecuritySettings(context.Background(), "o", "r"); err != nil {
		t.Fatal(err)
	}
	mu.Lock()
	defer mu.Unlock()
	for _, want := range []string{
		"/repos/o/r/vulnerability-alerts",
		"/repos/o/r/automated-security-fixes",
		"/repos/o/r/private-vulnerability-reporting",
	} {
		found := false
		for _, p := range paths {
			if p == want {
				found = true
			}
		}
		if !found {
			t.Errorf("missing probe %q; got %v", want, paths)
		}
	}
	if _, err := c.GetSecuritySettings(context.Background(), "o", "a/b"); err == nil {
		t.Error("an unsafe repository name must be refused before any request")
	}
}
