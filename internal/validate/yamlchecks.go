// Copyright the repoforge contributors.
//
// SPDX-License-Identifier: Apache-2.0

package validate

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	"gopkg.in/yaml.v3"

	"github.com/FernSyntax/repoforge/internal/config"
)

var issueFormTypes = map[string]bool{
	"markdown": true, "textarea": true, "input": true,
	"dropdown": true, "checkboxes": true,
}

// checkGitHubDir validates YAML/JSON configuration under .github/:
// issue forms, the issue-template config, workflows (including action
// pinning), and any other YAML/JSON for syntax.
func checkGitHubDir(root string, tracked []string, pol *config.Policy) []Finding {
	var out []Finding
	for _, f := range tracked {
		if !strings.HasPrefix(f, ".github/") {
			continue
		}
		ext := strings.ToLower(filepath.Ext(f))
		if ext != ".yml" && ext != ".yaml" && ext != ".json" {
			continue
		}
		data, err := os.ReadFile(filepath.Join(root, f))
		if err != nil {
			out = append(out, Finding{Rule: "yaml/github-config", Path: f, Message: fmt.Sprintf("cannot read file: %v", err)})
			continue
		}
		switch dir := filepath.Dir(f); {
		case dir == ".github/ISSUE_TEMPLATE" && ext != ".json":
			out = append(out, checkIssueTemplateFile(f, data)...)
		case dir == ".github/workflows" && ext != ".json":
			out = append(out, checkWorkflowFile(f, data, pol)...)
		default:
			out = append(out, checkSyntaxOnly(f, data, ext == ".json")...)
		}
	}
	return out
}

func checkSyntaxOnly(path string, data []byte, isJSON bool) []Finding {
	var v any
	var err error
	if isJSON {
		err = json.Unmarshal(data, &v)
	} else {
		err = yaml.Unmarshal(data, &v)
	}
	if err != nil {
		return []Finding{{Rule: "yaml/github-config", Path: path, Message: fmt.Sprintf("invalid syntax: %v", err)}}
	}
	return nil
}

type issueTemplate struct {
	Name               string           `yaml:"name"`
	Description        string           `yaml:"description"`
	Title              *string          `yaml:"title"`
	Body               []map[string]any `yaml:"body"`
	BlankIssuesEnabled *bool            `yaml:"blank_issues_enabled"`
	ContactLinks       []struct {
		Name  string `yaml:"name"`
		URL   string `yaml:"url"`
		About string `yaml:"about"`
	} `yaml:"contact_links"`
}

func checkIssueTemplateFile(path string, data []byte) []Finding {
	if filepath.Base(path) == "config.yml" || filepath.Base(path) == "config.yaml" {
		return checkIssueTemplateConfig(path, data)
	}
	var form issueTemplate
	if err := yaml.Unmarshal(data, &form); err != nil {
		return []Finding{{Rule: "yaml/issue-template", Path: path, Message: fmt.Sprintf("invalid YAML: %v", err)}}
	}
	var out []Finding
	if form.Name == "" {
		out = append(out, Finding{Rule: "yaml/issue-template", Path: path, Message: "missing required key: name"})
	}
	if form.Description == "" {
		out = append(out, Finding{Rule: "yaml/issue-template", Path: path, Message: "missing required key: description"})
	}
	// An empty title is rejected by GitHub ("must be of type String
	// and cannot be empty"); omit the key for no prefilled title.
	if form.Title != nil && *form.Title == "" {
		out = append(out, Finding{Rule: "yaml/issue-template", Path: path, Message: "title must not be empty; omit it for no prefilled title"})
	}
	if len(form.Body) == 0 {
		out = append(out, Finding{Rule: "yaml/issue-template", Path: path, Message: "missing required key: body (must list at least one element)"})
	}
	for i, item := range form.Body {
		t, _ := item["type"].(string)
		if !issueFormTypes[t] {
			out = append(out, Finding{Rule: "yaml/issue-template", Path: path, Message: fmt.Sprintf("body element %d has missing or unknown type %q", i, t)})
		}
	}
	return out
}

func checkIssueTemplateConfig(path string, data []byte) []Finding {
	var cfg issueTemplate
	if err := yaml.Unmarshal(data, &cfg); err != nil {
		return []Finding{{Rule: "yaml/issue-template", Path: path, Message: fmt.Sprintf("invalid YAML: %v", err)}}
	}
	var out []Finding
	if cfg.BlankIssuesEnabled == nil {
		out = append(out, Finding{Rule: "yaml/issue-template", Path: path, Message: "missing required key: blank_issues_enabled"})
	}
	for i, link := range cfg.ContactLinks {
		if link.Name == "" || link.URL == "" || link.About == "" {
			out = append(out, Finding{Rule: "yaml/issue-template", Path: path, Message: fmt.Sprintf("contact_links[%d] requires name, url, and about", i)})
		}
	}
	return out
}

type workflowDoc struct {
	On   any `yaml:"on"`
	Jobs map[string]struct {
		Steps []struct {
			Uses string `yaml:"uses"`
		} `yaml:"steps"`
	} `yaml:"jobs"`
}

var shaPinRe = regexp.MustCompile(`^[0-9a-f]{40}$`)

func checkWorkflowFile(path string, data []byte, pol *config.Policy) []Finding {
	var wf workflowDoc
	if err := yaml.Unmarshal(data, &wf); err != nil {
		return []Finding{{Rule: "yaml/workflow", Path: path, Message: fmt.Sprintf("invalid YAML: %v", err)}}
	}
	var out []Finding
	if wf.On == nil {
		out = append(out, Finding{Rule: "yaml/workflow", Path: path, Message: "missing required key: on"})
	}
	if len(wf.Jobs) == 0 {
		out = append(out, Finding{Rule: "yaml/workflow", Path: path, Message: "missing required key: jobs (must define at least one job)"})
	}
	pinning := "sha"
	if pol != nil {
		pinning = pol.ActionPinningMode()
	}
	if pinning == "sha" {
		for jobName, job := range wf.Jobs {
			for _, step := range job.Steps {
				if msg := checkActionPin(step.Uses); msg != "" {
					out = append(out, Finding{Rule: "yaml/workflow", Path: path, Message: fmt.Sprintf("job %q: %s", jobName, msg)})
				}
			}
		}
	}
	return out
}

// checkActionPin enforces full-length commit SHA pinning (spec.md §8.6) for
// a single "uses" value. Local (./) and docker:// actions are exempt.
func checkActionPin(uses string) string {
	if uses == "" || strings.HasPrefix(uses, "./") || strings.HasPrefix(uses, "docker://") {
		return ""
	}
	at := strings.LastIndex(uses, "@")
	if at < 0 {
		return fmt.Sprintf("action %q is unpinned; pin to a full-length commit SHA", uses)
	}
	if !shaPinRe.MatchString(uses[at+1:]) {
		return fmt.Sprintf("action %q is not pinned to a full-length commit SHA", uses)
	}
	return ""
}
