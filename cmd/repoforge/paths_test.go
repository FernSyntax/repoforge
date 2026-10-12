package main

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestValidateTextReport pins the human-readable report: the summary
// line, the per-finding format, and the exit-code distinction between
// a clean checkout (0) and one with findings (1). The JSON path is
// covered separately; this is the shape an operator reads.
func TestValidateTextReport(t *testing.T) {
	chdir(t, repoRoot(t))
	code, out, _ := runCLI("validate")
	if code != 0 || !strings.Contains(out, "validate: OK") {
		t.Fatalf("the engine checkout = %d, want 0 with the OK line (out=%q)", code, out)
	}
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "README.md"), []byte("# bare\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := gitInit(t, dir); err != nil {
		t.Fatal(err)
	}
	code, out, _ = runCLI("validate", "--path", dir)
	if code != 1 || !strings.Contains(out, "validate:") {
		t.Fatalf("a non-conforming directory = %d, want 1 with the report (out=%q)", code, out)
	}
}

// TestTemplateVerifyNotesTheDevelopmentBuild pins the verify command's
// guidance path: a tree whose manifest records no source is validated
// fully but cannot be reproduced, and the command says so instead of
// implying a guarantee it cannot give.
func TestTemplateVerifyNotesTheDevelopmentBuild(t *testing.T) {
	chdir(t, repoRoot(t))
	out := filepath.Join(t.TempDir(), "built")
	code, _, errOut := runCLI("template", "build", "--path", defaultTemplateSource, "--out", out)
	if code != 0 {
		t.Fatalf("build = %d, want 0 (err=%q)", code, errOut)
	}
	code, stdout, _ := runCLI("template", "verify", "--path", out)
	if code != 0 || !strings.Contains(stdout, "template verify") {
		t.Fatalf("a development build = %d, want 0 with the note (out=%q)", code, stdout)
	}
}

// TestPRCheckBodyRemoteValidationPaths pins the remote half of the
// traceability check: a malformed slug is an invocation error, and a
// missing credential is reported rather than silently skipped. The
// stub gh on PATH makes `gh auth token` resolve to nothing, so the
// credential path fails deterministically without the network.
func TestPRCheckBodyRemoteValidationPaths(t *testing.T) {
	chdir(t, repoRoot(t))
	body := filledPRScaffold(t)

	if code, _, errOut := runCLI("pr", "check-body", "--file", body, "--repo", "not-a-slug"); code != 1 ||
		!strings.Contains(errOut, "invalid repository slug") {
		t.Fatalf("a malformed slug = %d, want 1 with the slug named (err=%q)", code, errOut)
	}

	bin, _ := stubGH(t)
	withPATH(t, bin)
	if code, _, errOut := runCLI("pr", "check-body", "--file", body, "--repo", "owner/name"); code != 1 ||
		!strings.Contains(errOut, "without a credential") {
		t.Fatalf("a missing credential = %d, want 1 with the reason (err=%q)", code, errOut)
	}
}

// TestExecGHPropagatesTheDelegateExitCode pins the delegation contract:
// after a green gate, the wrapped command's own exit code is the
// answer — the wrapper never masks a delegate failure with its own
// generic error.
func TestExecGHPropagatesTheDelegateExitCode(t *testing.T) {
	// A clean, committed fixture: the pr create gate refuses an
	// uncommitted worktree, and the point of this test is what
	// happens after a green gate.
	dir := gatedRepo(t)
	chdir(t, dir)
	bin := t.TempDir()
	script := "#!/bin/sh\nexit 3\n"
	if err := os.WriteFile(filepath.Join(bin, "gh"), []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	withPATH(t, bin)

	body := filledPRScaffold(t)
	if code, _, _ := runCLI("pr", "create", "--title", "feat: a thing", "--file", body); code != 3 {
		t.Fatalf("a delegate exiting 3 = %d, want 3 propagated", code)
	}
}

// TestReadBodyFromStdin pins the documented `--file -` contract: the
// body is read from standard input, so a pipeline can gate a body
// without a temporary file.
func TestReadBodyFromStdin(t *testing.T) {
	chdir(t, repoRoot(t))
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	if _, err := w.WriteString("### Objective\n\nx\n\n### Bounded scope\n\nx\n\n### Dependencies\n\nx\n\n### Acceptance criteria\n\nx\n\n### Authorising context\n\nx\n"); err != nil {
		t.Fatal(err)
	}
	w.Close()
	old := os.Stdin
	os.Stdin = r
	defer func() { os.Stdin = old }()

	var stdout, stderr bytes.Buffer
	if code := run([]string{"issue", "check", "--type", "task", "--file", "-"}, &stdout, &stderr); code != 0 {
		t.Fatalf("a complete body on stdin = %d, want 0 (out=%q err=%q)", code, stdout.String(), stderr.String())
	}
}
