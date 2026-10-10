package doctor

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/FernSyntax/repoforge/configs"
	"github.com/FernSyntax/repoforge/internal/config"
	ghub "github.com/FernSyntax/repoforge/internal/github"
)

const doctorTestPolicy = `standard:
  version: v1
repository:
  governance_profile: solo
  visibility: public
  default_branch: main
github:
  issues: true
  wiki: true
ownership:
  managed_paths:
    - LICENSE
`

func git(t *testing.T, dir string, args ...string) {
	t.Helper()
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	cmd.Env = append(os.Environ(),
		"GIT_AUTHOR_NAME=Test Author", "GIT_AUTHOR_EMAIL=test@example.invalid",
		"GIT_COMMITTER_NAME=Test Author", "GIT_COMMITTER_EMAIL=test@example.invalid",
		"LC_ALL=C",
	)
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("git %s: %v\n%s", strings.Join(args, " "), err, out)
	}
}

func initRepo(t *testing.T, policy string) string {
	t.Helper()
	dir := t.TempDir()
	git(t, dir, "init", "-b", "main")
	write(t, dir, "README.md", "# Fixture\n")
	write(t, dir, "LICENSE", "Apache-2.0\n")
	write(t, dir, "CHANGELOG.md", "# Changelog\n")
	write(t, dir, "repoforge.yaml", policy)
	git(t, dir, "add", "-A")
	git(t, dir, "commit", "-m", "chore: initial commit")
	return dir
}

func write(t *testing.T, dir, name, content string) {
	t.Helper()
	p := filepath.Join(dir, name)
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(p, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

const repoJSON = `{
	"full_name": "o/r", "visibility": "public", "default_branch": "main",
	"has_issues": true, "has_discussions": false, "has_projects": false, "has_wiki": true,
	"allow_squash_merge": true, "allow_merge_commit": false, "allow_rebase_merge": false,
	"delete_branch_on_merge": true, "allow_auto_merge": false,
	"security_and_analysis": {
		"secret_scanning": {"status": "enabled"},
		"secret_scanning_push_protection": {"status": "enabled"}
	}
}`

// desiredRulesets composes the wants exactly as doctor.Run does, so
// the fake can serve a ruleset state that conforms to them.
func desiredRulesets(t *testing.T, dir string) (main, tags *ghub.RulesetPayload) {
	t.Helper()
	pol, violations, err := config.LoadPolicy(filepath.Join(dir, "repoforge.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	if len(violations) > 0 {
		t.Fatalf("fixture policy invalid: %+v", violations)
	}
	main, tags, err = configs.DesiredRulesets(pol.GovernanceProfileName(), config.DiscoverCheckContexts(dir), pol.MergeMethods())
	if err != nil {
		t.Fatal(err)
	}
	return main, tags
}

// driftedMainDetail is a hand-written ruleset response that drifts
// from the solo policy: enforcement off, 2 required approvals, and a
// rule the policy does not manage.
const driftedMainDetail = `{
	"id": 1, "name": "repoforge:main", "target": "branch",
	"source_type": "Repository", "enforcement": "disabled",
	"conditions": {"ref_name": {"include": ["~DEFAULT_BRANCH"], "exclude": []}},
	"bypass_actors": [],
	"rules": [
		{"type": "pull_request", "parameters": {
			"allowed_merge_methods": ["squash"],
			"dismiss_stale_reviews_on_push": false,
			"require_code_owner_review": false,
			"require_last_push_approval": false,
			"required_approving_review_count": 2,
			"required_review_thread_resolution": true
		}},
		{"type": "required_linear_history"},
		{"type": "non_fast_forward"},
		{"type": "deletion"},
		{"type": "creation"}
	]
}`

// fakeGitHub records every request method (mutation assertion) and
// serves a repository whose owned rulesets conform to the desired
// state derived from dir's policy. mutate may override the responses
// ("rulesets" list, "main_detail") to model drift.
func fakeGitHub(t *testing.T, dir string, mutate func(*map[string]string)) (*ghub.Client, *[]string) {
	t.Helper()
	methods := &[]string{}
	overrides := map[string]string{}
	if mutate != nil {
		mutate(&overrides)
	}
	wantMain, wantTags := desiredRulesets(t, dir)
	mainDetail, err := json.Marshal(wantMain)
	if err != nil {
		t.Fatal(err)
	}
	tagsDetail, err := json.Marshal(wantTags)
	if err != nil {
		t.Fatal(err)
	}
	list, err := json.Marshal([]map[string]any{
		{"id": 1, "name": wantMain.Name, "target": wantMain.Target, "source_type": "Repository", "source": "o/r", "enforcement": wantMain.Enforcement},
		{"id": 2, "name": wantTags.Name, "target": wantTags.Target, "source_type": "Repository", "source": "o/r", "enforcement": wantTags.Enforcement},
	})
	if err != nil {
		t.Fatal(err)
	}
	mux := http.NewServeMux()
	handle := func(methods *[]string) http.HandlerFunc {
		return func(w http.ResponseWriter, r *http.Request) {
			*methods = append(*methods, r.Method+" "+r.URL.Path)
			mux.ServeHTTP(w, r)
		}
	}
	mux.HandleFunc("GET /repos/o/r", func(w http.ResponseWriter, r *http.Request) {
		body := repoJSON
		if v, ok := overrides["repo"]; ok {
			body = v
		}
		fmt.Fprint(w, body)
	})
	mux.HandleFunc("GET /repos/o/r/topics", func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprint(w, `{"names": []}`)
	})
	mux.HandleFunc("GET /repos/o/r/rulesets", func(w http.ResponseWriter, r *http.Request) {
		if v, ok := overrides["rulesets"]; ok {
			fmt.Fprint(w, v)
			return
		}
		w.Write(list)
	})
	mux.HandleFunc("GET /repos/o/r/rulesets/1", func(w http.ResponseWriter, r *http.Request) {
		if v, ok := overrides["main_detail"]; ok {
			fmt.Fprint(w, v)
			return
		}
		w.Write(mainDetail)
	})
	mux.HandleFunc("GET /repos/o/r/rulesets/2", func(w http.ResponseWriter, r *http.Request) {
		w.Write(tagsDetail)
	})
	mux.HandleFunc("GET /repos/o/r/labels", func(w http.ResponseWriter, r *http.Request) {
		if v, ok := overrides["labels"]; ok {
			fmt.Fprint(w, v)
			return
		}
		fmt.Fprint(w, canonicalLabelsJSON(t, nil))
	})
	mux.HandleFunc("GET /repos/o/r/vulnerability-alerts", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(204)
	})
	mux.HandleFunc("GET /repos/o/r/automated-security-fixes", func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprint(w, `{"enabled": true, "paused": false}`)
	})
	mux.HandleFunc("GET /repos/o/r/private-vulnerability-reporting", func(w http.ResponseWriter, r *http.Request) {
		if v, ok := overrides["private_vulnerability_reporting"]; ok {
			fmt.Fprint(w, v)
			return
		}
		fmt.Fprint(w, `{"enabled": true}`)
	})
	mux.HandleFunc("GET /repos/o/r/code-scanning/default-setup", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(404)
		fmt.Fprint(w, `{"message": "Not Found"}`)
	})
	srv := httptest.NewServer(handle(methods))
	t.Cleanup(srv.Close)
	client := ghub.New(ghub.WithBaseURL(srv.URL), ghub.WithToken("test"), ghub.WithRetryBackoff(time.Millisecond))
	return client, methods
}

func runDoctor(t *testing.T, dir string, client *ghub.Client) Report {
	t.Helper()
	rep, err := Run(context.Background(), Options{StartDir: dir, Repo: "o/r", Client: client})
	if err != nil {
		t.Fatalf("doctor.Run: %v", err)
	}
	return rep
}

func findingsWithSeverity(rep Report, sev Severity) []Finding {
	var out []Finding
	for _, f := range rep.Findings {
		if f.Severity == sev {
			out = append(out, f)
		}
	}
	return out
}

// canonicalLabelsJSON serves the canonical label set as a labels-list
// response; mutate may alter the list to model drift.
func canonicalLabelsJSON(t *testing.T, mutate func([]map[string]any)) string {
	t.Helper()
	want, err := configs.CanonicalLabels()
	if err != nil {
		t.Fatal(err)
	}
	list := make([]map[string]any, 0, len(want))
	for _, w := range want {
		list = append(list, map[string]any{
			"name": w.Name, "color": w.Color, "description": w.Description, "default": false,
		})
	}
	if mutate != nil {
		mutate(list)
	}
	out, err := json.Marshal(list)
	if err != nil {
		t.Fatal(err)
	}
	return string(out)
}

func TestDoctorConformingRepository(t *testing.T) {
	dir := initRepo(t, doctorTestPolicy)
	client, _ := fakeGitHub(t, dir, nil)
	rep := runDoctor(t, dir, client)

	if fails := findingsWithSeverity(rep, Fail); len(fails) > 0 {
		t.Fatalf("expected no FAIL findings, got: %+v", fails)
	}
	if rep.Failed() {
		t.Fatal("rep.Failed() = true, want false")
	}
	if got := ExitCode(rep, nil); got != 0 {
		t.Fatalf("exit code = %d, want 0", got)
	}
	foundMain, foundTags := false, false
	for _, f := range findingsWithSeverity(rep, Pass) {
		if f.Check == "remote/rulesets" && strings.Contains(f.Message, configs.MainRulesetName) && strings.Contains(f.Message, "matches the policy") {
			foundMain = true
		}
		if f.Check == "remote/rulesets" && strings.Contains(f.Message, configs.ReleaseTagsRulesetName) && strings.Contains(f.Message, "matches the policy") {
			foundTags = true
		}
	}
	if !foundMain || !foundTags {
		t.Errorf("expected per-ruleset PASS findings (main=%t tags=%t) in %+v", foundMain, foundTags, rep.Findings)
	}
}

func TestDoctorDetectsDrift(t *testing.T) {
	dir := initRepo(t, doctorTestPolicy)
	client, _ := fakeGitHub(t, dir, func(o *map[string]string) {
		(*o)["repo"] = strings.Replace(repoJSON, `"allow_merge_commit": false`, `"allow_merge_commit": true`, 1)
		(*o)["rulesets"] = `[]`
	})
	rep := runDoctor(t, dir, client)

	fails := findingsWithSeverity(rep, Fail)
	if len(fails) == 0 {
		t.Fatal("expected FAIL findings for drifted repository")
	}
	var mergeFail, rulesetFail bool
	for _, f := range fails {
		if strings.Contains(f.Message, "merge commit enabled") {
			mergeFail = true
		}
		if strings.Contains(f.Message, `ruleset "`+configs.MainRulesetName+`" is missing`) {
			rulesetFail = true
		}
	}
	if !mergeFail || !rulesetFail {
		t.Errorf("missing expected FAIL findings: merge=%v rulesets=%v in %+v", mergeFail, rulesetFail, fails)
	}
	if got := ExitCode(rep, nil); got != 1 {
		t.Fatalf("exit code = %d, want 1", got)
	}
}

func TestDoctorReportsRulesetDriftDetail(t *testing.T) {
	dir := initRepo(t, doctorTestPolicy)
	client, _ := fakeGitHub(t, dir, func(o *map[string]string) {
		(*o)["main_detail"] = driftedMainDetail
	})
	rep := runDoctor(t, dir, client)

	var rulesetFails []Finding
	for _, f := range findingsWithSeverity(rep, Fail) {
		if f.Check == "remote/rulesets" {
			rulesetFails = append(rulesetFails, f)
		}
	}
	snippets := []string{
		`enforcement is "disabled", policy wants "active"`,
		"required_approving_review_count is 2, policy wants 0",
		`rule "creation" is not part of the desired policy`,
		"repoforge bootstrap",
	}
	for _, want := range snippets {
		var found bool
		for _, f := range rulesetFails {
			if strings.Contains(f.Message, want) {
				found = true
			}
		}
		if !found {
			t.Errorf("no ruleset FAIL contains %q in %+v", want, rulesetFails)
		}
	}
	if got := ExitCode(rep, nil); got != 1 {
		t.Errorf("exit code = %d, want 1", got)
	}
}

func TestDoctorWarnsOnUnmanagedRuleset(t *testing.T) {
	dir := initRepo(t, doctorTestPolicy)
	client, _ := fakeGitHub(t, dir, func(o *map[string]string) {
		(*o)["rulesets"] = fmt.Sprintf(`[
			{"id": 1, "name": %q, "target": "branch", "source_type": "Repository", "source": "o/r", "enforcement": "active"},
			{"id": 2, "name": %q, "target": "tag", "source_type": "Repository", "source": "o/r", "enforcement": "active"},
			{"id": 3, "name": "custom-gate", "target": "branch", "source_type": "Repository", "source": "o/r", "enforcement": "active"},
			{"id": 4, "name": "org-policy", "target": "branch", "source_type": "Organization", "source": "org", "enforcement": "active"}
		]`, configs.MainRulesetName, configs.ReleaseTagsRulesetName)
	})
	rep := runDoctor(t, dir, client)

	if fails := findingsWithSeverity(rep, Fail); len(fails) > 0 {
		t.Fatalf("unmanaged rulesets are not drift; got FAILs: %+v", fails)
	}
	var unmanagedWarn, inheritedWarn bool
	for _, f := range findingsWithSeverity(rep, Warn) {
		if strings.Contains(f.Message, "custom-gate") {
			unmanagedWarn = true
		}
		if strings.Contains(f.Message, "org-policy") {
			inheritedWarn = true
		}
	}
	if !unmanagedWarn {
		t.Errorf("expected WARN for unmanaged ruleset, got %+v", rep.Findings)
	}
	if inheritedWarn {
		t.Error("inherited org ruleset must not be warned about (out of repository scope)")
	}
}

func TestDoctorUnsupportedIsNotFailure(t *testing.T) {
	// Policy visibility must match the repository to avoid a
	// visibility FAIL.
	dir := initRepo(t, strings.Replace(doctorTestPolicy, "visibility: public", "visibility: private", 1))
	client, _ := fakeGitHub(t, dir, func(o *map[string]string) {
		(*o)["repo"] = strings.Replace(repoJSON, `"visibility": "public"`, `"visibility": "private"`, 1)
		// secret scanning status disabled → plan gap on private repos
		(*o)["repo"] = strings.Replace((*o)["repo"], `"status": "enabled"`, `"status": "disabled"`, -1)
	})
	rep := runDoctor(t, dir, client)

	unsupported := findingsWithSeverity(rep, Unsupported)
	if len(unsupported) == 0 {
		t.Fatal("expected UNSUPPORTED findings for plan-limited security features")
	}
	if fails := findingsWithSeverity(rep, Fail); len(fails) > 0 {
		t.Fatalf("UNSUPPORTED must not be a failure, but got FAILs: %+v", fails)
	}
	if got := ExitCode(rep, nil); got != 0 {
		t.Fatalf("exit code = %d, want 0 (UNSUPPORTED is not a failure)", got)
	}
}

func TestDoctorJSONFieldsStable(t *testing.T) {
	dir := initRepo(t, doctorTestPolicy)
	client, _ := fakeGitHub(t, dir, nil)
	rep := runDoctor(t, dir, client)

	data, err := json.Marshal(rep)
	if err != nil {
		t.Fatal(err)
	}
	var decoded struct {
		Repository string `json:"repository"`
		Findings   []struct {
			Check    string `json:"check"`
			Severity string `json:"severity"`
			Message  string `json:"message"`
		} `json:"findings"`
	}
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatalf("report JSON unstable: %v", err)
	}
	if decoded.Repository != "o/r" {
		t.Errorf("repository = %q", decoded.Repository)
	}
	if len(decoded.Findings) == 0 {
		t.Fatal("no findings in JSON")
	}
	validSeverities := map[string]bool{"PASS": true, "WARN": true, "FAIL": true, "UNSUPPORTED": true}
	for _, f := range decoded.Findings {
		if !validSeverities[f.Severity] {
			t.Errorf("unknown severity %q", f.Severity)
		}
		if f.Check == "" || f.Message == "" {
			t.Errorf("finding missing fields: %+v", f)
		}
	}
}

func TestDoctorNeverMutates(t *testing.T) {
	dir := initRepo(t, doctorTestPolicy)
	client, methods := fakeGitHub(t, dir, nil)
	_ = runDoctor(t, dir, client)

	if len(*methods) == 0 {
		t.Fatal("expected API calls")
	}
	for _, m := range *methods {
		if !strings.HasPrefix(m, "GET ") {
			t.Fatalf("doctor issued a non-read-only request: %s (spec.md §11.2)", m)
		}
	}
}

func TestDoctorReportsLabelsMatch(t *testing.T) {
	dir := initRepo(t, doctorTestPolicy)
	client, _ := fakeGitHub(t, dir, nil)
	rep := runDoctor(t, dir, client)

	var found bool
	for _, f := range findingsWithSeverity(rep, Pass) {
		if f.Check == "remote/labels" && strings.Contains(f.Message, "canonical labels exist and match") {
			found = true
		}
	}
	if !found {
		t.Errorf("expected remote/labels PASS in %+v", rep.Findings)
	}
}

func TestDoctorReportsMissingLabel(t *testing.T) {
	dir := initRepo(t, doctorTestPolicy)
	client, _ := fakeGitHub(t, dir, func(o *map[string]string) {
		(*o)["labels"] = `[]`
	})
	rep := runDoctor(t, dir, client)

	var labelFails []Finding
	for _, f := range findingsWithSeverity(rep, Fail) {
		if f.Check == "remote/labels" {
			labelFails = append(labelFails, f)
		}
	}
	want, err := configs.CanonicalLabels()
	if err != nil {
		t.Fatal(err)
	}
	if len(labelFails) != len(want) {
		t.Errorf("label FAILs = %d, want %d (one per missing label)", len(labelFails), len(want))
	}
	var bugFail, remediation bool
	for _, f := range labelFails {
		if strings.Contains(f.Message, `"type:bug" is missing`) {
			bugFail = true
		}
		if strings.Contains(f.Message, "repoforge bootstrap") {
			remediation = true
		}
	}
	if !bugFail || !remediation {
		t.Errorf("missing expected label FAIL content in %+v", labelFails)
	}
	if got := ExitCode(rep, nil); got != 1 {
		t.Errorf("exit code = %d, want 1", got)
	}
}

func TestDoctorReportsDriftedLabel(t *testing.T) {
	dir := initRepo(t, doctorTestPolicy)
	client, _ := fakeGitHub(t, dir, func(o *map[string]string) {
		(*o)["labels"] = canonicalLabelsJSON(t, func(list []map[string]any) {
			for _, l := range list {
				if l["name"] == "type:bug" {
					l["color"] = "000000"
				}
			}
		})
	})
	rep := runDoctor(t, dir, client)

	var labelFails []Finding
	for _, f := range findingsWithSeverity(rep, Fail) {
		if f.Check == "remote/labels" {
			labelFails = append(labelFails, f)
		}
	}
	if len(labelFails) != 1 {
		t.Fatalf("label FAILs = %d, want exactly 1 in %+v", len(labelFails), rep.Findings)
	}
	if !strings.Contains(labelFails[0].Message, `"type:bug"`) || !strings.Contains(labelFails[0].Message, "000000") {
		t.Errorf("drift FAIL does not name the label and color: %q", labelFails[0].Message)
	}
}

func TestDoctorIgnoresUnrelatedLabels(t *testing.T) {
	dir := initRepo(t, doctorTestPolicy)
	client, _ := fakeGitHub(t, dir, func(o *map[string]string) {
		(*o)["labels"] = canonicalLabelsJSON(t, func(list []map[string]any) {
			list = append(list, map[string]any{
				"name": "my-team", "color": "123456", "description": "team-owned", "default": false,
			})
		})
	})
	rep := runDoctor(t, dir, client)

	for _, f := range findingsWithSeverity(rep, Fail) {
		if f.Check == "remote/labels" {
			t.Errorf("unrelated labels are not drift: %+v", f)
		}
	}
	var labelsPass bool
	for _, f := range findingsWithSeverity(rep, Pass) {
		if f.Check == "remote/labels" {
			labelsPass = true
		}
	}
	if !labelsPass {
		t.Errorf("expected remote/labels PASS despite unrelated labels in %+v", rep.Findings)
	}
}

func TestDoctorDetectsDisabledPrivateVulnReporting(t *testing.T) {
	dir := initRepo(t, doctorTestPolicy)
	client, _ := fakeGitHub(t, dir, func(o *map[string]string) {
		(*o)["private_vulnerability_reporting"] = `{"enabled": false}`
	})
	rep := runDoctor(t, dir, client)

	var found bool
	for _, f := range findingsWithSeverity(rep, Fail) {
		if f.Check == "remote/security" && strings.Contains(f.Message, "Private vulnerability reporting") {
			found = true
		}
	}
	if !found {
		t.Errorf("expected remote/security FAIL for disabled private vulnerability reporting in %+v", rep.Findings)
	}
	if got := ExitCode(rep, nil); got != 1 {
		t.Errorf("exit code = %d, want 1", got)
	}
}

func TestDoctorLocalPlaceholderFail(t *testing.T) {
	dir := initRepo(t, doctorTestPolicy)
	// Built at runtime so this source file does not trip its own detector.
	write(t, dir, ".github/CODEOWNERS", "* {{ "+" .Owner }}\n")
	git(t, dir, "add", "-A")
	git(t, dir, "commit", "-m", "chore: add codeowners")
	client, _ := fakeGitHub(t, dir, nil)
	rep := runDoctor(t, dir, client)

	fails := findingsWithSeverity(rep, Fail)
	var placeholderFail bool
	for _, f := range fails {
		if f.Check == "local/placeholders" {
			placeholderFail = true
		}
	}
	if !placeholderFail {
		t.Fatalf("expected local/placeholders FAIL, got: %+v", fails)
	}
}

// TestDoctorExitsTwoWhenAuditCannotComplete pins spec.md §11.4: an audit
// that could not complete is exit 2, never exit 1. Reporting an API
// failure as "drift found" is a false-compliance defect — CI would
// block merges on an infrastructure fault while looking like real
// drift.
//
// On `main` the read failures were converted into FAIL findings with a
// nil error, so an HTTP 401/403/500 produced exit 1.
func TestDoctorExitsTwoWhenAuditCannotComplete(t *testing.T) {
	for _, c := range []struct{ name, status string }{
		{"unauthorized", "401"},
		{"forbidden", "403"},
		{"rate limited", "403"},
		{"server error", "500"},
		{"repository not found", "404"},
	} {
		t.Run(c.name, func(t *testing.T) {
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.Header().Set("Content-Type", "application/json")
				w.WriteHeader(atoi(c.status))
				fmt.Fprintf(w, `{"message": "%s"}`, c.name)
			}))
			defer srv.Close()
			client := ghub.New(ghub.WithBaseURL(srv.URL), ghub.WithRetryBackoff(time.Millisecond))

			dir := initRepo(t, doctorTestPolicy)
			rep, err := Run(context.Background(), Options{StartDir: dir, Repo: "o/r", Client: client})
			if err == nil {
				t.Fatal("an audit that could not complete must return an error")
			}
			if got := ExitCode(rep, err); got != 2 {
				t.Errorf("exit = %d, want 2 (audit could not complete, spec.md §11.4)", got)
			}
			// The failure is visible in the findings a human reads...
			var found bool
			for _, f := range rep.Findings {
				if f.Severity == Fail && strings.Contains(f.Message, "cannot read repository") {
					found = true
				}
			}
			if !found {
				t.Error("the API failure must be visible in the findings")
			}
			// ...but exit 2 (audit incomplete) takes precedence over exit 1
			// (drift), because the important distinction is that the repo
			// was never audited. The CLI errs on the side of "we could not
			// ask", which is what spec.md §11.4 requires.
			if got := ExitCode(rep, err); got != 2 {
				t.Errorf("ExitCode = %d, want 2", got)
			}

		})
	}
}

// TestDoctorExitsOneOnRealDrift keeps the other half of spec.md §11.4:
// genuine drift is a DrIFT finding, exit 1.
func TestDoctorExitsOneOnRealDrift(t *testing.T) {
	dir := initRepo(t, doctorTestPolicy)
	client, _ := fakeGitHub(t, dir, func(o *map[string]string) {
		(*o)["repo"] = strings.Replace(repoJSON, `"allow_merge_commit": false`, `"allow_merge_commit": true`, 1)
	})
	rep, err := Run(context.Background(), Options{StartDir: dir, Repo: "o/r", Client: client})
	if err != nil {
		t.Fatalf("doctor.Run: %v", err)
	}
	if !rep.Failed() {
		t.Error("expected FAIL findings for a drifted repository")
	}
	if got := ExitCode(rep, nil); got != 1 {
		t.Errorf("exit = %d, want 1 (drift found)", got)
	}
}

// TestDoctorExitsZeroWhenConvergent pins exit 0 for no drift.
func TestDoctorExitsZeroWhenConvergent(t *testing.T) {
	dir := initRepo(t, doctorTestPolicy)
	client, _ := fakeGitHub(t, dir, nil)
	rep, err := Run(context.Background(), Options{StartDir: dir, Repo: "o/r", Client: client})
	if err != nil {
		t.Fatalf("doctor.Run: %v", err)
	}
	if rep.Failed() {
		t.Errorf("unexpected FAIL findings: %+v", rep.Findings)
	}
	if got := ExitCode(rep, nil); got != 0 {
		t.Errorf("exit = %d, want 0 (no drift)", got)
	}
}

func atoi(s string) int {
	n := 0
	for _, r := range s {
		if r < '0' || r > '9' {
			continue
		}
		n = n*10 + int(r-'0')
	}
	return n
}
