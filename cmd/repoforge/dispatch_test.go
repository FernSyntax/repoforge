// Copyright the repoforge contributors.
//
// SPDX-License-Identifier: Apache-2.0

package main

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/FernSyntax/repoforge/internal/version"
)

// runCLI invokes the dispatch entry point the way the binary does and
// returns the exit code with both streams.
func runCLI(args ...string) (int, string, string) {
	var out, errOut bytes.Buffer
	code := run(args, &out, &errOut)
	return code, out.String(), errOut.String()
}

// TestRunDispatch pins the top-level dispatch contract: every path a
// caller can take without a valid subcommand is an invocation error
// with the usage text, and the two informational paths print to
// stdout with a success code. Exit codes are the machine-readable
// part of this contract, so they are asserted exactly.
func TestRunDispatch(t *testing.T) {
	if code, out, errOut := runCLI(); code != 2 || out != "" || !strings.Contains(errOut, "Usage:") {
		t.Fatalf("no args = %d, want 2 with usage on stderr (out=%q err=%q)", code, out, errOut)
	}
	if code, _, errOut := runCLI("bogus"); code != 2 || !strings.Contains(errOut, `unknown command "bogus"`) {
		t.Fatalf("unknown command = %d, want 2 with the command named", code)
	}
	for _, arg := range []string{"help", "--help", "-h"} {
		code, out, _ := runCLI(arg)
		if code != 0 || !strings.Contains(out, "Usage:") {
			t.Fatalf("%s = %d, want 0 with usage on stdout", arg, code)
		}
	}
	for _, arg := range []string{"version", "--version"} {
		code, out, _ := runCLI(arg)
		if code != 0 || strings.TrimSpace(out) != version.Version {
			t.Fatalf("%s = %d out=%q, want the version string", arg, code, out)
		}
	}
}

// TestSchemaCheckContract pins the schema-parity gate's CLI surface:
// the subcommand shape is enforced, the parity check itself runs from
// the repository root, and a start directory with no repository is an
// invocation error rather than a silent pass.
func TestSchemaCheckContract(t *testing.T) {
	if code, _, errOut := runCLI("schema"); code != 2 || !strings.Contains(errOut, "want subcommand 'check'") {
		t.Fatalf("no subcommand = %d, want 2", code)
	}
	if code, _, _ := runCLI("schema", "parity"); code != 2 {
		t.Fatalf("wrong subcommand = %d, want 2", code)
	}
	if code, _, errOut := runCLI("schema", "check", "extra"); code != 2 || !strings.Contains(errOut, "unexpected argument") {
		t.Fatalf("unexpected argument = %d, want 2", code)
	}
	if code, _, _ := runCLI("schema", "check", "--path", t.TempDir()); code != 2 {
		t.Fatalf("a directory with no repository = %d, want 2", code)
	}
	chdir(t, repoRoot(t))
	code, out, _ := runCLI("schema", "check")
	if code != 0 || !strings.Contains(out, "schema pair(s) identical") {
		t.Fatalf("the engine checkout = %d, want 0 with the parity summary (out=%q)", code, out)
	}
}

// TestGitSubcommands pin the git rule checks: the subcommand shape,
// the message/branch contracts, and that a violation is exit 1
// (findings) while a usage mistake is exit 2 — the distinction the
// hooks depend on.
func TestGitSubcommands(t *testing.T) {
	if code, _, _ := runCLI("git"); code != 2 {
		t.Fatalf("no subcommand = %d, want 2", code)
	}
	if code, _, _ := runCLI("git", "status"); code != 2 {
		t.Fatalf("unknown subcommand = %d, want 2", code)
	}
	if code, _, errOut := runCLI("git", "commit-check"); code != 2 || !strings.Contains(errOut, "message required") {
		t.Fatalf("commit-check without a message = %d, want 2", code)
	}
	code, out, _ := runCLI("git", "commit-check", "feat: add the thing")
	if code != 0 || !strings.Contains(out, "commit-check: OK") {
		t.Fatalf("a conforming message = %d, want 0 (out=%q)", code, out)
	}
	if code, _, _ := runCLI("git", "commit-check", "wip"); code != 1 {
		t.Fatalf("a violating message = %d, want 1", code)
	}
	if code, _, _ := runCLI("git", "branch-check"); code != 2 {
		t.Fatalf("branch-check without a branch = %d, want 2", code)
	}
	if code, _, _ := runCLI("git", "branch-check", "a", "b"); code != 2 {
		t.Fatalf("branch-check with two branches = %d, want 2", code)
	}
	if code, out, _ := runCLI("git", "branch-check", "feat/7-add-the-thing"); code != 0 || !strings.Contains(out, "branch-check: OK") {
		t.Fatalf("a conforming branch = %d, want 0", code)
	}
	if code, _, _ := runCLI("git", "branch-check", "wip"); code != 1 {
		t.Fatalf("a violating branch = %d, want 1", code)
	}
}

// TestWikiUsage pins the wiki subcommand shape and the slug gate:
// a malformed --repo slug is rejected before any network call, so the
// check is deterministic offline.
func TestWikiUsage(t *testing.T) {
	if code, _, errOut := runCLI("wiki"); code != 2 || !strings.Contains(errOut, "want subcommand 'sync'") {
		t.Fatalf("no subcommand = %d, want 2", code)
	}
	if code, _, _ := runCLI("wiki", "push"); code != 2 {
		t.Fatalf("unknown subcommand = %d, want 2", code)
	}
	if code, _, errOut := runCLI("wiki", "sync", "extra"); code != 2 || !strings.Contains(errOut, "unexpected argument") {
		t.Fatalf("unexpected argument = %d, want 2", code)
	}
	chdir(t, repoRoot(t))
	if code, _, errOut := runCLI("wiki", "sync", "--repo", "not-a-slug"); code != 2 ||
		!strings.Contains(errOut, "invalid repository slug") {
		t.Fatalf("a malformed slug = %d, want 2 with the slug named (err=%q)", code, errOut)
	}
}

// TestDoctorUsage pins the doctor flag surface. The audit itself is
// read-only and network-reaching, so the deterministic assertions are
// the invocation errors and the JSON report shape.
func TestDoctorUsage(t *testing.T) {
	if code, _, errOut := runCLI("doctor", "extra"); code != 2 || !strings.Contains(errOut, "unexpected argument") {
		t.Fatalf("unexpected argument = %d, want 2", code)
	}
	if code, _, _ := runCLI("doctor", "--nope"); code != 2 {
		t.Fatalf("an unknown flag = %d, want 2", code)
	}
	code, out, _ := runCLI("doctor", "--json", "--path", t.TempDir())
	if code != 2 {
		t.Fatalf("an unauditable directory = %d, want 2", code)
	}
	var report map[string]any
	if err := json.Unmarshal([]byte(out), &report); err != nil {
		t.Fatalf("the JSON report must be valid JSON even when the audit is incomplete: %v\n%s", err, out)
	}
}

// TestBootstrapUsage pins the bootstrap flag surface. The apply path
// is deliberately not exercised here: it reaches the GitHub API, and
// a test that mutates a live repository is not a test.
func TestBootstrapUsage(t *testing.T) {
	if code, _, errOut := runCLI("bootstrap", "extra"); code != 2 || !strings.Contains(errOut, "unexpected argument") {
		t.Fatalf("unexpected argument = %d, want 2", code)
	}
	if code, _, _ := runCLI("bootstrap", "--nope"); code != 2 {
		t.Fatalf("an unknown flag = %d, want 2", code)
	}
}

// TestTemplateSubcommandUsage pins the template subcommand shape: the
// dispatch names the subcommands, an unknown one is an invocation
// error, and a bad flag is an invocation error.
func TestTemplateSubcommandUsage(t *testing.T) {
	if code, _, errOut := runCLI("template"); code != 2 ||
		!strings.Contains(errOut, "want subcommand 'validate', 'build', 'verify', 'provenance', 'adopted', or 'adopt'") {
		t.Fatalf("no subcommand = %d, want 2 (err=%q)", code, errOut)
	}
	if code, _, _ := runCLI("template", "render"); code != 2 {
		t.Fatalf("unknown subcommand = %d, want 2", code)
	}
	if code, _, _ := runCLI("template", "validate", "--nope"); code != 2 {
		t.Fatalf("an unknown flag = %d, want 2", code)
	}
}

// provenanceTree builds a real template tree with provenance recorded,
// the way the release pipeline does, and returns its directory.
func provenanceTree(t *testing.T) string {
	t.Helper()
	out := filepath.Join(t.TempDir(), "built")
	code, _, errOut := runCLI("template", "build", "--path", defaultTemplateSource, "--out", out,
		"--source-ref", "v0.1.0", "--source-commit", strings.Repeat("a", 40),
		"--source-url", "https://example.invalid/repoforge", "--repoforge-version", "v0.1.0")
	if code != 0 {
		t.Fatalf("build = %d, want 0 (err=%q)", code, errOut)
	}
	return out
}

// TestTemplateProvenancePrintsTheRecordedFlags pins the provenance
// printer: the flags a determinism check needs to rebuild like with
// like, and nothing when the manifest records no source — an empty
// flag value would shift the next flag and corrupt the rebuild.
func TestTemplateProvenancePrintsTheRecordedFlags(t *testing.T) {
	chdir(t, repoRoot(t))
	dir := provenanceTree(t)
	code, out, errOut := runCLI("template", "provenance", "--path", dir)
	if code != 0 || !strings.Contains(out, "--source-ref") || !strings.Contains(out, "--source-commit") {
		t.Fatalf("a provenance-bearing tree = %d, want 0 with the recorded flags (out=%q err=%q)", code, out, errOut)
	}
	if errOut != "" {
		t.Errorf("provenance must be quiet on success, got %q", errOut)
	}
	code, _, _ = runCLI("template", "provenance", "--path", t.TempDir())
	if code != 2 {
		t.Fatalf("a tree without a manifest = %d, want 2", code)
	}
}

// TestTemplateAdoptedReportsTheGateState pins the machine-readable
// adoption report: exit 1 with the reason for a tree whose provenance
// gate still applies, exit 0 once adoption cleared it. A consumer's
// CI branches on exactly these two outcomes.
func TestTemplateAdoptedReportsTheGateState(t *testing.T) {
	chdir(t, repoRoot(t))
	dir := provenanceTree(t)
	if code, _, errOut := runCLI("template", "adopted", "--path", dir); code != 1 ||
		!strings.Contains(errOut, "is not an adopted generated repository") {
		t.Fatalf("a non-adopted tree = %d, want 1 with the reason (err=%q)", code, errOut)
	}
	if code := run(append([]string{"template", "adopt", "--path", dir}, adoptionArgs()[2:]...),
		&bytes.Buffer{}, &bytes.Buffer{}); code != 0 {
		t.Fatalf("adoption = %d, want 0", code)
	}
	code, out, _ := runCLI("template", "adopted", "--path", dir)
	if code != 0 || !strings.Contains(out, "the provenance gate does not apply") {
		t.Fatalf("an adopted tree = %d, want 0 with the confirmation (out=%q)", code, out)
	}
}

// TestReleaseReadyUsage pins the release gate's CLI surface. The gate
// itself runs a local script, so the shape errors and the
// missing-flag path are fully deterministic; a malformed tag fails
// the script's shape check with exit 1.
func TestReleaseReadyUsage(t *testing.T) {
	if code, _, errOut := runCLI("release"); code != 2 || !strings.Contains(errOut, "want subcommand 'ready'") {
		t.Fatalf("no subcommand = %d, want 2", code)
	}
	if code, _, _ := runCLI("release", "cut"); code != 2 {
		t.Fatalf("unknown subcommand = %d, want 2", code)
	}
	if code, _, errOut := runCLI("release", "ready"); code != 2 || !strings.Contains(errOut, "--tag is required") {
		t.Fatalf("a missing tag = %d, want 2", code)
	}
	if code, _, _ := runCLI("release", "ready", "--tag", "v0.1.1", "extra"); code != 2 {
		t.Fatalf("an unexpected argument = %d, want 2", code)
	}
	chdir(t, repoRoot(t))
	code, _, errOut := runCLI("release", "ready", "--tag", "bogus")
	if code != 1 || !strings.Contains(errOut, "not strict vMAJOR.MINOR.PATCH") {
		t.Fatalf("a malformed tag = %d, want 1 from the policy script (err=%q)", code, errOut)
	}
}

// TestHookUsage pins the hook wrapper's surface. The installer script
// is reached only from a repository root that carries it; a
// repository without it is a clean exit 1, never a crash.
func TestHookUsage(t *testing.T) {
	if code, _, errOut := runCLI("hook"); code != 2 || !strings.Contains(errOut, "want subcommand 'install', 'verify', or 'uninstall'") {
		t.Fatalf("no subcommand = %d, want 2", code)
	}
	if code, _, _ := runCLI("hook", "status"); code != 2 {
		t.Fatalf("unknown subcommand = %d, want 2", code)
	}
	if code, _, errOut := runCLI("hook", "verify", "extra"); code != 2 || !strings.Contains(errOut, "unexpected argument") {
		t.Fatalf("an unexpected argument = %d, want 2", code)
	}
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "README.md"), []byte("# repo\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := gitInit(t, dir); err != nil {
		t.Fatal(err)
	}
	code, _, errOut := runCLI("hook", "verify", "--path", dir)
	if code != 1 || !strings.Contains(errOut, "cannot reach scripts/hooks-install") {
		t.Fatalf("a repository without the installer = %d, want 1 with the reason (err=%q)", code, errOut)
	}
}

// TestIssueUsage pins the issue form surface: the subcommand shape and
// the required flags, with the distinction between an invocation
// error (2) and a form the repository cannot serve (1).
func TestIssueUsage(t *testing.T) {
	if code, _, errOut := runCLI("issue"); code != 2 || !strings.Contains(errOut, "want subcommand 'scaffold', 'check', or 'create'") {
		t.Fatalf("no subcommand = %d, want 2", code)
	}
	if code, _, _ := runCLI("issue", "list"); code != 2 {
		t.Fatalf("unknown subcommand = %d, want 2", code)
	}
	if code, _, errOut := runCLI("issue", "scaffold"); code != 2 || !strings.Contains(errOut, "--type is required") {
		t.Fatalf("scaffold without a type = %d, want 2", code)
	}
	if code, _, _ := runCLI("issue", "scaffold", "--type", "epic"); code != 1 {
		t.Fatalf("scaffold with an unknown type = %d, want 1", code)
	}
	if code, _, _ := runCLI("issue", "check", "--type", "task"); code != 2 {
		t.Fatalf("check without a body file = %d, want 2", code)
	}
	if code, _, _ := runCLI("issue", "check", "--type", "task", "--file", "missing.md"); code != 2 {
		t.Fatalf("check with an unreadable body = %d, want 2", code)
	}
	if code, _, errOut := runCLI("issue", "create", "--type", "task", "--title", "t"); code != 2 ||
		!strings.Contains(errOut, "--file is required") {
		t.Fatalf("create without a body = %d, want 2", code)
	}
	if code, _, errOut := runCLI("issue", "create", "--title", "t", "--file", "b.md"); code != 2 ||
		!strings.Contains(errOut, "--type is required") {
		t.Fatalf("create without a type = %d, want 2", code)
	}
}

// TestPRUsage pins the pr subcommand shape and its required flags.
func TestPRUsage(t *testing.T) {
	if code, _, errOut := runCLI("pr"); code != 2 ||
		!strings.Contains(errOut, "want subcommand 'check', 'check-body', 'scaffold', 'ready', or 'create'") {
		t.Fatalf("no subcommand = %d, want 2", code)
	}
	if code, _, _ := runCLI("pr", "merge"); code != 2 {
		t.Fatalf("unknown subcommand = %d, want 2", code)
	}
	if code, _, errOut := runCLI("pr", "check", "extra"); code != 2 || !strings.Contains(errOut, "unexpected argument") {
		t.Fatalf("check with an unexpected argument = %d, want 2", code)
	}
	if code, _, errOut := runCLI("pr", "ready"); code != 2 || !strings.Contains(errOut, "--file is required") {
		t.Fatalf("ready without a body = %d, want 2", code)
	}
	if code, _, errOut := runCLI("pr", "create", "--file", "b.md"); code != 2 || !strings.Contains(errOut, "--title is required") {
		t.Fatalf("create without a title = %d, want 2", code)
	}
}

// TestValidateJSONEmitsTheReport pins the machine-readable validate
// output: valid JSON with the summary the automation consumes, and
// an invocation error for a start directory that is not a repository.
func TestValidateJSONEmitsTheReport(t *testing.T) {
	chdir(t, repoRoot(t))
	code, out, _ := runCLI("validate", "--json")
	if code != 0 {
		t.Fatalf("the engine checkout = %d, want 0", code)
	}
	var report map[string]any
	if err := json.Unmarshal([]byte(out), &report); err != nil {
		t.Fatalf("--json must emit valid JSON: %v", err)
	}
	if code, _, _ := runCLI("validate", "--path", filepath.Join(t.TempDir(), "absent")); code != 2 {
		t.Fatalf("a nonexistent path = %d, want 2", code)
	}
	if code, _, _ := runCLI("validate", "extra"); code != 2 {
		t.Fatalf("an unexpected argument = %d, want 2", code)
	}
}
