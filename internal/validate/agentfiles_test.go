package validate

import (
	"os"
	"path/filepath"
	"testing"
)

// TestAgentFilesPassOnTheRealRepo proves the committed framework
// links to its normative sources: with the agent files tracked, the
// check is silent.
func TestAgentFilesPassOnTheRealRepo(t *testing.T) {
	root := repoRoot(t)
	tracked, err := trackedFiles(root)
	if err != nil {
		t.Fatal(err)
	}
	trackedSet := make(map[string]bool, len(tracked))
	for _, f := range tracked {
		trackedSet[f] = true
	}
	if findings := checkAgentFiles(root, trackedSet); len(findings) != 0 {
		for _, f := range findings {
			t.Errorf("%s %s: %s", f.Rule, f.Path, f.Message)
		}
	}
}

// TestAgentFilesNamesTheMissing produces a missing finding, not
// silence, when a framework file is absent from version control. The
// tracked set carries the decision index, which is what scopes the
// check to decision-practicing repositories.
func TestAgentFilesNamesTheMissing(t *testing.T) {
	findings := checkAgentFiles(t.TempDir(), map[string]bool{decisionIndex: true})
	if len(findings) != len(agentFileRefs) {
		t.Fatalf("want one finding per agent file (%d), got %d", len(agentFileRefs), len(findings))
	}
	for _, f := range findings {
		if f.Rule != "agents/missing" {
			t.Errorf("want agents/missing, got %s for %s", f.Rule, f.Path)
		}
	}
}

// TestAgentFilesNamesTheBrokenLink proves a file that stops linking
// to its sources fails with the reference named.
func TestAgentFilesNamesTheBrokenLink(t *testing.T) {
	dir := t.TempDir()
	for name := range agentFileRefs {
		p := filepath.Join(dir, filepath.FromSlash(name))
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte("# notes\n\nno links here\n"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	trackedSet := map[string]bool{decisionIndex: true}
	for name := range agentFileRefs {
		trackedSet[name] = true
	}
	findings := checkAgentFiles(dir, trackedSet)
	if len(findings) == 0 {
		t.Fatalf("unlinked agent files must fail")
	}
	for _, f := range findings {
		if f.Rule != "agents/reference" {
			t.Errorf("want agents/reference, got %s for %s", f.Rule, f.Path)
		}
	}
}
