package gitrules

import (
	"path/filepath"
	"strings"
	"testing"
)

// TestPRScaffoldCoversEveryRequiredSection proves the scaffold and the
// checker agree by construction: every heading the checker requires
// appears in the scaffold, with the issue link pre-placed in Why.
func TestPRScaffoldCoversEveryRequiredSection(t *testing.T) {
	out := PRScaffold(7)
	sections := sectionsOf(out)
	for _, want := range RequiredSections {
		if _, ok := sections[want]; !ok {
			t.Errorf("scaffold is missing section %q", want)
		}
	}
	if !strings.Contains(out, "Closes #7") {
		t.Errorf("scaffold does not pre-place the issue link:\n%s", out)
	}
}

// TestPRScaffoldAloneDoesNotValidate proves the scaffold guides but
// never passes for you: prompts are HTML comments, which the checker
// strips, so an unfilled scaffold fails exactly as an empty template
// would.
func TestPRScaffoldAloneDoesNotValidate(t *testing.T) {
	if issues := CheckPRBody(PRScaffold(7)); !Failed(issues) {
		t.Errorf("an unfilled scaffold must fail validation")
	}
}

// TestPRScaffoldFilledValidates proves the intended loop terminates:
// a scaffold with every prompt answered passes.
func TestPRScaffoldFilledValidates(t *testing.T) {
	var lines []string
	for _, line := range strings.Split(PRScaffold(7), "\n") {
		if strings.HasPrefix(strings.TrimSpace(line), "<!--") {
			lines = append(lines, "answered")
			continue
		}
		lines = append(lines, line)
	}
	if issues := CheckPRBody(strings.Join(lines, "\n")); Failed(issues) {
		t.Errorf("a filled scaffold must pass, got: %s", joinMessages(issues))
	}
}

// repoFormsRoot is the engine checkout holding the real issue forms.
func repoFormsRoot(t *testing.T) string {
	t.Helper()
	abs, err := filepath.Abs("../..")
	if err != nil {
		t.Fatal(err)
	}
	root, err := IssueFormRoot(abs)
	if err != nil {
		t.Fatal(err)
	}
	return root
}

// TestLoadIssueFieldsReadsTheRealBugForm proves the loader tracks the
// form on disk: every required field the bug form declares is
// returned, in order, with the dropdown options attached.
func TestLoadIssueFieldsReadsTheRealBugForm(t *testing.T) {
	root := repoFormsRoot(t)
	name, fields, err := LoadIssueFields(root, "bug")
	if err != nil {
		t.Fatal(err)
	}
	if name == "" {
		t.Errorf("the bug form has no name")
	}
	var labels []string
	for _, f := range fields {
		labels = append(labels, f.Label)
	}
	for _, want := range []string{"What happened?", "Expected behaviour", "Steps to reproduce", "Severity", "Affected requirements"} {
		found := false
		for _, l := range labels {
			if l == want {
				found = true
			}
		}
		if !found {
			t.Errorf("the bug form's required field %q was not loaded (got %q)", want, labels)
		}
	}
}

// TestLoadIssueFieldsRejectsUnknownTypes proves asking for the
// template chooser (or anything else) is a usage error, not a silent
// default.
func TestLoadIssueFieldsRejectsUnknownTypes(t *testing.T) {
	root := repoFormsRoot(t)
	for _, typ := range []string{"config", "question", ""} {
		if _, _, err := LoadIssueFields(root, typ); err == nil {
			t.Errorf("issue type %q must be rejected", typ)
		}
	}
}

// TestIssueScaffoldRoundtrip proves the intended loop: the scaffold
// for the real bug form fails unfilled and passes once every field
// is answered.
func TestIssueScaffoldRoundtrip(t *testing.T) {
	root := repoFormsRoot(t)
	_, fields, err := LoadIssueFields(root, "bug")
	if err != nil {
		t.Fatal(err)
	}
	scaffold := RenderIssueScaffold(fields)
	if issues := CheckIssueBody(fields, scaffold); !Failed(issues) {
		t.Errorf("an unfilled issue scaffold must fail validation")
	}
	var b strings.Builder
	for _, f := range fields {
		b.WriteString("### " + f.Label + "\n\nanswered\n\n")
	}
	if issues := CheckIssueBody(fields, b.String()); Failed(issues) {
		t.Errorf("a filled issue body must pass, got: %s", joinMessages(issues))
	}
}

// TestCheckIssueBodyNamesTheMissingField proves a half-filled body
// fails with the field named, so the author knows exactly what to fix.
func TestCheckIssueBodyNamesTheMissingField(t *testing.T) {
	root := repoFormsRoot(t)
	_, fields, err := LoadIssueFields(root, "task")
	if err != nil {
		t.Fatal(err)
	}
	body := "### " + fields[0].Label + "\n\nanswered\n"
	issues := CheckIssueBody(fields, body)
	if !Failed(issues) {
		t.Fatalf("a body missing required fields must fail")
	}
	if !strings.Contains(joinMessages(issues), fields[1].Label) {
		t.Errorf("the failure does not name the missing field %q: %s", fields[1].Label, joinMessages(issues))
	}
}

// TestIssueFormLabelsTracksTheForm proves automation files under the
// same labels a web-filed issue would get.
func TestIssueFormLabelsTracksTheForm(t *testing.T) {
	root := repoFormsRoot(t)
	labels, err := IssueFormLabels(root, "bug")
	if err != nil {
		t.Fatal(err)
	}
	found := false
	for _, l := range labels {
		if l == "bug" {
			found = true
		}
	}
	if !found {
		t.Errorf("the bug form's label was not loaded (got %q)", labels)
	}
	if _, err := IssueFormLabels(root, "nope"); err == nil {
		t.Errorf("labels for an unknown type must fail")
	}
}
