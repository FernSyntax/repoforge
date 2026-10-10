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
