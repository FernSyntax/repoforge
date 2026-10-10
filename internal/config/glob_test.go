package config

import "testing"

func TestMatchPattern(t *testing.T) {
	cases := []struct {
		pattern, name string
		want          bool
	}{
		{"LICENSE", "LICENSE", true},
		{"LICENSE", "docs/LICENSE", false},
		{".github/workflows/*.yml", ".github/workflows/ci.yml", true},
		{".github/workflows/*.yml", ".github/workflows/nested/ci.yml", false},
		{"template/**", "template/README.md", true},
		{"template/**", "template/docs/development/README.md", true},
		{"template/**", "template", false},
		{"template/**", "templates/x", false},
		{"docs/**/README.md", "docs/development/README.md", true},
		{"docs/**/README.md", "docs/README.md", true},
		{"scripts/verify", "scripts/verify", true},
		{"*.md", "README.md", true},
		{"*.md", "docs/README.md", false},
		{"src/?/main.c", "src/a/main.c", true},
		{"[a-z]ile", "file", true},
	}
	for _, c := range cases {
		got, err := MatchPattern(c.pattern, c.name)
		if err != nil {
			t.Errorf("MatchPattern(%q, %q): %v", c.pattern, c.name, err)
			continue
		}
		if got != c.want {
			t.Errorf("MatchPattern(%q, %q) = %v, want %v", c.pattern, c.name, got, c.want)
		}
	}
}

func TestValidatePattern(t *testing.T) {
	if err := ValidatePattern("docs/**"); err != nil {
		t.Errorf("valid pattern rejected: %v", err)
	}
	if err := ValidatePattern("bad["); err == nil {
		t.Error("invalid pattern accepted")
	}
	if err := ValidatePattern(""); err == nil {
		t.Error("empty pattern accepted")
	}
}
