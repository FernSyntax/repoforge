package config

import (
	"os"
	"path/filepath"
	"sort"
	"strings"

	"gopkg.in/yaml.v3"
)

// DiscoverCheckContexts derives required status check contexts from a
// repository's local workflows (deterministic, sorted): the job keys
// of every workflow that runs on pull_request events. Jobs in
// workflows that never run on pull requests (scheduled, tag-only,
// manual) can never report a check on a PR, so requiring them would
// block every merge forever. Empty when no workflows qualify.
func DiscoverCheckContexts(root string) []string {
	wfDir := filepath.Join(root, ".github", "workflows")
	entries, err := os.ReadDir(wfDir)
	if err != nil {
		return nil
	}
	var contexts []string
	for _, e := range entries {
		name := e.Name()
		if e.IsDir() || !(strings.HasSuffix(name, ".yml") || strings.HasSuffix(name, ".yaml")) {
			continue
		}
		data, err := os.ReadFile(filepath.Join(wfDir, name))
		if err != nil {
			continue
		}
		var wf struct {
			On   any            `yaml:"on"`
			Jobs map[string]any `yaml:"jobs"`
		}
		if err := yaml.Unmarshal(data, &wf); err != nil {
			continue
		}
		if !runsOnPullRequest(wf.On) {
			continue
		}
		for job := range wf.Jobs {
			contexts = append(contexts, job)
		}
	}
	sort.Strings(contexts)
	return contexts
}

// runsOnPullRequest reports whether a workflow trigger includes the
// pull_request event, in any of its YAML shapes (scalar, list, or
// mapping). pull_request_target is deliberately not counted: it is a
// different trigger, and the baseline forbids it.
func runsOnPullRequest(on any) bool {
	switch t := on.(type) {
	case string:
		return t == "pull_request"
	case []any:
		for _, e := range t {
			if s, ok := e.(string); ok && s == "pull_request" {
				return true
			}
		}
	case map[string]any:
		for k := range t {
			if k == "pull_request" {
				return true
			}
		}
	}
	return false
}

// CodeScanningCheckContexts derives the status check contexts that
// CodeQL default setup emits for this repository, deterministically and
// locally: a Go module ("Analyze (go)") and GitHub Actions workflow
// files ("Analyze (actions)", which default setup analyzes for any
// repository carrying workflows).
//
// These contexts are only useful when the policy requires code
// scanning; requiring them otherwise would block every merge with a
// check that never reports. Callers therefore gate this function on the
// contract's security.code_scanning setting.
//
// Detection is deliberately local, matching DiscoverCheckContexts: a
// repository that adds another CodeQL language must add its context
// here (docs/reference/limitations.md).
func CodeScanningCheckContexts(root string) []string {
	var out []string
	if info, err := os.Stat(filepath.Join(root, "go.mod")); err == nil && !info.IsDir() {
		out = append(out, "Analyze (go)")
	}
	if entries, err := os.ReadDir(filepath.Join(root, ".github", "workflows")); err == nil {
		for _, e := range entries {
			name := e.Name()
			if e.IsDir() || !(strings.HasSuffix(name, ".yml") || strings.HasSuffix(name, ".yaml")) {
				continue
			}
			out = append(out, "Analyze (actions)")
			break
		}
	}
	sort.Strings(out)
	return out
}
