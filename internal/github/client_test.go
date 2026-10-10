package github

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

const testToken = "test-token-0123456789abcdef"

func newTestClient(t *testing.T, handler http.Handler) (*Client, *httptest.Server) {
	t.Helper()
	srv := httptest.NewServer(handler)
	t.Cleanup(srv.Close)
	c := New(
		WithBaseURL(srv.URL),
		WithToken(testToken),
		WithRetryBackoff(time.Millisecond),
	)
	return c, srv
}

func TestGetRepository(t *testing.T) {
	c, _ := newTestClient(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/repos/o/r" {
			t.Errorf("unexpected path %q", r.URL.Path)
		}
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprint(w, `{
			"full_name": "o/r",
			"description": "test repo",
			"visibility": "public",
			"default_branch": "main",
			"has_issues": true,
			"has_discussions": false,
			"has_projects": false,
			"has_wiki": false,
			"allow_squash_merge": true,
			"allow_merge_commit": false,
			"allow_rebase_merge": false,
			"delete_branch_on_merge": true,
			"allow_auto_merge": false,
			"security_and_analysis": {
				"secret_scanning": {"status": "enabled"},
				"secret_scanning_push_protection": {"status": "enabled"}
			}
		}`)
	}))
	repo, err := c.GetRepository(context.Background(), "o", "r")
	if err != nil {
		t.Fatalf("GetRepository: %v", err)
	}
	if repo.FullName != "o/r" || repo.DefaultBranch != "main" {
		t.Errorf("unexpected repository: %+v", repo)
	}
	if !repo.AllowSquashMerge || repo.AllowMergeCommit || !repo.DeleteBranchOnMerge {
		t.Errorf("unexpected merge settings: %+v", repo)
	}
	if repo.SecurityAndAnalysis.SecretScanning == nil || repo.SecurityAndAnalysis.SecretScanning.Status != "enabled" {
		t.Errorf("unexpected security_and_analysis: %+v", repo.SecurityAndAnalysis)
	}
}

func TestListRulesetsPagination(t *testing.T) {
	c, _ := newTestClient(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Query().Get("page") {
		case "1":
			w.Header().Set("Link", fmt.Sprintf(`<%s/repos/o/r/rulesets?per_page=100&page=2>; rel="next"`, "http://"+r.Host))
			fmt.Fprint(w, `[{"id": 1, "name": "main", "target": "branch", "source_type": "Repository", "source": "o/r", "enforcement": "active"}]`)
		case "2":
			fmt.Fprint(w, `[{"id": 2, "name": "release tags", "target": "tag", "source_type": "Repository", "source": "o/r", "enforcement": "active"}]`)
		default:
			t.Errorf("unexpected page %q", r.URL.Query().Get("page"))
		}
	}))
	rulesets, err := c.ListRulesets(context.Background(), "o", "r")
	if err != nil {
		t.Fatalf("ListRulesets: %v", err)
	}
	if len(rulesets) != 2 || rulesets[0].Name != "main" || rulesets[1].Target != "tag" {
		t.Fatalf("unexpected rulesets: %+v", rulesets)
	}
}

func TestListTopicsAndLabels(t *testing.T) {
	c, _ := newTestClient(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/repos/o/r/topics":
			fmt.Fprint(w, `{"names": ["governance", "cli"]}`)
		case "/repos/o/r/labels":
			fmt.Fprint(w, `[{"name": "bug", "color": "d73a4a", "description": "Something isn't working", "default": true}]`)
		default:
			t.Errorf("unexpected path %q", r.URL.Path)
		}
	}))
	topics, err := c.ListTopics(context.Background(), "o", "r")
	if err != nil {
		t.Fatalf("ListTopics: %v", err)
	}
	if len(topics.Names) != 2 || topics.Names[0] != "governance" {
		t.Errorf("unexpected topics: %+v", topics)
	}
	labels, err := c.ListLabels(context.Background(), "o", "r")
	if err != nil {
		t.Fatalf("ListLabels: %v", err)
	}
	if len(labels) != 1 || labels[0].Name != "bug" || !labels[0].Default {
		t.Errorf("unexpected labels: %+v", labels)
	}
}

func securityMux(t *testing.T, vulnAlertsStatus, fixesStatus, setupStatus, pvrStatus int) http.Handler {
	t.Helper()
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/repos/o/r":
			fmt.Fprint(w, `{"full_name": "o/r", "security_and_analysis": {"secret_scanning": {"status": "enabled"}}}`)
		case "/repos/o/r/vulnerability-alerts":
			w.WriteHeader(vulnAlertsStatus)
		case "/repos/o/r/automated-security-fixes":
			if fixesStatus != 200 {
				w.WriteHeader(fixesStatus)
				fmt.Fprint(w, `{"message": "Not Found"}`)
				return
			}
			fmt.Fprint(w, `{"enabled": true, "paused": false}`)
		case "/repos/o/r/code-scanning/default-setup":
			if setupStatus != 200 {
				w.WriteHeader(setupStatus)
				fmt.Fprint(w, `{"message": "Not Found"}`)
				return
			}
			fmt.Fprint(w, `{"state": "configured", "languages": ["go"]}`)
		case "/repos/o/r/private-vulnerability-reporting":
			switch r.Method {
			case http.MethodGet:
				if pvrStatus != 200 {
					w.WriteHeader(pvrStatus)
					fmt.Fprint(w, `{"message": "Not Found"}`)
					return
				}
				fmt.Fprint(w, `{"enabled": true}`)
			case http.MethodPut, http.MethodDelete:
				w.WriteHeader(204)
			default:
				w.WriteHeader(405)
			}
		default:
			t.Errorf("unexpected path %q", r.URL.Path)
		}
	})
}

func TestGetSecuritySettings(t *testing.T) {
	c, _ := newTestClient(t, securityMux(t, 204, 200, 200, 200))
	s, err := c.GetSecuritySettings(context.Background(), "o", "r")
	if err != nil {
		t.Fatalf("GetSecuritySettings: %v", err)
	}
	if s.SecretScanning != "enabled" {
		t.Errorf("SecretScanning = %q", s.SecretScanning)
	}
	if !s.DependabotAlertsEnabled {
		t.Error("DependabotAlertsEnabled = false, want true")
	}
	if !s.AutomatedSecurityFixesEnabled {
		t.Error("AutomatedSecurityFixesEnabled = false, want true")
	}
	if s.CodeScanningDefaultSetup != "configured" {
		t.Errorf("CodeScanningDefaultSetup = %q", s.CodeScanningDefaultSetup)
	}
	if !s.PrivateVulnerabilityReporting {
		t.Error("PrivateVulnerabilityReporting = false, want true")
	}
}

func TestSetPrivateVulnerabilityReporting(t *testing.T) {
	var methods []string
	c, _ := newTestClient(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		methods = append(methods, r.Method)
		w.WriteHeader(204)
	}))
	if err := c.SetPrivateVulnerabilityReporting(context.Background(), "o", "r", true); err != nil {
		t.Fatalf("enable: %v", err)
	}
	if err := c.SetPrivateVulnerabilityReporting(context.Background(), "o", "r", false); err != nil {
		t.Fatalf("disable: %v", err)
	}
	if len(methods) != 2 || methods[0] != http.MethodPut || methods[1] != http.MethodDelete {
		t.Errorf("methods = %v, want [PUT DELETE]", methods)
	}
}

func TestGetSecuritySettingsDisabled(t *testing.T) {
	c, _ := newTestClient(t, securityMux(t, 404, 404, 404, 404))
	s, err := c.GetSecuritySettings(context.Background(), "o", "r")
	if err != nil {
		t.Fatalf("GetSecuritySettings: %v", err)
	}
	if s.DependabotAlertsEnabled || s.AutomatedSecurityFixesEnabled || s.CodeScanningDefaultSetup != "" || s.PrivateVulnerabilityReporting {
		t.Errorf("expected all probes disabled, got %+v", s)
	}
}

func TestNotFound(t *testing.T) {
	c, _ := newTestClient(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(404)
		fmt.Fprint(w, `{"message": "Not Found", "documentation_url": "https://docs.github.com/rest"}`)
	}))
	_, err := c.GetRepository(context.Background(), "o", "missing")
	if !IsNotFound(err) {
		t.Fatalf("expected IsNotFound, got %v", err)
	}
}

func TestConfigureCodeScanningNotFoundIsActionable(t *testing.T) {
	var method, path string
	c, _ := newTestClient(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		method, path = r.Method, r.URL.Path
		w.WriteHeader(404)
		fmt.Fprint(w, `{"message": "Not Found"}`)
	}))
	err := c.ConfigureCodeScanningDefaultSetup(context.Background(), "o", "r", []string{"go"})
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	if method != http.MethodPatch || path != "/repos/o/r/code-scanning/default-setup" {
		t.Errorf("want PATCH .../code-scanning/default-setup, got %s %s", method, path)
	}
	for _, want := range []string{"404", "token", "re-run bootstrap"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("diagnostic should mention %q: %v", want, err)
		}
	}
}

func TestRateLimited(t *testing.T) {
	c, _ := newTestClient(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("X-RateLimit-Limit", "5000")
		w.Header().Set("X-RateLimit-Remaining", "0")
		w.Header().Set("X-RateLimit-Reset", "1893456000")
		w.WriteHeader(403)
		fmt.Fprint(w, `{"message": "API rate limit exceeded"}`)
	}))
	_, err := c.GetRepository(context.Background(), "o", "r")
	if !IsRateLimited(err) {
		t.Fatalf("expected IsRateLimited, got %v", err)
	}
	if !strings.Contains(err.Error(), "rate limit") {
		t.Errorf("error should mention rate limit: %v", err)
	}
}

func TestRetryOnServerError(t *testing.T) {
	var calls atomic.Int32
	c, _ := newTestClient(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if calls.Add(1) == 1 {
			w.WriteHeader(503)
			return
		}
		fmt.Fprint(w, `{"full_name": "o/r"}`)
	}))
	repo, err := c.GetRepository(context.Background(), "o", "r")
	if err != nil {
		t.Fatalf("GetRepository: %v", err)
	}
	if repo.FullName != "o/r" {
		t.Errorf("unexpected repo: %+v", repo)
	}
	if calls.Load() != 2 {
		t.Errorf("calls = %d, want 2 (one retry)", calls.Load())
	}
}

func TestNoRetryOnClientError(t *testing.T) {
	var calls atomic.Int32
	c, _ := newTestClient(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		w.WriteHeader(404)
		fmt.Fprint(w, `{"message": "Not Found"}`)
	}))
	_, err := c.GetRepository(context.Background(), "o", "r")
	if !IsNotFound(err) {
		t.Fatalf("expected 404, got %v", err)
	}
	if calls.Load() != 1 {
		t.Errorf("calls = %d, want 1 (no retry on 4xx)", calls.Load())
	}
}

func TestNoTokenLeakInErrors(t *testing.T) {
	c, _ := newTestClient(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(400)
		// Server echoes the credential back in the error body; the client
		// must redact it.
		fmt.Fprintf(w, `{"message": "bad request involving %s"}`, testToken)
	}))
	_, err := c.GetRepository(context.Background(), "o", "r")
	if err == nil {
		t.Fatal("expected error")
	}
	if strings.Contains(err.Error(), testToken) {
		t.Fatalf("error leaks token: %v", err)
	}
	if !strings.Contains(err.Error(), "[REDACTED]") {
		t.Errorf("expected redaction marker: %v", err)
	}
}

func TestRequestHeaders(t *testing.T) {
	var seen http.Header
	c, _ := newTestClient(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		seen = r.Header.Clone()
		fmt.Fprint(w, `{"full_name": "o/r"}`)
	}))
	if _, err := c.GetRepository(context.Background(), "o", "r"); err != nil {
		t.Fatalf("GetRepository: %v", err)
	}
	if got := seen.Get("Accept"); got != acceptMediaType {
		t.Errorf("Accept = %q", got)
	}
	if got := seen.Get("X-GitHub-Api-Version"); got != APIVersion {
		t.Errorf("X-GitHub-Api-Version = %q", got)
	}
	if got := seen.Get("Authorization"); got != "Bearer "+testToken {
		t.Errorf("Authorization = %q", got)
	}
	if !strings.HasPrefix(seen.Get("User-Agent"), "repoforge/") {
		t.Errorf("User-Agent = %q", seen.Get("User-Agent"))
	}
}
