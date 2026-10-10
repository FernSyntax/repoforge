package bootstrap

import (
	"context"
	"strings"
	"testing"

	"github.com/FernSyntax/repoforge/configs"
	"github.com/FernSyntax/repoforge/internal/doctor"
	ghub "github.com/FernSyntax/repoforge/internal/github"
	"github.com/FernSyntax/repoforge/internal/validate"
)

// Full local lifecycle simulation, per governance profile: fresh
// repository → bootstrap plan (dry-run) → bootstrap execution
// against the fake GitHub API → second bootstrap with zero changes →
// doctor PASS → intentional remote drift → doctor detects drift →
// bootstrap reconciles → local validation gates (malformed config,
// commit, branch) → ruleset drift detected and reconciled.
//
// Everything runs offline: git in a temp dir, HTTP against the
// httptest fake. Assertions use containment, never ordering, so the
// suite is deterministic run to run.

func lifecyclePolicy(profile string) string {
	return `standard:
  version: v1
repository:
  governance_profile: ` + profile + `
  visibility: public
  default_branch: main
github:
  issues: true
  wiki: true
ownership:
  managed_paths:
    - LICENSE
`
}

func TestLifecycleSolo(t *testing.T) {
	lifecycle(t, "solo")
}

func TestLifecycleTeam(t *testing.T) {
	lifecycle(t, "team")
}

func TestLifecycleStrict(t *testing.T) {
	lifecycle(t, "strict")
}

func lifecycle(t *testing.T, profile string) {
	t.Helper()
	ctx := context.Background()

	// 1-3. Fresh repository with valid RepoForge config and
	// valid template contents.
	dir := initRepo(t, lifecyclePolicy(profile))
	fake := newFakeGitHub(t)
	client := fake.client(t)

	// 4. Bootstrap plan: dry-run schedules work and mutates nothing.
	resDry, err := Run(ctx, Options{StartDir: dir, Repo: "o/r", DryRun: true, Client: client})
	if err != nil {
		t.Fatalf("profile %s dry-run: %v", profile, err)
	}
	var ruleCreates, labelCreates int
	for _, item := range resDry.Plan {
		if item.Area == "rulesets" && item.Action == ActionCreate {
			ruleCreates++
		}
		if item.Area == "labels" && item.Action == ActionCreate {
			labelCreates++
		}
	}
	if ruleCreates != 2 {
		t.Errorf("profile %s: ruleset creates planned = %d, want 2", profile, ruleCreates)
	}
	wantLabels, err := configs.CanonicalLabels()
	if err != nil {
		t.Fatal(err)
	}
	if labelCreates != len(wantLabels) {
		t.Errorf("profile %s: label creates planned = %d, want %d", profile, labelCreates, len(wantLabels))
	}
	if muts := fake.mutatingRequests(); len(muts) > 0 {
		t.Fatalf("profile %s: dry-run mutated: %v", profile, muts)
	}

	// 5. Bootstrap execution converges the fake repository.
	res, err := Run(ctx, Options{StartDir: dir, Repo: "o/r", Yes: true, Client: client})
	if err != nil {
		t.Fatalf("profile %s bootstrap: %v", profile, err)
	}
	if !res.Converged {
		t.Fatalf("profile %s did not converge: %+v", profile, res.Plan)
	}

	// 6. Second bootstrap produces zero changes.
	res2, err := Run(ctx, Options{StartDir: dir, Repo: "o/r", Yes: true, Client: client})
	if err != nil {
		t.Fatalf("profile %s second bootstrap: %v", profile, err)
	}
	if res2.Applied != 0 || !res2.Converged {
		t.Fatalf("profile %s not idempotent: applied=%d converged=%t plan=%+v",
			profile, res2.Applied, res2.Converged, res2.Plan)
	}

	// 7. Doctor shows PASS.
	rep := runDoctorLifecycle(t, dir, client)
	if rep.Failed() {
		t.Fatalf("profile %s: doctor FAILs on converged repo: %+v", profile, rep.Findings)
	}
	if got := doctor.ExitCode(rep, nil); got != 0 {
		t.Fatalf("profile %s: doctor exit = %d, want 0", profile, got)
	}

	// 8. Intentional configuration drift on the remote side.
	driftRemote(t, fake)

	// 9. Doctor detects drift, per area.
	rep = runDoctorLifecycle(t, dir, client)
	if !rep.Failed() {
		t.Fatalf("profile %s: doctor missed drift", profile)
	}
	assertCheckFail(t, profile, rep, "remote/merge", "merge commit")
	assertCheckFail(t, profile, rep, "remote/rulesets", configs.MainRulesetName)
	assertCheckFail(t, profile, rep, "remote/labels", `"type:bug"`)
	assertCheckFail(t, profile, rep, "remote/security", "Dependabot alerts")
	assertCheckFail(t, profile, rep, "remote/security", "Private vulnerability reporting")
	if got := doctor.ExitCode(rep, nil); got != 1 {
		t.Fatalf("profile %s: doctor exit = %d, want 1", profile, got)
	}

	// 10. Bootstrap reconciles drift; doctor is green again.
	res3, err := Run(ctx, Options{StartDir: dir, Repo: "o/r", Yes: true, Client: client})
	if err != nil {
		t.Fatalf("profile %s reconcile: %v", profile, err)
	}
	if !res3.Converged {
		t.Fatalf("profile %s did not reconverge: %+v", profile, res3.Plan)
	}
	rep = runDoctorLifecycle(t, dir, client)
	if rep.Failed() {
		t.Fatalf("profile %s: doctor FAILs after reconcile: %+v", profile, rep.Findings)
	}

	// 14. Ruleset validation, isolated: drift only the tag ruleset
	// enforcement, confirm doctor names it, reconcile, confirm PASS.
	driftTagsEnforcement(t, fake)
	rep = runDoctorLifecycle(t, dir, client)
	assertCheckFail(t, profile, rep, "remote/rulesets", configs.ReleaseTagsRulesetName)
	if _, err := Run(ctx, Options{StartDir: dir, Repo: "o/r", Yes: true, Client: client}); err != nil {
		t.Fatalf("profile %s ruleset reconcile: %v", profile, err)
	}
	rep = runDoctorLifecycle(t, dir, client)
	if rep.Failed() {
		t.Fatalf("profile %s: doctor FAILs after ruleset reconcile: %+v", profile, rep.Findings)
	}

	// 11. Validate detects malformed local configuration.
	writeFile(t, dir, "repoforge.yaml", "standard:\n  version: v1\nrepository:\n  governance_profile: enterprise\n")
	vrep, verr := validate.Run(validate.Options{StartDir: dir})
	if verr != nil {
		t.Fatalf("profile %s validate: %v", profile, verr)
	}
	if vrep.Valid() || validate.ExitCode(vrep, nil) != 1 {
		t.Fatalf("profile %s: malformed policy must fail validation: %+v", profile, vrep.Findings)
	}
	writeFile(t, dir, "repoforge.yaml", lifecyclePolicy(profile))

	// 12. Commit validation.
	writeFile(t, dir, "oops.txt", "x\n")
	git(t, dir, "add", "-A")
	git(t, dir, "commit", "-m", "oops broken message")
	vrep, _ = validate.Run(validate.Options{StartDir: dir})
	if !hasValidateRule(vrep, "commit/message") {
		t.Fatalf("profile %s: bad commit must fail validation: %+v", profile, vrep.Findings)
	}

	// 13. Branch validation.
	git(t, dir, "checkout", "-b", "Bad_Branch_NAME")
	vrep, _ = validate.Run(validate.Options{StartDir: dir})
	if !hasValidateRule(vrep, "branch/name") {
		t.Fatalf("profile %s: bad branch must fail validation: %+v", profile, vrep.Findings)
	}
}

// driftRemote mutates the fake remote across four areas at once.
func driftRemote(t *testing.T, fake *fakeGitHub) {
	t.Helper()
	fake.mu.Lock()
	defer fake.mu.Unlock()
	fake.merge["allow_merge_commit"] = true
	fake.vulnAlerts = false
	fake.privateVulnReporting = false
	for _, rs := range fake.rulesets {
		if rs.name == configs.MainRulesetName {
			rs.enforcement = "disabled"
		}
	}
	if l, ok := fake.labels["type:bug"]; ok {
		l.color = "000000"
	} else {
		t.Fatal("owned label type:bug missing after converge")
	}
}

// driftTagsEnforcement drifts only the tag ruleset.
func driftTagsEnforcement(t *testing.T, fake *fakeGitHub) {
	t.Helper()
	fake.mu.Lock()
	defer fake.mu.Unlock()
	for _, rs := range fake.rulesets {
		if rs.name == configs.ReleaseTagsRulesetName {
			rs.enforcement = "evaluate"
		}
	}
}

func runDoctorLifecycle(t *testing.T, dir string, client *ghub.Client) doctor.Report {
	t.Helper()
	rep, err := doctor.Run(context.Background(), doctor.Options{StartDir: dir, Repo: "o/r", Client: client})
	if err != nil {
		t.Fatalf("doctor.Run: %v", err)
	}
	return rep
}

func assertCheckFail(t *testing.T, profile string, rep doctor.Report, check, snippet string) {
	t.Helper()
	for _, f := range rep.Findings {
		if f.Severity == doctor.Fail && f.Check == check && strings.Contains(f.Message, snippet) {
			return
		}
	}
	t.Errorf("profile %s: no FAIL finding for check %q containing %q in %+v", profile, check, snippet, rep.Findings)
}

func hasValidateRule(rep validate.Report, rule string) bool {
	for _, f := range rep.Findings {
		if f.Rule == rule {
			return true
		}
	}
	return false
}
