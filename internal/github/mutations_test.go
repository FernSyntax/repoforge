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

// TestMutateRejectsUnencodableBody proves an unencodable body fails
// before any request: a half-marshalled mutation is worse than no
// mutation, and the refusal must not look like a transport failure.
func TestMutateRejectsUnencodableBody(t *testing.T) {
	c, rt := newRecording()
	err := c.mutate(context.Background(), http.MethodPost, "/repos/o/r",
		map[string]any{"callback": func() {}}, nil)
	if err == nil {
		t.Fatal("an unencodable body must be refused")
	}
	if !strings.Contains(err.Error(), "encoding request body") {
		t.Errorf("err = %v, want the encoding failure", err)
	}
	if rt.count() != 0 {
		t.Errorf("a refused mutation still issued %d request(s)", rt.count())
	}
}

// TestMutateRejectsUnparsableBaseURL proves a malformed endpoint fails
// as a request-building error rather than reaching the transport,
// where it would be reported as an unknown mutation outcome.
func TestMutateRejectsUnparsableBaseURL(t *testing.T) {
	c := New(WithBaseURL("http://%zz-invalid"))
	err := c.UpdateRepository(context.Background(), "o", "r", RepoSettingsPatch{})
	if err == nil {
		t.Fatal("an unparsable base URL must fail")
	}
	if !strings.Contains(err.Error(), "building request") {
		t.Errorf("err = %v, want a request-building failure", err)
	}
}

// TestMutationsRefuseUnsafeSlugs proves every mutation validates its
// slug before requesting: an unsafe name must never become a request
// to an unintended endpoint, and the refusal must be an error — not a
// panic, and not a silent success.
func TestMutationsRefuseUnsafeSlugs(t *testing.T) {
	ctx := context.Background()
	cases := []struct {
		name string
		run  func(*Client) error
	}{
		{"ReplaceTopics", func(c *Client) error {
			return c.ReplaceTopics(ctx, "o", "a/b", []string{"x"})
		}},
		{"RenameBranch repo", func(c *Client) error {
			return c.RenameBranch(ctx, "o", "a/b", "main", "trunk")
		}},
		{"RenameBranch branch", func(c *Client) error {
			return c.RenameBranch(ctx, "o", "r", "", "trunk")
		}},
		{"RenameBranch new name", func(c *Client) error {
			return c.RenameBranch(ctx, "o", "r", "main", "bad\nname")
		}},
		{"CreateRuleset", func(c *Client) error {
			_, err := c.CreateRuleset(ctx, "o", "a/b", RulesetPayload{})
			return err
		}},
		{"UpdateRuleset", func(c *Client) error {
			return c.UpdateRuleset(ctx, "o", "a/b", 1, RulesetPayload{})
		}},
		{"CreateLabel", func(c *Client) error {
			return c.CreateLabel(ctx, "o", "a/b", LabelPayload{})
		}},
		{"UpdateLabel", func(c *Client) error {
			return c.UpdateLabel(ctx, "o", "r", "a/b", LabelPayload{})
		}},
		{"SetVulnerabilityAlerts", func(c *Client) error {
			return c.SetVulnerabilityAlerts(ctx, "o", "a/b", true)
		}},
		{"SetAutomatedSecurityFixes", func(c *Client) error {
			return c.SetAutomatedSecurityFixes(ctx, "o", "a/b", true)
		}},
		{"SetPrivateVulnerabilityReporting", func(c *Client) error {
			return c.SetPrivateVulnerabilityReporting(ctx, "o", "a/b", true)
		}},
		{"SetSecretScanning", func(c *Client) error {
			return c.SetSecretScanning(ctx, "o", "a/b", true, true)
		}},
		{"ConfigureCodeScanningDefaultSetup", func(c *Client) error {
			return c.ConfigureCodeScanningDefaultSetup(ctx, "o", "a/b", []string{"go"})
		}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			c, rt := newRecording()
			err := tc.run(c)
			if err == nil {
				t.Fatal("an unsafe slug must be refused")
			}
			if !strings.Contains(err.Error(), "unsafe path component") {
				t.Errorf("err = %v, want the actionable refusal", err)
			}
			if rt.count() != 0 {
				t.Errorf("a refused mutation still issued %d request(s)", rt.count())
			}
		})
	}
}

// TestToggleMutationsUseTheDocumentedVerb proves the enable/disable
// toggles map to the documented verbs: PUT to enable, DELETE to
// disable — the verbs are not interchangeable on these endpoints.
func TestToggleMutationsUseTheDocumentedVerb(t *testing.T) {
	toggles := []struct {
		name, path string
		run        func(*Client, bool) error
	}{
		{"SetVulnerabilityAlerts", "/repos/o/r/vulnerability-alerts", func(c *Client, on bool) error {
			return c.SetVulnerabilityAlerts(context.Background(), "o", "r", on)
		}},
		{"SetAutomatedSecurityFixes", "/repos/o/r/automated-security-fixes", func(c *Client, on bool) error {
			return c.SetAutomatedSecurityFixes(context.Background(), "o", "r", on)
		}},
		{"SetPrivateVulnerabilityReporting", "/repos/o/r/private-vulnerability-reporting", func(c *Client, on bool) error {
			return c.SetPrivateVulnerabilityReporting(context.Background(), "o", "r", on)
		}},
	}
	for _, tg := range toggles {
		t.Run(tg.name+"/enable", func(t *testing.T) {
			c, rt := newRecording()
			if err := tg.run(c, true); err != nil {
				t.Fatal(err)
			}
			assertRequest(t, rt, http.MethodPut, tg.path)
		})
		t.Run(tg.name+"/disable", func(t *testing.T) {
			c, rt := newRecording()
			if err := tg.run(c, false); err != nil {
				t.Fatal(err)
			}
			assertRequest(t, rt, http.MethodDelete, tg.path)
		})
	}
}

// TestCreateRulesetReturnsTheCreatedDetail proves the created ruleset
// is decoded and returned: bootstrap uses the returned id to plan the
// next update, so a lost body would lose the identity of what was
// just created.
func TestCreateRulesetReturnsTheCreatedDetail(t *testing.T) {
	c, _ := newTestClient(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprint(w, `{"id": 101, "name": "repoforge:main", "enforcement": "active"}`)
	}))
	d, err := c.CreateRuleset(context.Background(), "o", "r", RulesetPayload{Name: "repoforge:main"})
	if err != nil {
		t.Fatalf("CreateRuleset: %v", err)
	}
	if d == nil || d.ID != 101 || d.Name != "repoforge:main" {
		t.Fatalf("detail = %+v, want the created ruleset", d)
	}
}

// TestConfigureCodeScanningOmitsEmptyLanguages proves an empty
// language list sends no languages key: the endpoint upserts, and an
// empty array would read as "no languages" rather than "unchanged".
func TestConfigureCodeScanningOmitsEmptyLanguages(t *testing.T) {
	c, rt := newRecording()
	if err := c.ConfigureCodeScanningDefaultSetup(context.Background(), "o", "r", nil); err != nil {
		t.Fatal(err)
	}
	var body map[string]any
	if err := json.Unmarshal([]byte(rt.last().body), &body); err != nil {
		t.Fatalf("body %q: %v", rt.last().body, err)
	}
	if body["state"] != "configured" {
		t.Errorf("state = %v, want configured", body["state"])
	}
	if _, ok := body["languages"]; ok {
		t.Errorf("body = %v, want no languages key for an empty list", body)
	}
}

// TestConfigureCodeScanningNon404SurfacesVerbatim proves the 404
// guidance is scoped to a 404: any other failure is reported as the
// API error it is, so a 500 is not dressed up as a permissions
// problem.
func TestConfigureCodeScanningNon404SurfacesVerbatim(t *testing.T) {
	c, _ := newTestClient(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(500)
	}))
	err := c.ConfigureCodeScanningDefaultSetup(context.Background(), "o", "r", []string{"go"})
	if err == nil {
		t.Fatal("a 500 must fail")
	}
	if strings.Contains(err.Error(), "404") {
		t.Errorf("err = %v, must not claim a 404", err)
	}
	if !strings.Contains(err.Error(), "status 500") {
		t.Errorf("err = %v, want the status reported verbatim", err)
	}
}
