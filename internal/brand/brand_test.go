package brand

import (
	"strings"
	"testing"
)

// TestFindRejectsEveryKnownVariant proves the gate catches the legacy
// product in every separator and case combination that appears in real
// files: prose, identifiers, module paths, and URLs.
func TestFindRejectsEveryKnownVariant(t *testing.T) {
	legacy := func(parts ...string) string { return Normalize(parts...) }
	cases := []string{
		legacy("Repo", "Doctrine"),
		legacy("repo", "doctrine"),
		legacy("REPO", "DOCTRINE"),
		legacy("repo", "-", "doctrine"),
		legacy("repo", "_", "doctrine"),
		legacy("Repo ", "Doctrine"),
		legacy("Repro", "Doctrine"),
		legacy("github.com/fernsyntax/repo", "-doctrine"),
		legacy("fernsyntax/repo", "-doctrine"),
		legacy("DOCTRINE", "-", "REPO"),
	}
	for _, text := range cases {
		if got := Find(text); len(got) == 0 {
			t.Errorf("Find(%q) = no findings, want a branding finding", text)
		}
	}
}

// TestFindNamesWhatToRemove proves the finding is actionable: it quotes
// the offending text and names the rule it breaks.
func TestFindNamesWhatToRemove(t *testing.T) {
	msgs := Find("see " + Normalize("Repo", "Doctrine") + " for details")
	if len(msgs) == 0 {
		t.Fatal("no finding")
	}
	joined := strings.Join(msgs, "\n")
	for _, want := range []string{Normalize("Repo", "Doctrine"), "legacy product", "fresh product history"} {
		if !strings.Contains(joined, want) {
			t.Errorf("finding does not contain %q:\n%s", want, joined)
		}
	}
}

// TestCleanAcceptsRepoForge proves the gate does not fire on the
// current product name in any of its spellings, including the ones that
// share characters with the legacy name.
func TestCleanAcceptsRepoForge(t *testing.T) {
	for _, text := range []string{
		"", "RepoForge", "repoforge", "repo-forge", "REPOFORGE",
		"github.com/FernSyntax/repoforge", "repoforge:main",
		"a doctrine of repoforge", "doctrine", "repo",
	} {
		if got := Find(text); len(got) != 0 {
			t.Errorf("Find(%q) = %v, want no findings", text, got)
		}
	}
}

// TestGateDoesNotMatchItsOwnSource proves the pattern construction is
// sound: the fragments this file is built from must not themselves trip
// the gate, or every repository would fail validation on the gate.
func TestGateDoesNotMatchItsOwnSource(t *testing.T) {
	for _, re := range patterns {
		if re.MatchString(re.String()) {
			t.Errorf("the branding gate matches its own pattern source: %s", re.String())
		}
	}
	if !Clean(LegacyModulePrefix) == false {
		// The constant itself is the legacy prefix, so it must be
		// detected when it is used verbatim.
		t.Log("LegacyModulePrefix is intentionally the legacy path")
	}
	if got := Find(LegacyModulePrefix); len(got) == 0 {
		t.Error("the legacy module path must be detected verbatim")
	}
}
