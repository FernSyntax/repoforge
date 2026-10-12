// Copyright the repoforge contributors.
//
// SPDX-License-Identifier: Apache-2.0

package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func writeWorkflow(t *testing.T, dir, name, content string) {
	t.Helper()
	wfDir := filepath.Join(dir, ".github", "workflows")
	if err := os.MkdirAll(wfDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(wfDir, name), []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

func TestDiscoverCheckContextsOnlyPullRequest(t *testing.T) {
	dir := t.TempDir()
	writeWorkflow(t, dir, "pr-map.yml", "on:\n  push:\n    branches: [main]\n  pull_request:\njobs:\n  verify:\n    runs-on: ubuntu-latest\n    steps:\n      - run: true\n")
	writeWorkflow(t, dir, "pr-list.yml", "on: [pull_request]\njobs:\n  lint:\n    runs-on: ubuntu-latest\n    steps:\n      - run: true\n")
	writeWorkflow(t, dir, "pr-scalar.yml", "on: pull_request\njobs:\n  scalar:\n    runs-on: ubuntu-latest\n    steps:\n      - run: true\n")
	writeWorkflow(t, dir, "push-only.yml", "on:\n  push:\n    branches: [main]\njobs:\n  nightly:\n    runs-on: ubuntu-latest\n    steps:\n      - run: true\n")
	writeWorkflow(t, dir, "schedule-only.yml", "on:\n  schedule:\n    - cron: '0 6 * * 1'\njobs:\n  score:\n    runs-on: ubuntu-latest\n    steps:\n      - run: true\n")
	writeWorkflow(t, dir, "tags-only.yml", "on:\n  push:\n    tags: ['v*']\njobs:\n  release:\n    runs-on: ubuntu-latest\n    steps:\n      - run: true\n")
	writeWorkflow(t, dir, "target.yml", "on: [pull_request_target]\njobs:\n  dangerous:\n    runs-on: ubuntu-latest\n    steps:\n      - run: true\n")
	writeWorkflow(t, dir, "broken.yml", "on: [unclosed\n")

	got := DiscoverCheckContexts(dir)
	want := []string{"lint", "scalar", "verify"}
	if strings.Join(got, ",") != strings.Join(want, ",") {
		t.Errorf("contexts = %v, want %v (only pull_request jobs, sorted)", got, want)
	}
}

func TestDiscoverCheckContextsMissingDir(t *testing.T) {
	if got := DiscoverCheckContexts(t.TempDir()); len(got) != 0 {
		t.Errorf("contexts = %v, want empty for missing workflows dir", got)
	}
}

func TestDiscoverCheckContextsSkipsNonWorkflowEntries(t *testing.T) {
	dir := t.TempDir()
	wfDir := filepath.Join(dir, ".github", "workflows")
	if err := os.MkdirAll(filepath.Join(wfDir, "nested"), 0o755); err != nil {
		t.Fatal(err)
	}
	writeWorkflow(t, dir, "pr.yml", "on: [pull_request]\njobs:\n  verify:\n    runs-on: ubuntu-latest\n    steps:\n      - run: true\n")
	if err := os.WriteFile(filepath.Join(wfDir, "notes.md"), []byte("not a workflow\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	got := DiscoverCheckContexts(dir)
	if strings.Join(got, ",") != "verify" {
		t.Errorf("contexts = %v, want [verify] (directories and non-workflow files skipped)", got)
	}
}

func TestDiscoverCheckContextsUnreadableWorkflow(t *testing.T) {
	// An unreadable workflow file must be skipped rather than
	// crashing discovery: the contexts are a best-effort local
	// derivation, and one unreadable file must not block every
	// merge with a missing required check.
	dir := t.TempDir()
	if err := os.MkdirAll(filepath.Join(dir, ".github", "workflows"), 0o755); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(dir, ".github", "workflows", "pr.yml")
	if err := os.WriteFile(path, []byte("on: [pull_request]\njobs:\n  verify:\n    runs-on: ubuntu-latest\n"), 0o000); err != nil {
		t.Fatal(err)
	}
	if os.Geteuid() == 0 {
		t.Skip("root bypasses file permissions, so the unreadable branch is unreachable")
	}
	if got := DiscoverCheckContexts(dir); len(got) != 0 {
		t.Errorf("contexts = %v, want empty for an unreadable workflow", got)
	}
}

func TestCodeScanningCheckContexts(t *testing.T) {
	t.Run("go module and workflows", func(t *testing.T) {
		dir := t.TempDir()
		if err := os.WriteFile(filepath.Join(dir, "go.mod"), []byte("module x\n"), 0o644); err != nil {
			t.Fatal(err)
		}
		writeWorkflow(t, dir, "ci.yml", "on: [push]\njobs:\n  test:\n    runs-on: ubuntu-latest\n    steps:\n      - run: true\n")
		got := CodeScanningCheckContexts(dir)
		want := "Analyze (actions),Analyze (go)"
		if strings.Join(got, ",") != want {
			t.Errorf("contexts = %v, want %s (sorted)", got, want)
		}
	})
	t.Run("workflows without a module", func(t *testing.T) {
		dir := t.TempDir()
		writeWorkflow(t, dir, "ci.yaml", "on: [push]\njobs:\n  test:\n    runs-on: ubuntu-latest\n    steps:\n      - run: true\n")
		got := CodeScanningCheckContexts(dir)
		if strings.Join(got, ",") != "Analyze (actions)" {
			t.Errorf("contexts = %v, want [Analyze (actions)]", got)
		}
	})
	t.Run("neither", func(t *testing.T) {
		if got := CodeScanningCheckContexts(t.TempDir()); len(got) != 0 {
			t.Errorf("contexts = %v, want empty", got)
		}
	})
	t.Run("workflows dir exists but holds no workflow files", func(t *testing.T) {
		dir := t.TempDir()
		if err := os.MkdirAll(filepath.Join(dir, ".github", "workflows"), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(dir, ".github", "workflows", "README.md"), []byte("x\n"), 0o644); err != nil {
			t.Fatal(err)
		}
		if got := CodeScanningCheckContexts(dir); len(got) != 0 {
			t.Errorf("contexts = %v, want empty when no workflow files exist", got)
		}
	})
}
