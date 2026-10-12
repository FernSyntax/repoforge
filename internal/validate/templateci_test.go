// Copyright the repoforge contributors.
//
// SPDX-License-Identifier: Apache-2.0

package validate

import (
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

// templateCIWorkflowFile is the generated template's CI.
const templateCIWorkflowFile = "template/.github/workflows/ci.yml"

// templateCLIActionFile is the composite action that builds the CLI.
const templateCLIActionFile = "template/.github/actions/cli/action.yml"

// TestTemplateCIBuildsTheCLIFromTheRecordedSource is the regression test
// for the first publication that produced a red template.
//
// The template's CI used to install its CLI with
//
//	go install github.com/FernSyntax/repoforge/cmd/repoforge@v0.1.0
//
// which resolves through the Go module proxy, and the proxy caches by tag
// STRING. A tag reused across repository recreations is served from
// whatever was cached first, so the validating CLI was an older build
// than the repository it was validating. The observed failure was two
// jobs failing for two different reasons at once:
//
//	template validate: .github/dependabot.yml: required template file missing
//	repoforge.manifest.json: [manifest/invalid] json: unknown field "adopted"
//
// Both were the CLI being stale, not the tree being wrong.
//
// The fix reads the engine source commit out of this repository's own
// manifest and builds the CLI from that commit, so the validator is by
// construction the version that produced the tree. Proven locally: the
// generated template passes both gated jobs with the source-built CLI and
// fails with the proxy-installed one.
func TestTemplateCIBuildsTheCLIFromTheRecordedSource(t *testing.T) {
	workflow, err := os.ReadFile(filepath.Join("..", "..", templateCIWorkflowFile))
	if err != nil {
		t.Fatalf("cannot read %s: %v", templateCIWorkflowFile, err)
	}
	if body := string(workflow); strings.Contains(body, "go install github.com/") {
		t.Errorf("%s installs the CLI from the module proxy; the proxy caches by tag string, so a reused tag serves a stale build that disagrees with the manifest it reads", templateCIWorkflowFile)
	}
	if !strings.Contains(string(workflow), "uses: ./.github/actions/cli") {
		t.Errorf("%s does not use the composite action that builds the CLI from the recorded source", templateCIWorkflowFile)
	}

	action, err := os.ReadFile(filepath.Join("..", "..", templateCLIActionFile))
	if err != nil {
		t.Fatalf("cannot read %s: %v", templateCLIActionFile, err)
	}
	body := string(action)
	for _, want := range []string{
		"repoforge.manifest.json",
		"source_url",
		"source_commit",
		"git init --quiet",
		"git -C \"$src\" remote add origin",
		"git -C \"$src\" fetch --quiet --depth 1 origin",
		"go build -C",
	} {
		if !strings.Contains(body, want) {
			t.Errorf("%s does not %q; the CLI must be built from the engine commit recorded in the manifest", templateCLIActionFile, want)
		}
	}
}

// TestTemplateCIHasNoArtifactRoundTripForTheCLI is the regression test
// for the publication whose template went red on exit 126.
//
// The gated jobs used to download the source-built CLI from a workflow
// artifact. The executable bit did not survive the round-trip, so both
// jobs failed with exit 126 (command found, permission denied) — after
// the build job had produced it successfully. The fix removes the
// artifact entirely and builds in each job.
func TestTemplateCIHasNoArtifactRoundTripForTheCLI(t *testing.T) {
	data, err := os.ReadFile(filepath.Join("..", "..", templateCIWorkflowFile))
	if err != nil {
		t.Fatal(err)
	}
	body := string(data)
	for _, forbidden := range []string{
		"actions/upload-artifact",
		"actions/download-artifact",
	} {
		if strings.Contains(body, forbidden) {
			t.Errorf("%s still uses %s for the CLI; an artifact round-trip loses the executable bit and both gated jobs fail with exit 126", templateCIWorkflowFile, forbidden)
		}
	}
}

// TestTemplateCIProvenanceGateIsRepositoryScoped proves the strict gates
// are scoped to the canonical template repository, so an adopting project
// is not failed for adding its own source.
func TestTemplateCIProvenanceGateIsRepositoryScoped(t *testing.T) {
	data, err := os.ReadFile(filepath.Join("..", "..", templateCIWorkflowFile))
	if err != nil {
		t.Fatal(err)
	}
	body := string(data)
	scoped := regexp.MustCompile(`(?m)^\s*if: github\.repository == 'FernSyntax/repoforge-template'$`)
	if n := len(scoped.FindAllString(body, -1)); n < 2 {
		t.Errorf("the repoforge and provenance jobs must both be scoped to the canonical template repository; found %d such guards, want at least 2", n)
	}
	if !strings.Contains(body, "needs: source") {
		t.Error("the gated jobs must depend on the source job, which resolves the recorded engine commit")
	}
}

// TestTemplateCINeverReplacesPATH is the regression test for the template
// CI that failed with
//
//	exec: "git": executable file not found in $PATH
//
// after every step up to that point had succeeded. The cause was
//
//	env:
//	  PATH: ${{ runner.temp }}:${{ env.PATH }}
//
// The `env.PATH` context resolves to empty in a step-level `env:` block,
// so the assignment REPLACES PATH with just $RUNNER_TEMP instead of
// prepending to it. `git` — and every other tool — then becomes
// unfindable, and the failure names a missing binary rather than the
// env override that removed it.
//
// The documented way to extend PATH inside a job is to append a
// directory to $GITHUB_PATH from a run step, which accumulates and cannot
// clobber anything.
func TestTemplateCINeverReplacesPATH(t *testing.T) {
	data, err := os.ReadFile(filepath.Join("..", "..", templateCIWorkflowFile))
	if err != nil {
		t.Fatal(err)
	}
	// Comments are stripped: the workflow's own header explains the bug
	// by naming it, and a comment that mentions the broken expression is
	// not a use of it.
	var code strings.Builder
	for _, line := range strings.Split(string(data), "\n") {
		if strings.HasPrefix(strings.TrimSpace(line), "#") {
			continue
		}
		code.WriteString(line)
		code.WriteString("\n")
	}
	body := code.String()

	if strings.Contains(body, "${{ env.PATH }}") {
		t.Errorf("%s overrides PATH with a ${{ env.PATH }} expansion; that context resolves to empty in a step-level env block, so PATH is replaced rather than prepended and every tool becomes unfindable", templateCIWorkflowFile)
	}
	// A bare `PATH:` assignment is the shape to avoid.
	barePath := regexp.MustCompile(`(?m)^\s*PATH:\s*\$\{\{[\s\S]*?\}\}\s*$`)
	if m := barePath.FindString(body); m != "" {
		t.Errorf("%s assigns PATH directly (%q); append to $GITHUB_PATH instead", templateCIWorkflowFile, strings.TrimSpace(m))
	}
	if !strings.Contains(body, `echo "$RUNNER_TEMP" >> "$GITHUB_PATH"`) {
		t.Errorf("%s must extend PATH through $GITHUB_PATH", templateCIWorkflowFile)
	}
}

// TestTemplateCIGitHubPathIsItsOwnStep is the regression test for the
// provenance job that failed with
//
//	repoforge: command not found
//
// even though the CLI had just been built successfully.
//
// $GITHUB_PATH takes effect for SUBSEQUENT steps, not for the step that
// writes it. Appending to it inside the same run block as the command
// leaves the command unable to find the binary, and the failure names a
// missing command rather than the ordering that caused it.
func TestTemplateCIGitHubPathIsItsOwnStep(t *testing.T) {
	data, err := os.ReadFile(filepath.Join("..", "..", templateCIWorkflowFile))
	if err != nil {
		t.Fatal(err)
	}
	body := string(data)

	// The write must be the last command of its own step.
	if strings.Contains(body, ">> \"$GITHUB_PATH\"") {
		// Split into steps and check none that appends to GITHUB_PATH also
		// invokes the CLI.
		for _, block := range strings.Split(body, "      - name: ") {
			if !strings.Contains(block, ">> \"$GITHUB_PATH\"") {
				continue
			}
			if strings.Contains(block, "repoforge template") {
				name := strings.SplitN(block, "\n", 2)[0]
				t.Errorf("the step %q appends to $GITHUB_PATH and also runs the CLI; $GITHUB_PATH applies to subsequent steps only, so the CLI is not found", strings.TrimSpace(name))
			}
		}
	}

	// And each run of the CLI must be its own step.
	invocations := regexp.MustCompile(`(?m)^\s*run:\s*repoforge template`)
	if n := len(invocations.FindAllString(body, -1)); n != 2 {
		t.Errorf("found %d steps that run the CLI; want 2, each in its own step so the PATH extension has already taken effect", n)
	}
}
