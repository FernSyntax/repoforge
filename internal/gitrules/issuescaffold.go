package gitrules

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"gopkg.in/yaml.v3"
)

// IssueTypes are the issue forms an agent can scaffold. config.yml is
// the template chooser, not an issue type, so it is excluded: asking
// for it is a usage error, not a silent default.
var IssueTypes = []string{"bug", "feature", "task", "security"}

// IssueField is one required answer an issue form demands. Markdown
// items carry guidance, not answers, so they never appear here.
type IssueField struct {
	Label   string
	Kind    string
	Options []string
}

// issueForm mirrors the GitHub issue-form schema closely enough to
// extract required fields. Unknown item types are kept, not rejected:
// a form the scaffold does not understand fails loudly at scaffold
// time instead of emitting a body that can never validate.
type issueForm struct {
	Name   string   `yaml:"name"`
	Labels []string `yaml:"labels"`
	Body   []struct {
		Type  string `yaml:"type"`
		ID    string `yaml:"id"`
		Attrs struct {
			Label       string   `yaml:"label"`
			Description string   `yaml:"description"`
			Options     []string `yaml:"options"`
		} `yaml:"attributes"`
		Validations struct {
			Required bool `yaml:"required"`
		} `yaml:"validations"`
	} `yaml:"body"`
}

// IssueFormRoot walks upward from dir looking for the issue-template
// directory, so scaffolding works from any working directory.
func IssueFormRoot(dir string) (string, error) {
	abs, err := filepath.Abs(dir)
	if err != nil {
		return "", err
	}
	for {
		candidate := filepath.Join(abs, ".github", "ISSUE_TEMPLATE")
		if st, err := os.Stat(candidate); err == nil && st.IsDir() {
			return abs, nil
		}
		parent := filepath.Dir(abs)
		if parent == abs {
			return "", fmt.Errorf("no .github/ISSUE_TEMPLATE directory found walking up from %s", dir)
		}
		abs = parent
	}
}

// LoadIssueFields reads the required fields of an issue form. Every
// returned field must be answered; every answered field is checked.
func LoadIssueFields(root, typ string) (formName string, fields []IssueField, err error) {
	known := false
	for _, t := range IssueTypes {
		if typ == t {
			known = true
		}
	}
	if !known {
		return "", nil, fmt.Errorf("unknown issue type %q (want one of: %s)", typ, strings.Join(IssueTypes, ", "))
	}
	data, err := os.ReadFile(filepath.Join(root, ".github", "ISSUE_TEMPLATE", typ+".yml"))
	if err != nil {
		return "", nil, fmt.Errorf("reading the %q issue form: %w", typ, err)
	}
	var form issueForm
	if err := yaml.Unmarshal(data, &form); err != nil {
		return "", nil, fmt.Errorf("parsing the %q issue form: %w", typ, err)
	}
	for _, item := range form.Body {
		if item.Type == "markdown" {
			continue
		}
		if !item.Validations.Required {
			continue
		}
		if item.Attrs.Label == "" {
			return "", nil, fmt.Errorf("the %q issue form has a required item without a label (id %q)", typ, item.ID)
		}
		fields = append(fields, IssueField{Label: item.Attrs.Label, Kind: item.Type, Options: item.Attrs.Options})
	}
	if len(fields) == 0 {
		return "", nil, fmt.Errorf("the %q issue form declares no required fields", typ)
	}
	return form.Name, fields, nil
}

// IssueFormLabels returns the labels an issue form declares, so
// automation files under the same labels a human filing through the
// web form would get. An empty list means the form declares none.
func IssueFormLabels(root, typ string) ([]string, error) {
	data, err := os.ReadFile(filepath.Join(root, ".github", "ISSUE_TEMPLATE", typ+".yml"))
	if err != nil {
		return nil, fmt.Errorf("reading the %q issue form: %w", typ, err)
	}
	var form issueForm
	if err := yaml.Unmarshal(data, &form); err != nil {
		return nil, fmt.Errorf("parsing the %q issue form: %w", typ, err)
	}
	return form.Labels, nil
}

// RenderIssueScaffold prints the issue skeleton: one heading per
// required field with a fill prompt. Prompts are HTML comments, which
// validation strips, so an unfilled scaffold still fails — the
// scaffold guides, it never passes for you.
func RenderIssueScaffold(fields []IssueField) string {
	var b strings.Builder
	for _, f := range fields {
		fmt.Fprintf(&b, "### %s\n\n", f.Label)
		if len(f.Options) > 0 {
			fmt.Fprintf(&b, "<!-- Pick exactly one of the form's options: %s -->\n\n", strings.Join(f.Options, " / "))
		} else {
			fmt.Fprintf(&b, "<!-- Fill this in; an empty answer fails validation -->\n\n")
		}
	}
	return b.String()
}

// CheckIssueBody validates a drafted issue body against the form's
// required fields. Labels match case-insensitively; anything else is
// reported, never silently accepted.
func CheckIssueBody(fields []IssueField, body string) []Issue {
	var issues []Issue
	add := func(sev Severity, rule, format string, args ...any) {
		issues = append(issues, Issue{Rule: rule, Severity: sev, Message: fmt.Sprintf(format, args...)})
	}
	if strings.TrimSpace(body) == "" {
		add(Error, "issue/body", "the issue body is empty; fill every required field of the issue form")
		return issues
	}
	sections := sectionsOf(strings.TrimSpace(body))
	for _, f := range fields {
		content, ok := sections[strings.ToLower(f.Label)]
		if !ok {
			add(Error, "issue/field", "missing field %q; every required field of the issue form is required", f.Label)
			continue
		}
		if strings.TrimSpace(stripComments(content)) == "" {
			add(Error, "issue/field", "field %q is empty; state the answer", f.Label)
		}
	}
	return issues
}

// PRScaffold renders the pull request body skeleton: every heading
// the traceability checker requires, in order, with the issue link
// pre-placed in Why and a fill prompt under each heading. Prompts are
// HTML comments, which the checker strips, so handing this in unfilled
// fails exactly as an empty template would.
func PRScaffold(issue int) string {
	var b strings.Builder
	for i, section := range RequiredSections {
		fmt.Fprintf(&b, "## %s\n\n", titleCase(section))
		if section == "why" {
			fmt.Fprintf(&b, "Closes #%d\n\n", issue)
		}
		if i == 0 {
			b.WriteString("<!-- One coherent purpose; what changed and nothing else -->\n\n")
			continue
		}
		b.WriteString("<!-- Fill this in; \"none\" with a reason beats an empty section -->\n\n")
	}
	return b.String()
}
