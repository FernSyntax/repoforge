// Copyright the repoforge contributors.
//
// SPDX-License-Identifier: Apache-2.0

package bootstrap

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/FernSyntax/repoforge/internal/config"
	"github.com/FernSyntax/repoforge/internal/doctor"
)

// securityPatches returns every (secret_scanning, push_protection)
// status pair that a PATCH of the repository object's
// security_and_analysis carried, in order. Asserting the exact wire
// body is what makes this test meaningful: the old bug passed a literal
// `true` in one field of the Go call, so only the wire body can prove
// it is fixed.
func (f *fakeGitHub) securityPatches() [][2]string {
	f.mu.Lock()
	raw := append([]string(nil), f.patchBodies...)
	f.mu.Unlock()
	var out [][2]string
	for _, s := range raw {
		var body struct {
			SecurityAndAnalysis map[string]map[string]string `json:"security_and_analysis"`
		}
		if err := json.Unmarshal([]byte(s), &body); err != nil {
			continue
		}
		if len(body.SecurityAndAnalysis) == 0 {
			continue
		}
		var pair [2]string
		if v, ok := body.SecurityAndAnalysis["secret_scanning"]; ok {
			pair[0] = v["status"]
		}
		if v, ok := body.SecurityAndAnalysis["secret_scanning_push_protection"]; ok {
			pair[1] = v["status"]
		}
		out = append(out, pair)
	}
	return out
}

// seededSecretScanningCase seeds the fake repository with the actual
// secret-scanning state and returns a repo whose policy declares the
// desired pair.
func seededSecretScanningCase(t *testing.T, actualScan, actualPush bool, visibility string) (*fakeGitHub, string) {
	t.Helper()
	f := newFakeGitHub(t)
	f.visibility = visibility
	f.secretScanning = statusText(actualScan)
	f.pushProtection = statusText(actualPush)
	seedCanonicalLabels(t, f)
	return f, initRepo(t, bootstrapPolicy)
}

// securityPolicy writes the secret-scanning policy into an existing
// fixture policy file and returns the parsed policy.
func securityPolicy(t *testing.T, dir string, scan, push bool) *config.Policy {
	t.Helper()
	path := dir + "/repoforge.yaml"
	body := "standard:\n" +
		"  version: v1\n" +
		"repository:\n" +
		"  governance_profile: solo\n" +
		"  visibility: public\n" +
		"  default_branch: main\n" +
		"github:\n" +
		"  issues: true\n" +
		"  wiki: true\n" +
		"merge:\n" +
		"  squash: true\n" +
		"security:\n" +
		"  secret_scanning: " + literalBool(scan) + "\n" +
		"  secret_scanning_push_protection: " + literalBool(push) + "\n" +
		"ownership:\n" +
		"  managed_paths:\n" +
		"    - LICENSE\n"
	writeFile(t, dir, "repoforge.yaml", body)
	pol, _, err := config.LoadPolicy(path)
	if err != nil {
		t.Fatalf("LoadPolicy: %v", err)
	}
	return pol
}

// planFor reads the fake over HTTP and computes a plan. It performs no
// mutation, so it is also the dry-run path under test.
func planFor(t *testing.T, dir string, pol *config.Policy, f *fakeGitHub) ([]PlanItem, *execEnv) {
	t.Helper()
	gov, err := doctor.LoadProfile(pol)
	if err != nil {
		t.Fatalf("LoadProfile: %v", err)
	}
	env := &execEnv{client: f.client(t), owner: "o", repo: "r"}
	actual, err := readActual(context.Background(), env.client, env.owner, env.repo)
	if err != nil {
		t.Fatalf("readActual: %v", err)
	}
	return computePlan(dir, pol, gov, actual, env), env
}

// hasViolationField reports whether any violation names the field.
func hasViolationField(vs []config.Violation, field string) bool {
	for _, v := range vs {
		if strings.Contains(v.Field, field) {
			return true
		}
	}
	return false
}

func literalBool(b bool) string {
	if b {
		return "true"
	}
	return "false"
}

func statusText(on bool) string {
	if on {
		return "enabled"
	}
	return "disabled"
}

func boolPtr(b bool) *bool { return &b }

func ss(on bool) string {
	if on {
		return "on"
	}
	return "off"
}

func securityItemFor(plan []PlanItem, subject string) (PlanItem, bool) {
	for _, p := range plan {
		if p.Area == "security" && p.Subject == subject {
			return p, true
		}
	}
	return PlanItem{}, false
}

// securityCase drives one cell of the truth table end to end: seed the
// actual state, write the desired policy, plan, apply, read back over
// HTTP, and re-plan. It asserts the exact request body, so the
// regression it pins is unambiguous.
//
// On `main` the two callbacks were
//
//	SetSecretScanning(ctx, …, true,           pushProtection)
//	SetSecretScanning(ctx, …, secretScanning, true)
//
// so a plan to disable either feature re-enabled it and the post-apply
// re-read reported drift forever (permanent exit 1).
func securityCase(t *testing.T, actualScan, actualPush, wantScan, wantPush bool, visibility string) {
	t.Helper()

	// Push protection cannot be enabled while secret scanning is
	// disabled: the schema rejects it, so the pair never reaches
	// planning. Assert the contract instead of a plan.
	if wantPush && !wantScan {
		_, dir := seededSecretScanningCase(t, actualScan, actualPush, visibility)
		pol := securityPolicy(t, dir, wantScan, wantPush)
		if violations := config.ValidatePolicy(pol); !hasViolationField(violations, "secret_scanning_push_protection") {
			t.Errorf("policy must reject push protection with secret scanning disabled: %v", violations)
		}
		if _, _, err := loadDesired(dir); err == nil {
			t.Error("loadDesired must refuse a policy with push protection but no secret scanning")
		}
		return
	}

	f, dir := seededSecretScanningCase(t, actualScan, actualPush, visibility)
	pol := securityPolicy(t, dir, wantScan, wantPush)
	plan, env := planFor(t, dir, pol, f)

	item, ok := securityItemFor(plan, "secret scanning")
	if !ok {
		t.Fatalf("no 'secret scanning' plan item in: %v", planSubjects(plan))
	}
	pushItem, _ := securityItemFor(plan, "secret scanning push protection")

	if actualScan == wantScan && actualPush == wantPush {
		if item.Action != ActionNoop {
			t.Errorf("state already matches policy, but plan says [%s] %s", item.Action, item.Detail)
		}
		if pushItem.Action != ActionNoop {
			t.Errorf("push protection also matches, but plan says [%s] %s", pushItem.Action, pushItem.Detail)
		}
		if got := f.securityPatches(); len(got) != 0 {
			t.Errorf("a converged state sent %d security_and_analysis patch(es)", len(got))
		}
		return
	}

	// A drift exists. Decide whether the transition is reachable.
	enabling := (wantScan && !actualScan) || (wantPush && !actualPush)
	gated := visibility == "private"

	if enabling && gated {
		// Enabling on a private repository needs a GitHub Advanced
		// Security product: it cannot converge, so it must be classified
		// as unsupported and never sent.
		//
		// The feature that stays disabled on policy ("secret scanning"
		// itself, when only push protection is wanted) is reported as
		// no-op; the feature that cannot be enabled is reported as
		// unsupported. Both must be non-mutating.
		if item.applicable() {
			t.Errorf("private-repository enable must never be mutated: [%s] %s", item.Action, item.Detail)
		}
		if pushItem.applicable() {
			t.Errorf("push protection must never be mutated on a private repository: [%s] %s",
				pushItem.Action, pushItem.Detail)
		}
		if got := f.mutatingRequests(); len(got) != 0 {
			t.Errorf("a doomed mutation was attempted: %v", got)
		}
		return
	}

	if !item.applicable() {
		t.Fatalf("drift is convergent on %s but the plan carries no mutation: [%s] %s",
			visibility, item.Action, item.Detail)
	}

	before := len(f.securityPatches())
	if err := item.apply(context.Background()); err != nil {
		t.Fatalf("apply: %v", err)
	}
	patches := f.securityPatches()
	if len(patches) != before+1 {
		t.Fatalf("apply sent %d security_and_analysis patch(es), want exactly 1", len(patches)-before)
	}
	last := patches[len(patches)-1]
	if last[0] != statusText(wantScan) {
		t.Errorf("PATCH secret_scanning = %q, want %q — a disable re-enabled the feature",
			last[0], statusText(wantScan))
	}
	if last[1] != statusText(wantPush) {
		t.Errorf("PATCH secret_scanning_push_protection = %q, want %q — updating one setting undid the other",
			last[1], statusText(wantPush))
	}

	// Read the resulting remote state back over HTTP.
	remote, err := env.client.GetSecuritySettings(context.Background(), "o", "r")
	if err != nil {
		t.Fatalf("GetSecuritySettings: %v", err)
	}
	if remote.SecretScanning != statusText(wantScan) {
		t.Errorf("remote secret_scanning = %q, want %q", remote.SecretScanning, statusText(wantScan))
	}
	if remote.SecretScanningPushProtection != statusText(wantPush) {
		t.Errorf("remote secret_scanning_push_protection = %q, want %q",
			remote.SecretScanningPushProtection, statusText(wantPush))
	}

	// Re-plan: must now be a no-op for this setting.
	after, _ := planFor(t, dir, pol, f)
	again, ok := securityItemFor(after, "secret scanning")
	if !ok {
		t.Fatal("re-plan lost the secret scanning item")
	}
	if again.Action != ActionNoop {
		t.Errorf("re-plan is not converged: [%s] %s", again.Action, again.Detail)
	}
}

func planSubjects(plan []PlanItem) []string {
	var out []string
	for _, p := range plan {
		if p.Area == "security" {
			out = append(out, p.Subject+"="+string(p.Action))
		}
	}
	return out
}

// TestSecretScanningTruthTable is the full 4x4 grid of actual against
// desired (secret scanning, push protection), on both public and
// private repositories.
func TestSecretScanningTruthTable(t *testing.T) {
	for _, actualScan := range []bool{true, false} {
		for _, actualPush := range []bool{true, false} {
			for _, wantScan := range []bool{true, false} {
				for _, wantPush := range []bool{true, false} {
					t.Run("actual_"+ss(actualScan)+"push"+ss(actualPush)+"_want_"+ss(wantScan)+"push"+ss(wantPush),
						func(t *testing.T) {
							securityCase(t, actualScan, actualPush, wantScan, wantPush, "public")
							securityCase(t, actualScan, actualPush, wantScan, wantPush, "private")
						})
				}
			}
		}
	}
}

// The mission's named cases, each in its own test so a failure names
// the exact combination.

func TestSecretScanningBothEnabled(t *testing.T) {
	securityCase(t, true, true, true, true, "public")
}

func TestSecretScanningBothDisabled(t *testing.T) {
	securityCase(t, false, false, false, false, "public")
}

func TestSecretScanningEnableScanningOnly(t *testing.T) {
	securityCase(t, false, false, true, false, "public")
}

func TestSecretScanningEnablePushProtectionOnly(t *testing.T) {
	securityCase(t, false, false, false, true, "public")
}

// TestDisableScanKeepsPushProtection: disabling secret scanning must
// leave push protection exactly where policy asked.
func TestDisableScanKeepsPushProtection(t *testing.T) {
	securityCase(t, true, true, false, true, "public")
}

// TestDisablePushProtectionKeepsScan is its mirror.
func TestDisablePushProtectionKeepsScan(t *testing.T) {
	securityCase(t, true, true, true, false, "public")
}

// TestSecretScanningSimultaneousBothWays covers simultaneous changes
// in both directions, in both orders.
func TestSecretScanningSimultaneousBothWays(t *testing.T) {
	securityCase(t, true, false, false, true, "public")
	securityCase(t, false, true, true, false, "public")
}

// TestPrivateEnableIsUnsupported isolates the capability model for each
// enabling transition on a private repository. The
// (scanning off, push on) policy is invalid by schema and is covered
// by the truth table's contract assertion, so it is not repeated here.
func TestPrivateEnableIsUnsupported(t *testing.T) {
	for _, c := range []struct{ scan, push bool }{{true, true}, {true, false}, {false, false}} {
		f, dir := seededSecretScanningCase(t, false, false, "private")
		pol := securityPolicy(t, dir, c.scan, c.push)

		plan, _ := planFor(t, dir, pol, f)
		item, ok := securityItemFor(plan, "secret scanning")
		if !ok {
			t.Fatalf("scan=%v push=%v: no plan item", c.scan, c.push)
		}
		if c.scan {
			if item.applicable() {
				t.Errorf("scan=%v push=%v: must not be mutated: [%s] %s", c.scan, c.push, item.Action, item.Detail)
			}
			if item.Action != ActionUnsupported {
				t.Errorf("scan=%v push=%v: action = %s, want %s", c.scan, c.push, item.Action, ActionUnsupported)
			}
			if !hasUnsupported(plan) {
				t.Error("the capability gap is not reported as unsupported (the run could look converged)")
			}
		}
		if got := f.mutatingRequests(); len(got) != 0 {
			t.Errorf("scan=%v push=%v: a doomed mutation was attempted: %v", c.scan, c.push, got)
		}
	}
}

// TestDisableOnConverges proves the capability model never blocks a
// disable: GitHub permits disabling on any plan.
func TestDisableOnPrivateConverges(t *testing.T) {
	securityCase(t, true, true, false, false, "private")
}

// TestOtherSecurityTogglesTruthTable extends the same treatment to the
// remaining policy-managed security settings, so the defect class is
// covered rather than only this one instance.
func TestOtherSecurityTogglesTruthTable(t *testing.T) {
	toggles := []struct {
		subject string
		seed    func(f *fakeGitHub, on bool)
		apply   func(p *config.Policy, on bool)
	}{
		{"Dependabot alerts",
			func(f *fakeGitHub, on bool) { f.vulnAlerts = on },
			func(p *config.Policy, on bool) { p.Security.Dependabot = boolPtr(on) }},
		{"Dependabot security updates",
			func(f *fakeGitHub, on bool) { f.autoFixes = on },
			func(p *config.Policy, on bool) { p.Security.Dependabot = boolPtr(on) }},
		{"Private vulnerability reporting",
			func(f *fakeGitHub, on bool) { f.privateVulnReporting = on },
			func(p *config.Policy, on bool) { p.Security.PrivateVulnerabilityReporting = boolPtr(on) }},
	}
	for _, tog := range toggles {
		for _, actual := range []bool{true, false} {
			for _, want := range []bool{true, false} {
				f := newFakeGitHub(t)
				f.visibility = "public"
				tog.seed(f, actual)
				seedCanonicalLabels(t, f)
				dir := initRepo(t, bootstrapPolicy)
				pol := securityPolicy(t, dir, true, true)
				tog.apply(pol, want)

				plan, _ := planFor(t, dir, pol, f)
				item, ok := securityItemFor(plan, tog.subject)
				if !ok {
					t.Fatalf("%s: no plan item", tog.subject)
				}
				wantAction := ActionNoop
				if actual != want {
					if want {
						wantAction = ActionEnable
					} else {
						wantAction = ActionDisable
					}
				}
				if item.Action != wantAction {
					t.Errorf("%s (actual=%v, want=%v): action = %s, want %s — %s",
						tog.subject, actual, want, item.Action, wantAction, item.Detail)
				}
				if actual == want {
					continue
				}
				if err := item.apply(context.Background()); err != nil {
					t.Fatalf("%s: apply: %v", tog.subject, err)
				}
				after, _ := planFor(t, dir, pol, f)
				if i2, ok := securityItemFor(after, tog.subject); !ok || i2.Action != ActionNoop {
					t.Errorf("%s: did not converge after apply: %v", tog.subject, planSubjects(after))
				}
			}
		}
	}
}
