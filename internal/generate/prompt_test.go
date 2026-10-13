package generate

import (
	"strings"
	"testing"
)

// TestAffirmativeAcceptsYesPrefixes proves y, ye, and yes approve
// (case-insensitive) so hurried input works.
func TestAffirmativeAcceptsYesPrefixes(t *testing.T) {
	for _, in := range []string{"y", "Y", "ye", "YE", "yes", "YES", "Yes"} {
		if !affirmative(strings.ToLower(strings.TrimSpace(in))) {
			t.Fatalf("input %q must approve", in)
		}
	}
}

// TestAffirmativeDeclinesAnythingElse proves anything else — empty,
// ambiguous, or doubled input — declines and fails safe.
func TestAffirmativeDeclinesAnythingElse(t *testing.T) {
	for _, in := range []string{"", "n", "no", "yes yes", "yeah", "ok", "yep"} {
		if affirmative(strings.ToLower(strings.TrimSpace(in))) {
			t.Fatalf("input %q must decline", in)
		}
	}
}

// TestConfirmDeclinesWithoutTerminal proves piped input never approves,
// even when it says yes.
func TestConfirmDeclinesWithoutTerminal(t *testing.T) {
	var out strings.Builder
	p := NewTerminalPrompter(strings.NewReader("yes\n"), &out)
	ok, err := p.Confirm("approve? ")
	if err != nil || ok {
		t.Fatal("non-terminal input must decline")
	}
}
