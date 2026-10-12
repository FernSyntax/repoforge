// Copyright the repoforge contributors.
//
// SPDX-License-Identifier: Apache-2.0

package gitrules

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// writeIssueForm lays down a minimal issue-form fixture in root so a test
// can exercise exactly one branch of the form parser.
func writeIssueForm(t *testing.T, root, typ, body string) {
	t.Helper()
	dir := filepath.Join(root, ".github", "ISSUE_TEMPLATE")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, typ+".yml"), []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
}

// TestIssueFormRootWalksUpAndFailsLoudly pins the discovery contract:
// scaffolding works from any subdirectory of the repository, and a
// repository with no issue-form directory is reported rather than
// silently defaulting to a form that does not exist.
func TestIssueFormRootWalksUpAndFailsLoudly(t *testing.T) {
	t.Run("found by walking up", func(t *testing.T) {
		root := t.TempDir()
		deep := filepath.Join(root, "internal", "gitrules")
		if err := os.MkdirAll(deep, 0o755); err != nil {
			t.Fatal(err)
		}
		writeIssueForm(t, root, "bug", "name: Bug\n")
		got, err := IssueFormRoot(deep)
		if err != nil {
			t.Fatalf("IssueFormRoot: %v", err)
		}
		if got != root {
			t.Errorf("IssueFormRoot(%q) = %q, want %q", deep, got, root)
		}
	})

	t.Run("missing is reported", func(t *testing.T) {
		// A directory tree with no .github/ISSUE_TEMPLATE anywhere above
		// it must fail rather than return the first plausible ancestor.
		_, err := IssueFormRoot(t.TempDir())
		if err == nil {
			t.Fatal("IssueFormRoot must fail when no issue-form directory exists")
		}
		if !strings.Contains(err.Error(), "ISSUE_TEMPLATE") {
			t.Errorf("err = %v, want it to name ISSUE_TEMPLATE", err)
		}
	})
}

// TestLoadIssueFieldsRejectsEveryMalformedForm pins the parser's failure
// modes. Each one is a scaffold-time error on purpose: a form the
// scaffold does not understand must fail loudly rather than emit a body
// that can never validate.
func TestLoadIssueFieldsRejectsEveryMalformedForm(t *testing.T) {
	tests := []struct {
		name string
		body string
		want string
	}{
		{
			name: "unparseable YAML",
			body: "name: [unclosed\n",
			want: "parsing the \"bug\" issue form",
		},
		{
			name: "a required item without a label",
			body: "name: Bug\nbody:\n  - type: textarea\n    id: repro\n    validations:\n      required: true\n",
			want: "required item without a label",
		},
		{
			name: "no required fields at all",
			body: "name: Bug\nbody:\n  - type: markdown\n    attributes:\n      value: guidance\n",
			want: "declares no required fields",
		},
		{
			name: "an optional item is not required",
			body: "name: Bug\nbody:\n  - type: textarea\n    id: repro\n    attributes:\n      label: Repro\n    validations:\n      required: false\n",
			want: "declares no required fields",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			root := t.TempDir()
			writeIssueForm(t, root, "bug", tt.body)
			_, _, err := LoadIssueFields(root, "bug")
			if err == nil {
				t.Fatal("LoadIssueFields must reject a malformed form")
			}
			if !strings.Contains(err.Error(), tt.want) {
				t.Errorf("err = %v, want it to mention %q", err, tt.want)
			}
		})
	}
}

// TestLoadIssueFieldsRejectsAnUnknownType proves config.yml is not an
// issue type: asking for it is a usage error, not a silent default, so
// the caller gets the list of real types back.
func TestLoadIssueFieldsRejectsAnUnknownType(t *testing.T) {
	root := t.TempDir()
	_, _, err := LoadIssueFields(root, "config")
	if err == nil {
		t.Fatal("an unknown issue type must be refused")
	}
	for _, want := range IssueTypes {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("err = %v, want it to list the real type %q", err, want)
		}
	}
}

// TestLoadIssueFieldsReportsAnUnreadableForm proves a missing form file is
// reported as a read failure naming the type, not as a generic error the
// operator cannot act on.
func TestLoadIssueFieldsReportsAnUnreadableForm(t *testing.T) {
	_, _, err := LoadIssueFields(t.TempDir(), "bug")
	if err == nil {
		t.Fatal("a missing issue form must be reported")
	}
	if !strings.Contains(err.Error(), `"bug"`) {
		t.Errorf("err = %v, want it to name the requested type", err)
	}
}

// TestLoadIssueFieldsKeepsEveryRequiredFieldAndItsOptions pins the shape a
// scaffold needs: one entry per required item, carrying the label, the
// item type and the declared options, with markdown guidance excluded
// because it carries no answer.
func TestLoadIssueFieldsKeepsEveryRequiredFieldAndItsOptions(t *testing.T) {
	root := t.TempDir()
	writeIssueForm(t, root, "bug", `name: Bug report
labels: [bug]
body:
  - type: markdown
    attributes:
      value: Thanks for filing a bug
  - type: dropdown
    id: severity
    attributes:
      label: Severity
      options: [low, high]
    validations:
      required: true
  - type: textarea
    id: repro
    attributes:
      label: Reproduction steps
    validations:
      required: true
`)
	name, fields, err := LoadIssueFields(root, "bug")
	if err != nil {
		t.Fatalf("LoadIssueFields: %v", err)
	}
	if name != "Bug report" {
		t.Errorf("form name = %q, want %q", name, "Bug report")
	}
	if len(fields) != 2 {
		t.Fatalf("fields = %+v, want 2 (markdown guidance excluded)", fields)
	}
	if fields[0].Label != "Severity" || fields[0].Kind != "dropdown" {
		t.Errorf("fields[0] = %+v, want the dropdown", fields[0])
	}
	if strings.Join(fields[0].Options, ",") != "low,high" {
		t.Errorf("fields[0].Options = %v, want [low high]", fields[0].Options)
	}
	if fields[1].Label != "Reproduction steps" || fields[1].Kind != "textarea" {
		t.Errorf("fields[1] = %+v, want the textarea", fields[1])
	}
	if len(fields[1].Options) != 0 {
		t.Errorf("fields[1].Options = %v, want none", fields[1].Options)
	}
}

// TestIssueFormLabelsReportsAnUnparseableForm proves the labels helper
// fails on the same malformed input the field loader refuses, so
// automation cannot pick up a label list from a form that will never
// validate.
func TestIssueFormLabelsReportsAnUnparseableForm(t *testing.T) {
	root := t.TempDir()
	writeIssueForm(t, root, "bug", "labels: [unclosed\n")
	if _, err := IssueFormLabels(root, "bug"); err == nil {
		t.Fatal("IssueFormLabels must reject an unparseable form")
	}
}

// TestIssueFormLabelsReportsAnUnreadableForm proves a missing form is a
// read failure naming the type, matching the field loader's behavior.
func TestIssueFormLabelsReportsAnUnreadableForm(t *testing.T) {
	if _, err := IssueFormLabels(t.TempDir(), "bug"); err == nil {
		t.Fatal("a missing issue form must be reported")
	}
}

// TestIssueFormLabelsReturnsAnEmptyListForAFormWithoutLabels pins the
// documented meaning of an empty result: the form declares no labels, so
// automation adds none. It is not an error.
func TestIssueFormLabelsReturnsAnEmptyListForAFormWithoutLabels(t *testing.T) {
	root := t.TempDir()
	writeIssueForm(t, root, "bug", "name: Bug\n")
	labels, err := IssueFormLabels(root, "bug")
	if err != nil {
		t.Fatalf("IssueFormLabels: %v", err)
	}
	if len(labels) != 0 {
		t.Errorf("labels = %v, want none", labels)
	}
}

// TestRenderIssueScaffoldGuidesEveryFieldShape pins the two prompt shapes
// the scaffold emits. A field with options must advertise them so the
// author picks a valid one; a free-text field must say an empty answer
// fails, because the scaffold guides and never passes for you.
func TestRenderIssueScaffoldGuidesEveryFieldShape(t *testing.T) {
	out := RenderIssueScaffold([]IssueField{
		{Label: "Severity", Kind: "dropdown", Options: []string{"low", "high"}},
		{Label: "Reproduction steps", Kind: "textarea"},
	})

	if !strings.Contains(out, "### Severity\n") || !strings.Contains(out, "### Reproduction steps\n") {
		t.Errorf("scaffold is missing a field heading:\n%s", out)
	}
	if !strings.Contains(out, "Pick exactly one of the form's options: low / high") {
		t.Errorf("a field with options must advertise them:\n%s", out)
	}
	if !strings.Contains(out, "Fill this in; an empty answer fails validation") {
		t.Errorf("a free-text field must state that an empty answer fails:\n%s", out)
	}
	if got := RenderIssueScaffold(nil); got != "" {
		t.Errorf("RenderIssueScaffold(nil) = %q, want empty", got)
	}
}

// TestCheckIssueBodyRejectsAnEmptyBody pins the first gate: an empty body
// is one finding, not one per missing field, so the author is told to fill
// the form rather than handed a list of every heading.
func TestCheckIssueBodyRejectsAnEmptyBody(t *testing.T) {
	for _, body := range []string{"", "   \n\t\n"} {
		issues := CheckIssueBody([]IssueField{{Label: "Severity"}}, body)
		if len(issues) != 1 {
			t.Fatalf("CheckIssueBody(%q) = %+v, want exactly one issue", body, issues)
		}
		if issues[0].Rule != "issue/body" {
			t.Errorf("rule = %q, want issue/body", issues[0].Rule)
		}
	}
}

// TestCheckIssueBodyMatchesLabelsCaseInsensitively pins the matching rule:
// the form's label is the contract, but an author who types different
// capitalisation has still answered, so refusing it would be a false
// failure.
func TestCheckIssueBodyMatchesLabelsCaseInsensitively(t *testing.T) {
	fields := []IssueField{{Label: "Reproduction Steps"}}
	body := "### reproduction steps\n\nSteps here.\n"
	if issues := CheckIssueBody(fields, body); Failed(issues) {
		t.Errorf("a case-insensitively matching heading must pass: %s", joinMessages(issues))
	}
}

// TestCheckIssueBodyTreatsACommentOnlyAnswerAsEmpty pins the reason the
// scaffold's prompts are HTML comments: they are stripped before the
// emptiness check, so an unfilled scaffold fails exactly as an empty
// template would.
func TestCheckIssueBodyTreatsACommentOnlyAnswerAsEmpty(t *testing.T) {
	fields := []IssueField{{Label: "Severity"}}
	body := "### Severity\n\n<!-- Fill this in; an empty answer fails validation -->\n"
	issues := CheckIssueBody(fields, body)
	if !Failed(issues) {
		t.Error("a comment-only answer must fail validation")
	}
	if len(issues) != 1 || !strings.Contains(issues[0].Message, "is empty") {
		t.Errorf("issues = %+v, want one 'is empty' finding", issues)
	}
}

// TestCheckIssueBodyNamesEveryMissingField proves the report is actionable
// when more than one field is absent: each missing field is named so the
// author can fill them all in one pass.
func TestCheckIssueBodyNamesEveryMissingField(t *testing.T) {
	fields := []IssueField{
		{Label: "Severity"},
		{Label: "Reproduction Steps"},
	}
	issues := CheckIssueBody(fields, "### Severity\n\nhigh\n")
	if len(issues) != 1 {
		t.Fatalf("issues = %+v, want exactly one", issues)
	}
	if !strings.Contains(issues[0].Message, "Reproduction Steps") {
		t.Errorf("message = %q, want it to name the missing field", issues[0].Message)
	}
	if issues[0].Rule != "issue/field" {
		t.Errorf("rule = %q, want issue/field", issues[0].Rule)
	}
}
