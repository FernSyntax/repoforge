// Copyright the repoforge contributors.
//
// SPDX-License-Identifier: Apache-2.0

package github

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"testing"
)

// TestNormalizeLabelColor pins the canonical form the API uses for
// both reads and writes: a "#" and mixed case must compare equal to
// the normalized form, or every label would drift on every run.
func TestNormalizeLabelColor(t *testing.T) {
	cases := []struct{ in, want string }{
		{"d73a4a", "d73a4a"},
		{"D73A4A", "d73a4a"},
		{"#D73A4A", "d73a4a"},
		{"  #d73a4a  ", "d73a4a"},
		{"", ""},
	}
	for _, c := range cases {
		if got := NormalizeLabelColor(c.in); got != c.want {
			t.Errorf("NormalizeLabelColor(%q) = %q, want %q", c.in, got, c.want)
		}
	}
}

// TestIssueExistsDistinguishesStates proves the linkage validator's
// contract: a resolving reference is silent, a missing one is named,
// and a broken read is never mistaken for a missing issue.
func TestIssueExistsDistinguishesStates(t *testing.T) {
	cases := []struct {
		name       string
		status     int
		wantErr    bool
		wantSubstr string
	}{
		{"a resolving issue is silent", 200, false, ""},
		{"a missing issue is named", 404, true, "does not exist"},
		{"a broken read is not a missing issue", 500, true, "reading issue"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			client, _ := newTestClient(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.URL.Path != "/repos/o/r/issues/7" {
					t.Errorf("unexpected path %q", r.URL.Path)
				}
				if c.status == 200 {
					fmt.Fprint(w, `{"number": 7, "state": "open"}`)
					return
				}
				w.WriteHeader(c.status)
				fmt.Fprint(w, `{"message": "Not Found"}`)
			}))
			err := client.IssueExists(context.Background(), "o", "r", 7)
			if c.wantErr && err == nil {
				t.Fatalf("expected an error mentioning %q", c.wantSubstr)
			}
			if !c.wantErr && err != nil {
				t.Fatalf("expected silence, got %v", err)
			}
			if c.wantErr && !strings.Contains(err.Error(), c.wantSubstr) {
				t.Errorf("err = %v, want it to mention %q", err, c.wantSubstr)
			}
		})
	}
}

// TestGetRulesetReturnsTheDetail proves the rules come back with
// their raw parameters intact: the drift comparison is only as good
// as the parameters it can read.
func TestGetRulesetReturnsTheDetail(t *testing.T) {
	c, _ := newTestClient(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/repos/o/r/rulesets/101" {
			t.Errorf("unexpected path %q", r.URL.Path)
		}
		fmt.Fprint(w, `{"id": 101, "name": "repoforge:main", "enforcement": "active",
			"rules": [{"type": "pull_request", "parameters": {"required_approving_review_count": 1}}]}`)
	}))
	d, err := c.GetRuleset(context.Background(), "o", "r", 101)
	if err != nil {
		t.Fatalf("GetRuleset: %v", err)
	}
	if d.ID != 101 || d.Name != "repoforge:main" {
		t.Errorf("detail = %+v", d)
	}
	params := d.PullRequestRuleParams()
	if params == nil || params.RequiredApprovingReviewCount != 1 {
		t.Errorf("pull_request parameters = %+v, want the decoded count", params)
	}
	if _, err := c.GetRuleset(context.Background(), "o", "a/b", 101); err == nil {
		t.Error("an unsafe slug must be refused")
	}
}

// TestRuleParamsDecodingEdges proves the parameter decoders answer
// nil for every shape that is not a decodable parameter object: an
// absent rule, absent parameters, and a value of the wrong type must
// all read as "no parameters" rather than a half-decoded rule.
func TestRuleParamsDecodingEdges(t *testing.T) {
	cases := []struct{ name, body string }{
		{"no rules", `{}`},
		{"absent parameters", `{"rules":[{"type":"pull_request"}]}`},
		{"wrong type parameters", `{"rules":[{"type":"pull_request","parameters":"nope"}]}`},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			var d RulesetDetail
			if err := json.Unmarshal([]byte(c.body), &d); err != nil {
				t.Fatalf("fixture %q: %v", c.body, err)
			}
			if got := d.PullRequestRuleParams(); got != nil {
				t.Errorf("PullRequestRuleParams = %+v, want nil", got)
			}
			if got := d.StatusCheckRuleParams(); got != nil {
				t.Errorf("StatusCheckRuleParams = %+v, want nil", got)
			}
		})
	}
}

// TestListLanguagesReturnsTheBreakdown proves the language map is
// decoded as the API reports it: bootstrap's default-setup languages
// come from this breakdown, so a decode that silently dropped entries
// would configure the wrong languages.
func TestListLanguagesReturnsTheBreakdown(t *testing.T) {
	c, _ := newTestClient(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/repos/o/r/languages" {
			t.Errorf("unexpected path %q", r.URL.Path)
		}
		fmt.Fprint(w, `{"Go": 12345, "Shell": 56}`)
	}))
	langs, err := c.ListLanguages(context.Background(), "o", "r")
	if err != nil {
		t.Fatalf("ListLanguages: %v", err)
	}
	if langs["Go"] != 12345 || langs["Shell"] != 56 || len(langs) != 2 {
		t.Errorf("languages = %v", langs)
	}
}

// securityProbeMux serves every security probe, failing exactly the
// one at failPath with status. The probes are independent, so the
// table can prove each failure is attributed to its own probe rather
// than to the first one.
func securityProbeMux(t *testing.T, failPath string, status int) http.Handler {
	t.Helper()
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/repos/o/r":
			fmt.Fprint(w, `{"full_name": "o/r"}`)
		case "/repos/o/r/vulnerability-alerts":
			if r.URL.Path == failPath {
				w.WriteHeader(status)
				fmt.Fprint(w, `{"message": "boom"}`)
				return
			}
			w.WriteHeader(204) // 2xx means enabled
		case "/repos/o/r/automated-security-fixes",
			"/repos/o/r/private-vulnerability-reporting",
			"/repos/o/r/code-scanning/default-setup":
			if r.URL.Path == failPath {
				w.WriteHeader(status)
				fmt.Fprint(w, `{"message": "boom"}`)
				return
			}
			fmt.Fprint(w, `{"enabled": true, "state": "configured"}`)
		default:
			t.Errorf("unexpected path %q", r.URL.Path)
		}
	})
}

// TestGetSecuritySettingsAttributesProbeFailures proves each probe's
// failure is reported against that probe: a generic "security probe
// failed" sends the operator hunting through four endpoints.
func TestGetSecuritySettingsAttributesProbeFailures(t *testing.T) {
	cases := []struct{ path, want string }{
		{"/repos/o/r/vulnerability-alerts", "probing vulnerability alerts"},
		{"/repos/o/r/automated-security-fixes", "probing automated security fixes"},
		{"/repos/o/r/code-scanning/default-setup", "probing code scanning default setup"},
		{"/repos/o/r/private-vulnerability-reporting", "probing private vulnerability reporting"},
	}
	for _, c := range cases {
		t.Run(c.path, func(t *testing.T) {
			client, _ := newTestClient(t, securityProbeMux(t, c.path, 500))
			_, err := client.GetSecuritySettings(context.Background(), "o", "r")
			if err == nil {
				t.Fatalf("a 500 probe must fail")
			}
			if !strings.Contains(err.Error(), c.want) {
				t.Errorf("err = %v, want it to name %q", err, c.want)
			}
		})
	}
}

// TestGetSecuritySettingsSurfacesRepositoryError proves the base read
// failing aborts the whole posture read: a default-empty posture from
// an unreadable repository would read as "every feature off" instead
// of "unknown".
func TestGetSecuritySettingsSurfacesRepositoryError(t *testing.T) {
	c, _ := newTestClient(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(500)
	}))
	if _, err := c.GetSecuritySettings(context.Background(), "o", "r"); err == nil {
		t.Error("an unreadable repository must fail the posture read")
	}
	if _, err := c.GetSecuritySettings(context.Background(), "o", "a/b"); err == nil {
		t.Error("an unsafe slug must be refused before any request")
	}
}

// TestListCallsRefuseUnsafeSlugs proves every list entry point
// validates before requesting: an unchecked slug would turn a name
// into a different endpoint.
func TestListCallsRefuseUnsafeSlugs(t *testing.T) {
	c, _ := newTestClient(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		t.Errorf("no request must be issued for an unsafe slug, got %s", r.URL.Path)
	}))
	if _, err := c.ListRulesets(context.Background(), "o", "a/b"); err == nil {
		t.Error("ListRulesets must refuse an unsafe slug")
	}
	if _, err := c.ListTopics(context.Background(), "o", "a/b"); err == nil {
		t.Error("ListTopics must refuse an unsafe slug")
	}
	if _, err := c.ListLabels(context.Background(), "o", "a/b"); err == nil {
		t.Error("ListLabels must refuse an unsafe slug")
	}
}
