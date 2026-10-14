// Copyright the repoforge contributors.
//
// SPDX-License-Identifier: Apache-2.0

package doctor

import (
	"encoding/json"
	"strings"
	"testing"
)

// TestSummaryCountsEverySeverity pins the tally the CLI prints: an
// UNSUPPORTED finding is a capability gap, not a pass and not a
// failure, so it must be counted in its own bucket. Collapsing it into
// either would make the printed summary lie about the audit.
func TestSummaryCountsEverySeverity(t *testing.T) {
	for _, c := range []struct {
		name                                 string
		findings                             []Finding
		wantPass, wantWarn, wantFail, wantUn int
	}{
		{"empty", nil, 0, 0, 0, 0},
		{"passes only", []Finding{{Severity: Pass}, {Severity: Pass}}, 2, 0, 0, 0},
		{"warn only", []Finding{{Severity: Warn}}, 0, 1, 0, 0},
		{"fail only", []Finding{{Severity: Fail}}, 0, 0, 1, 0},
		{"unsupported only", []Finding{{Severity: Unsupported}}, 0, 0, 0, 1},
		{
			"mixed",
			[]Finding{{Severity: Pass}, {Severity: Warn}, {Severity: Fail}, {Severity: Unsupported}, {Severity: Fail}},
			1, 1, 2, 1,
		},
	} {
		t.Run(c.name, func(t *testing.T) {
			rep := Report{Findings: c.findings}
			pass, warn, fail, uns := rep.Summary()
			if pass != c.wantPass || warn != c.wantWarn || fail != c.wantFail || uns != c.wantUn {
				t.Errorf("Summary() = %d/%d/%d/%d, want %d/%d/%d/%d",
					pass, warn, fail, uns, c.wantPass, c.wantWarn, c.wantFail, c.wantUn)
			}
		})
	}
}

// TestFailedOnlyCountsFail pins spec.md §11.4: WARN and UNSUPPORTED are
// never drift. Treating a plan gap (secret scanning on a private
// repository without GitHub Code Security) as drift would block merges
// on something the repository owner cannot fix.
func TestFailedOnlyCountsFail(t *testing.T) {
	for _, c := range []struct {
		name     string
		findings []Finding
		want     bool
	}{
		{"empty", nil, false},
		{"pass only", []Finding{{Severity: Pass}}, false},
		{"warn only", []Finding{{Severity: Warn}}, false},
		{"unsupported only", []Finding{{Severity: Unsupported}}, false},
		{"warn and unsupported", []Finding{{Severity: Warn}, {Severity: Unsupported}, {Severity: Pass}}, false},
		{"one fail", []Finding{{Severity: Pass}, {Severity: Fail}}, true},
		{"fail first", []Finding{{Severity: Fail}, {Severity: Pass}}, true},
	} {
		t.Run(c.name, func(t *testing.T) {
			if got := (Report{Findings: c.findings}).Failed(); got != c.want {
				t.Errorf("Failed() = %t, want %t", got, c.want)
			}
		})
	}
}

// TestExitCode pins the spec.md §11.4 mapping, including the precedence
// of an incomplete audit (2) over detected drift (1).
func TestExitCode(t *testing.T) {
	for _, c := range []struct {
		name     string
		rep      Report
		err      error
		wantCode int
	}{
		{"no findings", Report{}, nil, 0},
		{"clean findings", Report{Findings: []Finding{{Severity: Pass}}}, nil, 0},
		{"warn only", Report{Findings: []Finding{{Severity: Warn}}}, nil, 0},
		{"unsupported only", Report{Findings: []Finding{{Severity: Unsupported}}}, nil, 0},
		{"drift", Report{Findings: []Finding{{Severity: Fail}}}, nil, 1},
		{"incomplete audit with drift", Report{Findings: []Finding{{Severity: Fail}}}, errAuditIncomplete{}, 2},
		{"incomplete audit without drift", Report{}, errAuditIncomplete{}, 2},
	} {
		t.Run(c.name, func(t *testing.T) {
			if got := ExitCode(c.rep, c.err); got != c.wantCode {
				t.Errorf("ExitCode() = %d, want %d", got, c.wantCode)
			}
		})
	}
}

type errAuditIncomplete struct{}

func (errAuditIncomplete) Error() string { return "audit could not complete" }

// TestAddFormatsMessage keeps every finding message a single
// printf-style call site: the JSON report is the machine interface and
// a message assembled across several calls would not round-trip.
func TestAddFormatsMessage(t *testing.T) {
	var rep Report
	rep.add(Pass, "local/git", "git repository found (%s)", "/tmp/x")
	rep.add(Fail, "remote/merge", "squash merge %s, policy wants %s", onOff(true), onOff(false))
	if len(rep.Findings) != 2 {
		t.Fatalf("findings = %d, want 2", len(rep.Findings))
	}
	want := []Finding{
		{Check: "local/git", Severity: Pass, Message: "git repository found (/tmp/x)"},
		{Check: "remote/merge", Severity: Fail, Message: "squash merge enabled, policy wants disabled"},
	}
	for i, w := range want {
		if rep.Findings[i] != w {
			t.Errorf("finding %d = %+v, want %+v", i, rep.Findings[i], w)
		}
	}
}

// TestReportJSONShapeRoundTrips is the consumer contract for
// `doctor --json`: the field names and the severity vocabulary are the
// API. The summary must also agree with what was marshalled, so a
// finding cannot be counted but lost in serialization.
func TestReportJSONShapeRoundTrips(t *testing.T) {
	rep := Report{Repository: "o/r", Findings: []Finding{
		{Check: "local/git", Severity: Pass, Message: "git repository found (/tmp/x)"},
		{Check: "remote/topics", Severity: Warn, Message: "cannot read topics: boom"},
		{Check: "remote/security", Severity: Unsupported, Message: "plan gap"},
	}}
	data, err := json.Marshal(rep)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	var decoded Report
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatalf("report JSON shape is not stable: %v", err)
	}
	if decoded.Repository != "o/r" {
		t.Errorf("repository = %q, want %q", decoded.Repository, "o/r")
	}
	gotPass, gotWarn, gotFail, gotUn := decoded.Summary()
	wantPass, wantWarn, wantFail, wantUn := rep.Summary()
	if gotPass != wantPass || gotWarn != wantWarn || gotFail != wantFail || gotUn != wantUn {
		t.Errorf("summary after round-trip = %d/%d/%d/%d, want %d/%d/%d/%d",
			gotPass, gotWarn, gotFail, gotUn, wantPass, wantWarn, wantFail, wantUn)
	}
	valid := map[Severity]bool{Pass: true, Warn: true, Fail: true, Unsupported: true}
	for _, f := range decoded.Findings {
		if !valid[f.Severity] {
			t.Errorf("severity %q is outside the documented vocabulary", f.Severity)
		}
		if f.Check == "" || f.Message == "" {
			t.Errorf("finding missing fields: %+v", f)
		}
	}
}

// TestResolveSlug pins slug parsing: --repo wins over origin, a .git
// suffix is tolerated, and anything that is not exactly owner/name is
// an invocation error rather than a malformed API path.
func TestResolveSlug(t *testing.T) {
	for _, c := range []struct {
		name      string
		opts      Options
		wantOwner string
		wantRepo  string
		wantErr   string
	}{
		{"explicit slug", Options{Repo: "owner/name"}, "owner", "name", ""},
		{"explicit slug with .git", Options{Repo: "owner/name.git"}, "owner", "name", ""},
		{"dotted names", Options{Repo: "some.owner/some.name"}, "some.owner", "some.name", ""},
		{"no slash", Options{Repo: "owner"}, "", "", `invalid repository slug "owner"`},
		{"three parts", Options{Repo: "a/b/c"}, "", "", `invalid repository slug "a/b/c"`},
		{"empty owner", Options{Repo: "/name"}, "", "", `invalid repository slug "/name"`},
		{"empty name", Options{Repo: "owner/"}, "", "", `invalid repository slug "owner/"`},
		{"empty slug", Options{Repo: ""}, "", "", "cannot determine repository slug"},
	} {
		t.Run(c.name, func(t *testing.T) {
			// The empty-slug case falls through to SlugFromOrigin, which
			// needs a git checkout with an origin; a bare temp dir is a
			// deterministic stand-in for "no origin remote".
			if c.opts.Repo == "" {
				c.opts.StartDir = t.TempDir()
			}
			owner, name, err := resolveSlug(c.opts)
			if c.wantErr == "" {
				if err != nil {
					t.Fatalf("unexpected error: %v", err)
				}
				if owner != c.wantOwner || name != c.wantRepo {
					t.Errorf("resolveSlug() = %q/%q, want %q/%q", owner, name, c.wantOwner, c.wantRepo)
				}
				return
			}
			if err == nil {
				t.Fatalf("expected an error containing %q, got %q/%q", c.wantErr, owner, name)
			}
			if !strings.Contains(err.Error(), c.wantErr) {
				t.Errorf("error = %q, want it to contain %q", err, c.wantErr)
			}
		})
	}
}

// TestLoadProfile covers the embedded governance profiles doctor reads
// to derive the desired rulesets. Every accepted profile name must
// resolve to a loadable profile whose self-declared name matches, and
// an unknown one must fail loudly rather than silently auditing
// against nothing.
func TestLoadProfile(t *testing.T) {
	for _, profile := range []string{"solo", "team", "strict"} {
		t.Run(profile, func(t *testing.T) {
			gov, err := LoadProfile(policyWithProfile(t, profile))
			if err != nil {
				t.Fatalf("LoadProfile(%q): %v", profile, err)
			}
			if gov.Profile != profile {
				t.Errorf("profile = %q, want %q; a mismatch means the wrong embedded file was read", gov.Profile, profile)
			}
		})
	}

	t.Run("nil policy defaults to solo", func(t *testing.T) {
		// GovernanceProfileName() defaults to solo, so a repository
		// without an explicit profile must still audit against the solo
		// profile rather than none.
		gov, err := LoadProfile(nil)
		if err != nil {
			t.Fatalf("LoadProfile(nil): %v", err)
		}
		if gov.Profile != "solo" {
			t.Errorf("profile = %q, want solo", gov.Profile)
		}
	})

	t.Run("unknown profile", func(t *testing.T) {
		gov, err := LoadProfile(policyWithProfile(t, "does-not-exist"))
		if err == nil {
			t.Fatalf("expected an error, got profile %+v", gov)
		}
		if !strings.Contains(err.Error(), "embedded governance profile") {
			t.Errorf("error = %q, want it to name the embedded governance profile", err)
		}
		if !strings.Contains(err.Error(), "does-not-exist") {
			t.Errorf("error = %q, want it to name the requested profile", err)
		}
	})
}
