package validate

import (
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"testing"

	"gopkg.in/yaml.v3"
)

// publicationWorkflowFile is the workflow under test.
const publicationWorkflowFile = ".github/workflows/template-publication.yml"

// TestPublicationResolvesTheTagCommitNotTheReleaseBranch is the
// regression test for the publication that refused to publish.
//
// The publication must confirm that the release tag targets the commit
// it is building. The obvious implementation — comparing the release's
// `targetCommitish` against `git rev-parse HEAD` — cannot work, because
// for a release created from a tag GitHub reports `targetCommitish` as
// the BRANCH the tag sits on, not as a SHA:
//
//	release target: main
//	checked-out: 5866a73a1ba238a7d5233dcd881533cfa8f451e4
//
// The check compares a branch name against a digest, so it fails on
// every run while looking exactly like a legitimate guard. The tag
// object is the authoritative reference, and the workflow is triggered
// by it.
//
// This test asserts both that the broken comparison is gone and that the
// correct one resolves a real annotated tag to the right commit.
func TestPublicationResolvesTheTagCommitNotTheReleaseBranch(t *testing.T) {
	data, err := os.ReadFile(filepath.Join("..", "..", publicationWorkflowFile))
	if err != nil {
		t.Fatalf("cannot read %s: %v", publicationWorkflowFile, err)
	}
	// Comments are stripped before the assertions: the workflow's own
	// header explains the bug this test guards against, and a comment
	// that names the broken field is not a use of it.
	var code strings.Builder
	for _, line := range strings.Split(string(data), "\n") {
		trimmed := strings.TrimSpace(line)
		if strings.HasPrefix(trimmed, "#") {
			continue
		}
		code.WriteString(line)
		code.WriteString("\n")
	}
	body := code.String()

	if strings.Contains(body, "targetCommitish") {
		t.Errorf("%s compares targetCommitish against a commit SHA; GitHub reports that field as the branch name (%q) for a release created from a tag, so the check can never match", publicationWorkflowFile, "main")
	}
	for _, want := range []string{
		`git rev-parse --verify --quiet "$tag^{commit}"`,
		`git rev-parse HEAD`,
		`git cat-file -t "$tag"`,
	} {
		if !strings.Contains(body, want) {
			t.Errorf("%s does not contain %q; the tag's commit must be resolved from the tag object itself", publicationWorkflowFile, want)
		}
	}
}

// TestTagCommitResolutionLogicExecutesCorrectly runs the resolution
// procedure the workflow uses, against a real annotated tag, so the
// guard is behavioural rather than textual.
func TestTagCommitResolutionLogicExecutesCorrectly(t *testing.T) {
	dir := t.TempDir()
	run := func(args ...string) string {
		t.Helper()
		cmd := exec.Command("git", args...)
		cmd.Dir = dir
		cmd.Env = append(os.Environ(),
			"GIT_AUTHOR_NAME=t", "GIT_AUTHOR_EMAIL=t@example.invalid",
			"GIT_COMMITTER_NAME=t", "GIT_COMMITTER_EMAIL=t@example.invalid",
			"GIT_CONFIG_NOSYSTEM=1", "HOME="+t.TempDir(),
			// Two config entries: signing is disabled for both commit and tag.
			"GIT_CONFIG_COUNT=2",
			"GIT_CONFIG_KEY_0=commit.gpgsign", "GIT_CONFIG_VALUE_0=false",
			"GIT_CONFIG_KEY_1=tag.gpgsign", "GIT_CONFIG_VALUE_1=false")
		out, err := cmd.CombinedOutput()
		if err != nil {
			t.Fatalf("git %s: %v\n%s", strings.Join(args, " "), err, out)
		}
		return strings.TrimSpace(string(out))
	}

	run("init", "--quiet", "-b", "main", dir)
	if err := os.WriteFile(filepath.Join(dir, "f"), []byte("x\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	run("add", "-A")
	run("commit", "--quiet", "-m", "init")
	head := run("rev-parse", "HEAD")
	run("tag", "-a", "v1.0.0", "-m", "v1.0.0")

	// The command the workflow uses, verbatim.
	resolved := run("rev-parse", "--verify", "--quiet", "v1.0.0^{commit}")
	if resolved != head {
		t.Errorf("tag resolution: %s, want %s", resolved, head)
	}
	if typ := run("cat-file", "-t", "v1.0.0"); typ != "tag" {
		t.Errorf("tag object type = %q, want tag (annotated)", typ)
	}

	// A lightweight tag must be rejected by the same check.
	run("tag", "v2.0.0")
	if typ := run("cat-file", "-t", "v2.0.0"); typ == "tag" {
		t.Error("a lightweight tag must not report type tag")
	}
}

// TestPublishedNotesUseRealNewlines is the regression test for release
// notes and PR bodies that were a single line.
//
// The natural way to build a multi-paragraph body in bash is
//
//	body="$body$(printf '\n\n')Manifest digest: $D"
//
// and it does not work: command substitution strips trailing newlines,
// so `$(printf '\n\n')` expands to the empty string and every paragraph
// runs together on one line with no separator at all. `printf -v` reads
// the format string's `\n` escapes directly, so it produces real
// newlines.
func TestPublishedNotesUseRealNewlines(t *testing.T) {
	data, err := os.ReadFile(filepath.Join("..", "..", publicationWorkflowFile))
	if err != nil {
		t.Fatalf("cannot read %s: %v", publicationWorkflowFile, err)
	}
	var code strings.Builder
	for _, line := range strings.Split(string(data), "\n") {
		if strings.HasPrefix(strings.TrimSpace(line), "#") {
			continue
		}
		code.WriteString(line)
		code.WriteString("\n")
	}
	body := code.String()

	if strings.Contains(body, `$(printf '\n\n')`) || strings.Contains(body, `$(printf "\n\n")`) {
		t.Errorf("%s builds notes with $(printf '\\n\\n'); command substitution strips trailing newlines, so the body has no separators", publicationWorkflowFile)
	}
	for _, want := range []string{"printf -v notes", "printf -v body"} {
		if !strings.Contains(body, want) {
			t.Errorf("%s does not use %q for the published text", publicationWorkflowFile, want)
		}
	}

	// Behavioural half: the two commands must actually produce newlines
	// where paragraphs meet.
	for _, tc := range []struct{ name, format string }{
		{"notes", "Generated from %s %s (%s).\\n\\nManifest digest: %s\\n\\n"},
		{"body", "Generated from %s %s (%s).\\n\\nManifest digest: %s\\n\\n"},
	} {
		cmd := exec.Command("bash", "-c", `printf -v out "$1"; printf '%s' "$out"`, "_",
			"x v1.0.0 (abc).\n\nManifest digest: d\n\n")
		raw, err := cmd.Output()
		if err != nil {
			t.Fatalf("%s: %v", tc.name, err)
		}
		if !strings.Contains(string(raw), "\n\n") {
			t.Errorf("%s: the format did not produce a paragraph break", tc.name)
		}
	}
}

// hiddenPathRe matches the `path:` input of an artifact upload step.
var hiddenPathRe = regexp.MustCompile(`(?m)^\s*path:\s*(\S+)\s*$`)

// TestHiddenUploadPathsSetIncludeHiddenFiles is the regression test for
// the publication that failed after a successful build.
//
// actions/upload-artifact v4 and later exclude any path whose name begins
// with a dot unless `include-hidden-files: true` is set. The template
// build writes to `.template-out/`, so the upload step failed with:
//
//	No files were found with the provided path: .template-out/.
//
// ...immediately after a step that had just written 42 files there. The
// failure names the path, not the cause, which makes it look like a
// build problem rather than an uploader default.
//
// The guard is that every artifact upload from a dot-prefixed path sets
// the flag, checked across every workflow in the repository.
func TestHiddenUploadPathsSetIncludeHiddenFiles(t *testing.T) {
	var checked int
	for _, dir := range workflowDirs {
		entries, err := os.ReadDir(filepath.Join("..", "..", dir))
		if err != nil {
			continue
		}
		for _, e := range entries {
			if e.IsDir() || !strings.HasSuffix(e.Name(), ".yml") && !strings.HasSuffix(e.Name(), ".yaml") {
				continue
			}
			path := filepath.Join(dir, e.Name())
			data, err := os.ReadFile(filepath.Join("..", "..", path))
			if err != nil {
				continue
			}
			if !strings.Contains(string(data), "upload-artifact") {
				continue
			}
			for _, block := range uploadBlocks(string(data)) {
				checked++
				if !strings.Contains(block, "upload-artifact") {
					continue
				}
				m := hiddenPathRe.FindStringSubmatch(block)
				if m == nil {
					continue
				}
				target := m[1]
				if !strings.HasPrefix(target, ".") && !strings.Contains(target, "/.") {
					continue
				}
				if !strings.Contains(block, "include-hidden-files: true") {
					t.Errorf("%s uploads from the hidden path %q without include-hidden-files: true; upload-artifact v4+ excludes dot-prefixed paths and the step fails with \"No files were found\"", path, target)
				}
			}
		}
	}
	if checked == 0 {
		t.Fatal("no artifact upload steps found; the guard must run against real steps")
	}
}

// uploadBlocks splits a workflow into its step blocks so `path:` can be
// matched against the `include-hidden-files` of the same step rather
// than of some later one.
func uploadBlocks(body string) []string {
	var out []string
	rest := body
	for {
		i := strings.Index(rest, "      - name:")
		if i < 0 {
			break
		}
		rest = rest[i:]
		j := strings.Index(rest[len("      - name:"):], "      - name:")
		if j < 0 {
			out = append(out, rest)
			break
		}
		out = append(out, rest[:j+len("      - name:")])
		rest = rest[j+len("      - name:"):]
	}
	return out
}

// TestEmptinessCheckTreats409AsEmpty is the regression test for the
// publication that refused to publish to an empty repository.
//
// An EMPTY repository does not present a zero-length commit list. The
// commits endpoint answers:
//
//	HTTP 409 {"message":"Git Repository is empty.", ...}
//
// and gh prints that error body to stdout while exiting non-zero. So
//
//	commits=$(gh api ... --jq 'length' 2>/dev/null || echo "0")
//
// captures the error JSON *and* the fallback concatenated:
//
//	state: commits={"message":"Git Repository is empty.",...}0
//
// which then reads as a non-empty count. The guard therefore rejected
// precisely the state it exists to accept.
//
// The test runs the real step body against a stub gh that reproduces the
// 409, so the behaviour is verified rather than described.
func TestEmptinessCheckTreats409AsEmpty(t *testing.T) {
	data, err := os.ReadFile(filepath.Join("..", "..", publicationWorkflowFile))
	if err != nil {
		t.Fatalf("cannot read %s: %v", publicationWorkflowFile, err)
	}
	if body := string(data); strings.Contains(body, `|| echo "0"`) || strings.Contains(body, `|| echo 0`) {
		// The comment explains the bug by naming it; ignore comments.
		var code strings.Builder
		for _, line := range strings.Split(body, "\n") {
			if strings.HasPrefix(strings.TrimSpace(line), "#") {
				continue
			}
			code.WriteString(line)
			code.WriteString("\n")
		}
		if strings.Contains(code.String(), `|| echo "0"`) || strings.Contains(code.String(), `|| echo 0`) {
			t.Errorf("%s still uses `$(gh api ... || echo 0)`; gh prints the 409 error body to stdout while exiting non-zero, so the fallback is concatenated rather than substituted", publicationWorkflowFile)
		}
	}

	// Extract the real step and run it.
	step := extractStep(t, publicationWorkflowFile, "Refuse a target that is not empty")
	runStep := func(ghBody string, exitCode int) (string, int) {
		t.Helper()
		bin := t.TempDir()
		script := "#!/bin/sh\ncat <<'JSON'\n" + ghBody + "\nJSON\nexit " + itoa(exitCode) + "\n"
		if err := os.WriteFile(filepath.Join(bin, "gh"), []byte(script), 0o755); err != nil {
			t.Fatal(err)
		}
		cmd := exec.Command("bash", "-e")
		cmd.Stdin = strings.NewReader(step)
		cmd.Env = append(os.Environ(),
			"PATH="+bin+":"+os.Getenv("PATH"),
			"TARGET=FernSyntax/repoforge-template")
		out, err := cmd.CombinedOutput()
		code := 0
		if ee, ok := err.(*exec.ExitError); ok {
			code = ee.ExitCode()
		} else if err != nil {
			t.Fatalf("running the step: %v", err)
		}
		return string(out), code
	}

	// 1. An empty repository must be accepted.
	out, code := runStep(`{"message":"Git Repository is empty.","status":409}`, 1)
	if code != 0 {
		t.Errorf("an empty repository must be accepted, got exit %d:\n%s", code, out)
	}
	if !strings.Contains(out, "is empty; the initial path may proceed") {
		t.Errorf("the empty case must be reported:\n%s", out)
	}
	if !strings.Contains(out, "commits=0 ") {
		t.Errorf("commits must be counted as zero:\n%s", out)
	}

	// 2. A genuine permission failure must fail closed.
	_, code = runStep(`{"message":"Bad credentials","status":401}`, 1)
	if code == 0 {
		t.Error("a genuine API failure must fail closed, not be read as empty")
	}

	// 3. A repository that already has content must still be refused.
	out, code = runStep(`[{"sha":"abc"}`, 0)
	if code != 1 {
		t.Errorf("a non-empty repository must be refused, got exit %d", code)
	}
	if !strings.Contains(out, "refuses to overwrite") {
		t.Errorf("the refusal must say why:\n%s", out)
	}
}

// extractStep pulls a step's run body out of a workflow file.
func yamlUnmarshalForTest(data []byte, v any) error {
	return yaml.Unmarshal(data, v)
}

func extractStep(t *testing.T, path, name string) string {
	t.Helper()
	data, err := os.ReadFile(filepath.Join("..", "..", path))
	if err != nil {
		t.Fatal(err)
	}
	var wf struct {
		Jobs map[string]struct {
			Steps []struct {
				Name string `yaml:"name"`
				Run  string `yaml:"run"`
			} `yaml:"steps"`
		} `yaml:"jobs"`
	}
	if err := yamlUnmarshalForTest(data, &wf); err != nil {
		t.Fatal(err)
	}
	for _, job := range wf.Jobs {
		for _, s := range job.Steps {
			if s.Name == name {
				return s.Run
			}
		}
	}
	t.Fatalf("step %q not found in %s", name, path)
	return ""
}

func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	var b []byte
	for n > 0 {
		b = append([]byte{byte('0' + n%10)}, b...)
		n /= 10
	}
	return string(b)
}

// TestTagDereferenceUsesTheGitTagsEndpoint is the regression test for the
// publication that failed after successfully creating the tag.
//
// The verification step built the dereference URL as
//
//	repos/$TARGET/repos/$TARGET/git/tags/$deref
//
// a duplicated path prefix that 404s, and the fallback inherited the
// same 404. The check then printed the JSON error body in place of the
// SHA and compared that against the root commit, so the two sides looked
// almost identical and the failure read as tag drift rather than a bad
// URL. The expected SHA was already correct and present: the tag object
// had been created properly and only the verification was wrong.
//
// The dereference must go straight to /git/tags/<sha>, the one endpoint
// that resolves a tag object to its commit.
//
// The fix was verified against the real GitHub API on a disposable
// repository, which was then deleted.
func TestTagDereferenceUsesTheGitTagsEndpoint(t *testing.T) {
	data, err := os.ReadFile(filepath.Join("..", "..", publicationWorkflowFile))
	if err != nil {
		t.Fatal(err)
	}
	// Comments are stripped: the explanatory comment names the broken
	// path by quoting it, and quoting it is not building it.
	var code strings.Builder
	for _, line := range strings.Split(string(data), "\n") {
		if strings.HasPrefix(strings.TrimSpace(line), "#") {
			continue
		}
		code.WriteString(line)
		code.WriteString("\n")
	}
	body := code.String()

	// No API path may contain the repository prefix twice.
	dup := regexp.MustCompile(`repos/\$[A-Za-z_]+/repos/\$[A-Za-z_]+/`)
	if m := dup.FindString(body); m != "" {
		t.Errorf("%s builds the API path %q; the repository prefix appears twice, so the request 404s and the failure looks like tag drift", publicationWorkflowFile, m)
	}

	want := `gh api "repos/$TARGET/git/tags/$deref" --jq '.object.sha'`
	if !strings.Contains(body, want) {
		t.Errorf("%s does not dereference through %q", publicationWorkflowFile, want)
	}
}

// workflowConcurrency mirrors the `concurrency:` block GitHub parses.
type workflowConcurrency struct {
	Group            string `yaml:"group"`
	CancelInProgress bool   `yaml:"cancel-in-progress"`
}

func readConcurrency(t *testing.T) workflowConcurrency {
	t.Helper()
	data, err := os.ReadFile(filepath.Join("..", "..", publicationWorkflowFile))
	if err != nil {
		t.Fatal(err)
	}
	var wf struct {
		Concurrency workflowConcurrency `yaml:"concurrency"`
	}
	if err := yaml.Unmarshal(data, &wf); err != nil {
		t.Fatalf("cannot parse %s: %v", publicationWorkflowFile, err)
	}
	return wf.Concurrency
}

// evalGroup simulates GitHub's `${{ A || B }}` inside a concurrency
// group for the two trigger shapes this workflow has: a workflow_run
// (head_branch carries the tag) and a manual dispatch (inputs.tag).
func evalGroup(t *testing.T, group, headBranch, inputTag string) string {
	t.Helper()
	re := regexp.MustCompile(`\$\{\{\s*github\.event\.workflow_run\.head_branch\s*\|\|\s*inputs\.tag\s*\}\}`)
	loc := re.FindStringIndex(group)
	if loc == nil {
		t.Fatalf("concurrency group %q is not keyed on head_branch || inputs.tag", group)
	}
	var key string
	if headBranch != "" {
		key = headBranch
	} else {
		key = inputTag
	}
	return group[:loc[0]] + key + group[loc[1]:]
}

// TestPublicationConcurrencyIsKeyedOnReleaseIdentity is the regression
// test for publications cancelling each other.
//
// The old key was `workflow`-plus-`ref`. For `workflow_run`, `github.ref`
// is the default branch, so every release tag shared one group: pushing
// v0.2.0 while v0.1.0 was still publishing cancelled v0.1.0 midway
// through a remote write. The key must be the source release identity so
// distinct releases never cancel each other, while duplicate runs for
// the same release share a group.
func TestPublicationConcurrencyIsKeyedOnReleaseIdentity(t *testing.T) {
	c := readConcurrency(t)
	if strings.Contains(c.Group, "github.ref") {
		t.Errorf("concurrency group %q is keyed on github.ref, which is the default branch for workflow_run — every release tag shares one group", c.Group)
	}
	runA := evalGroup(t, c.Group, "v0.1.0", "")
	runB := evalGroup(t, c.Group, "v0.2.0", "")
	runA2 := evalGroup(t, c.Group, "", "v0.1.0")
	if runA == runB {
		t.Errorf("distinct releases share concurrency group %q; one would cancel the other", runA)
	}
	if runA != runA2 {
		t.Errorf("workflow_run for v0.1.0 (%q) and manual dispatch of v0.1.0 (%q) must share a group so duplicates queue instead of racing", runA, runA2)
	}
}

// TestPublicationNeverCancelsInFlight is the companion guard: with the
// identity key in place, an in-flight run must never be killed by a
// later one. Cancelling between "push root commit" and "publish release"
// leaves a partially initialized template.
func TestPublicationNeverCancelsInFlight(t *testing.T) {
	c := readConcurrency(t)
	if c.CancelInProgress {
		t.Errorf("cancel-in-progress is true; a later run would kill an active publication midway through a remote write")
	}
}

// TestPublicationBuildsFromTheTriggeringSHA is the regression test for
// publications built from the wrong source.
//
// On `workflow_run`, both `github.sha` and a default `actions/checkout`
// mean the DEFAULT BRANCH tip, not the release commit. If main advances
// after the tag is created, `source-commit=$(git rev-parse HEAD)` and
// every `HEAD_SHA: ${{ github.sha }}` silently record main's tip while
// the manifest claims the tag. The template is then built, verified,
// and published from a commit the release never vetted.
//
// The workflow must therefore (a) resolve the release tag to its
// commit through the API before anything runs, (b) check out exactly
// that commit — as resolved data, never as an event expression — and
// (c) feed that resolved commit, never HEAD, never the branch tip,
// to every later step as `source-commit`.
func TestPublicationBuildsFromTheTriggeringSHA(t *testing.T) {
	data, err := os.ReadFile(filepath.Join("..", "..", publicationWorkflowFile))
	if err != nil {
		t.Fatal(err)
	}
	body := string(data)

	// (a) The checkout pins the resolved release commit. An event
	// expression in a checkout ref is what the untrusted-checkout
	// scanners flag, and it is what previously let the publishing
	// credential coexist with release-source code in one job.
	if !strings.Contains(body, "ref: ${{ steps.source.outputs.source-commit }}") {
		t.Errorf("%s does not pin the checkout to the resolved release commit", publicationWorkflowFile)
	}
	for _, line := range strings.Split(body, "\n") {
		trimmed := strings.TrimSpace(line)
		if !strings.HasPrefix(trimmed, "ref:") {
			continue
		}
		if strings.Contains(trimmed, "github.event.") {
			t.Errorf("%s checks out an event expression (%q); a checkout ref must be resolved data, not attacker-influenceable event input", publicationWorkflowFile, trimmed)
		}
		if strings.Contains(trimmed, "github.sha") {
			t.Errorf("%s checks out bare github.sha (%q); on workflow_run that is the default-branch tip, not the release", publicationWorkflowFile, trimmed)
		}
	}
	// (c) No step takes the branch tip as its source.
	if strings.Contains(body, "HEAD_SHA: ${{ github.sha }}") {
		t.Errorf("%s feeds bare github.sha as a source commit; on workflow_run that is the default-branch tip, not the release", publicationWorkflowFile)
	}
	if !strings.Contains(body, "HEAD_SHA: ${{ steps.source.outputs.source-commit }}") {
		t.Errorf("%s does not feed the resolved source-commit to the build steps", publicationWorkflowFile)
	}
	// (b) The checkout is verified against the release identity: the
	// tag object is annotated and dereferences to the commit the tree
	// is sitting on.
	if !strings.Contains(body, `git rev-parse --verify --quiet "$tag^{commit}"`) {
		t.Errorf("%s does not dereference the tag object; the tag's commit must be resolved from the tag object itself", publicationWorkflowFile)
	}
	if !strings.Contains(body, `git cat-file -t "$tag"`) {
		t.Errorf("%s does not assert the tag object type; a lightweight tag must be rejected", publicationWorkflowFile)
	}
	if !strings.Contains(body, "git rev-parse HEAD") {
		t.Errorf("%s does not compare the checked-out tree against the resolved commit", publicationWorkflowFile)
	}
	if strings.Contains(body, `echo "source-commit=$(git rev-parse HEAD)"`) {
		t.Errorf("%s records HEAD as the source commit; HEAD is whatever got checked out, not what was resolved", publicationWorkflowFile)
	}
}

// publicationJob mirrors the job fields the separation test needs.
type publicationJob struct {
	Needs       any               `yaml:"needs"`
	Permissions map[string]string `yaml:"permissions"`
	Steps       []struct {
		Uses string         `yaml:"uses"`
		With map[string]any `yaml:"with"`
		Env  map[string]any `yaml:"env"`
		Run  string         `yaml:"run"`
	} `yaml:"steps"`
}

// neededJobs normalizes the two YAML shapes `needs` can take: a single
// job id as a string, or a list of job ids.
func neededJobs(needs any) []string {
	switch t := needs.(type) {
	case string:
		return []string{t}
	case []any:
		out := make([]string, 0, len(t))
		for _, e := range t {
			if s, ok := e.(string); ok {
				out = append(out, s)
			}
		}
		return out
	}
	return nil
}

// TestPublicationSeparatesBuildFromPublish pins the privilege split
// that keeps the publishing credential away from release-source code:
// the job that checks out and executes the release source (build)
// holds no secrets and no id-token, and the job that holds the App
// private key (publish) executes only the default-branch engine code.
//
// This is the structural half of the untrusted-checkout fix: the
// credential and the untrusted-code execution must live in different
// jobs, or no amount of ref hygiene helps.
func TestPublicationSeparatesBuildFromPublish(t *testing.T) {
	data, err := os.ReadFile(filepath.Join("..", "..", publicationWorkflowFile))
	if err != nil {
		t.Fatal(err)
	}
	var wf struct {
		Jobs map[string]publicationJob `yaml:"jobs"`
	}
	if err := yamlUnmarshalForTest(data, &wf); err != nil {
		t.Fatal(err)
	}
	build, ok := wf.Jobs["build"]
	if !ok {
		t.Fatal("no build job: the job that executes the release source is missing")
	}
	publish, ok := wf.Jobs["publish"]
	if !ok {
		t.Fatal("no publish job: the job that holds the credential is missing")
	}
	// The publish job must run after, and only after, the build.
	found := false
	for _, n := range neededJobs(publish.Needs) {
		if n == "build" {
			found = true
		}
	}
	if !found {
		t.Errorf("the publish job does not need the build job; the two halves of the publication are not ordered")
	}

	// The build job is unprivileged: contents: read and nothing else.
	for scope := range build.Permissions {
		if scope != "contents" {
			t.Errorf("the build job grants %s; a job that executes the release source must hold only contents: read", scope)
		}
	}
	if level, ok := build.Permissions["contents"]; ok && level != "read" {
		t.Errorf("the build job grants contents: %s; a job that executes the release source needs read only", level)
	}
	// No secrets and no id-token anywhere in the build job. The job is
	// re-marshaled so the check covers every field, not just the ones
	// this test knows about.
	buildYAML, err := yaml.Marshal(map[string]any{"job": build})
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(buildYAML), "secrets.") {
		t.Errorf("the build job references a repository secret; the release-source execution must hold nothing an attacker could want:\n%s", buildYAML)
	}
	if strings.Contains(string(buildYAML), "id-token") {
		t.Errorf("the build job references id-token; the release-source execution must hold nothing an attacker could want:\n%s", buildYAML)
	}
	if _, ok := publish.Permissions["id-token"]; !ok {
		t.Error("the publish job does not hold id-token: write; it cannot mint the App installation token")
	}
}

// TestPublicationSurvivesMainAdvancingPastTheTag reproduces the exact
// failure mode: tag created, main advances, publication must still
// build from the tag's commit. It runs the same git operations the
// workflow's resolve step uses, in order.
func TestPublicationSurvivesMainAdvancingPastTheTag(t *testing.T) {
	dir := t.TempDir()
	run := func(args ...string) string {
		t.Helper()
		cmd := exec.Command("git", args...)
		cmd.Dir = dir
		cmd.Env = append(os.Environ(),
			"GIT_AUTHOR_NAME=t", "GIT_AUTHOR_EMAIL=t@example.invalid",
			"GIT_COMMITTER_NAME=t", "GIT_COMMITTER_EMAIL=t@example.invalid",
			"GIT_CONFIG_NOSYSTEM=1", "HOME="+t.TempDir(),
			"GIT_CONFIG_COUNT=2",
			"GIT_CONFIG_KEY_0=commit.gpgsign", "GIT_CONFIG_VALUE_0=false",
			"GIT_CONFIG_KEY_1=tag.gpgsign", "GIT_CONFIG_VALUE_1=false")
		out, err := cmd.CombinedOutput()
		if err != nil {
			t.Fatalf("git %s: %v\n%s", strings.Join(args, " "), err, out)
		}
		return strings.TrimSpace(string(out))
	}

	// Commit A, tag it, then advance main to commit B — the state the
	// workflow faces when a release tag already exists and main moved on.
	if err := os.WriteFile(filepath.Join(dir, "f"), []byte("a\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	run("init", "--quiet", "-b", "main", dir)
	run("add", "-A")
	run("commit", "--quiet", "-m", "release commit")
	run("tag", "-a", "v9.9.9", "-m", "v9.9.9")
	commitA := run("rev-parse", "HEAD")
	if err := os.WriteFile(filepath.Join(dir, "f"), []byte("b\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	run("add", "-A")
	run("commit", "--quiet", "-m", "later main commit")
	commitB := run("rev-parse", "HEAD")
	if commitA == commitB {
		t.Fatal("test setup failed: tag and main tip are the same commit")
	}

	// What `git rev-parse HEAD` would have recorded before the fix.
	if got := run("rev-parse", "HEAD"); got != commitB {
		t.Fatalf("HEAD = %s, want main tip %s", got, commitB)
	}
	// What the fixed resolve step records instead: the tag's commit.
	resolved := run("rev-parse", "--verify", "--quiet", "v9.9.9^{commit}")
	if resolved != commitA {
		t.Fatalf("resolved = %s, want tag commit %s", resolved, commitA)
	}
	// Pinning the tree lands on the tag's commit, not main's tip.
	run("checkout", "--quiet", "--detach", resolved)
	if got := run("rev-parse", "HEAD"); got != commitA {
		t.Fatalf("pinned HEAD = %s, want tag commit %s", got, commitA)
	}
	// And the tag is the annotated object the policy requires.
	if typ := run("cat-file", "-t", "v9.9.9"); typ != "tag" {
		t.Errorf("tag object type = %q, want tag (annotated)", typ)
	}
}

// canonicalMainRuleset is the solo-profile branch ruleset the template
// repository must carry. The required_status_checks contexts are filled
// per repository at apply time, so the contract pins the structure and
// the template-CI test pins the context names.
const canonicalMainRuleset = "configs/github/rulesets/main-solo.json"

// templateCIWorkflow is the CI whose job names become required checks.
const templateCIWorkflow = "template/.github/workflows/ci.yml"

// TestTemplateBranchProtectionContract pins the repository contract the
// canonical template must carry: PR-only updates, the checks its own CI
// actually emits, no force-push or deletion, squash merging, no bypass
// actor for the publication bot, and no independent approval that a
// single maintainer cannot supply.
//
// Verified live against FernSyntax/repoforge-template on 2026-10-11:
// repoforge:main (branch, active) and repoforge:release-tags (tag,
// active), required checks [verify, repoforge, provenance], zero
// approvals, squash only, no bypass actors.
func TestTemplateBranchProtectionContract(t *testing.T) {
	data, err := os.ReadFile(filepath.Join("..", "..", canonicalMainRuleset))
	if err != nil {
		t.Fatal(err)
	}
	var rs struct {
		Name        string `yaml:"name"`
		Target      string `yaml:"target"`
		Enforcement string `yaml:"enforcement"`
		Bypass      []any  `yaml:"bypass_actors"`
		Rules       []struct {
			Type       string         `yaml:"type"`
			Parameters map[string]any `yaml:"parameters"`
		} `yaml:"rules"`
	}
	// The canonical files are JSON; yaml.v3 reads JSON as a subset.
	if err := yaml.Unmarshal(data, &rs); err != nil {
		t.Fatalf("cannot parse %s: %v", canonicalMainRuleset, err)
	}
	if rs.Name != "repoforge:main" || rs.Target != "branch" || rs.Enforcement != "active" {
		t.Errorf("ruleset identity = %s/%s/%s, want repoforge:main/branch/active", rs.Name, rs.Target, rs.Enforcement)
	}
	if len(rs.Bypass) != 0 {
		t.Errorf("bypass_actors has %d entries; the publication bot must not bypass its own protection", len(rs.Bypass))
	}
	byType := map[string]map[string]any{}
	for _, r := range rs.Rules {
		byType[r.Type] = r.Parameters
	}
	pr, ok := byType["pull_request"]
	if !ok {
		t.Fatal("no pull_request rule: direct pushes would be allowed")
	}
	if n, _ := pr["required_approving_review_count"].(int); n != 0 {
		t.Errorf("required_approving_review_count = %v, want 0: a single maintainer cannot supply an independent approval", n)
	}
	methods, _ := pr["allowed_merge_methods"].([]any)
	if len(methods) != 1 || methods[0] != "squash" {
		t.Errorf("allowed_merge_methods = %v, want exactly [squash]", methods)
	}
	for _, need := range []string{"required_status_checks", "required_linear_history", "non_fast_forward", "deletion"} {
		if _, ok := byType[need]; !ok {
			t.Errorf("missing %s rule", need)
		}
	}

	// The required checks must be checks the template CI actually emits.
	ci, err := os.ReadFile(filepath.Join("..", "..", templateCIWorkflow))
	if err != nil {
		t.Fatal(err)
	}
	var wf struct {
		Jobs map[string]struct {
			If string `yaml:"if"`
		} `yaml:"jobs"`
	}
	if err := yaml.Unmarshal(ci, &wf); err != nil {
		t.Fatalf("cannot parse %s: %v", templateCIWorkflow, err)
	}
	for _, need := range []string{"verify", "repoforge", "provenance"} {
		job, ok := wf.Jobs[need]
		if !ok {
			t.Errorf("template CI emits no %q job, but the ruleset requires it — the check would never report", need)
			continue
		}
		// verify is generic and runs anywhere; repoforge and provenance
		// only make sense on the canonical repository.
		if need == "verify" {
			continue
		}
		if !strings.Contains(job.If, "FernSyntax/repoforge-template") {
			t.Errorf("job %q is not scoped to the canonical template repository", need)
		}
	}
}

// TestTemplateTagRulesetIsImmutable pins the release-tag protection:
// tags can be neither deleted nor updated (moved).
func TestTemplateTagRulesetIsImmutable(t *testing.T) {
	data, err := os.ReadFile(filepath.Join("..", "..", "configs/github/rulesets/tags.json"))
	if err != nil {
		t.Fatal(err)
	}
	var rs struct {
		Name   string `yaml:"name"`
		Target string `yaml:"target"`
		Rules  []struct {
			Type string `yaml:"type"`
		} `yaml:"rules"`
	}
	if err := yaml.Unmarshal(data, &rs); err != nil {
		t.Fatal(err)
	}
	if rs.Name != "repoforge:release-tags" || rs.Target != "tag" {
		t.Errorf("identity = %s/%s, want repoforge:release-tags/tag", rs.Name, rs.Target)
	}
	want := map[string]bool{"deletion": false, "update": false}
	for _, r := range rs.Rules {
		want[r.Type] = true
	}
	for typ, seen := range want {
		if !seen {
			t.Errorf("missing %s rule: release tags must be immutable", typ)
		}
	}
}

// actionRunBlock extracts a named step's shell from the composite action.
func actionRunBlock(t *testing.T, name string) string {
	t.Helper()
	data, err := os.ReadFile(filepath.Join("..", "..", "template/.github/actions/cli/action.yml"))
	if err != nil {
		t.Fatal(err)
	}
	var action struct {
		Runs struct {
			Steps []struct {
				Name string `yaml:"name"`
				Run  string `yaml:"run"`
			} `yaml:"steps"`
		} `yaml:"runs"`
	}
	if err := yaml.Unmarshal(data, &action); err != nil {
		t.Fatalf("cannot parse composite action: %v", err)
	}
	for _, s := range action.Runs.Steps {
		if s.Name == name {
			return s.Run
		}
	}
	t.Fatalf("step %q not found in composite action", name)
	return ""
}

// stubToolchain installs stub git and go binaries that record their
// invocations instead of running anything. The stubs exit 0; the go
// stub additionally creates its -o output so later steps in the script
// have a file to chmod and run.
func stubToolchain(t *testing.T, bin string) (log string) {
	t.Helper()
	log = filepath.Join(bin, "calls.log")
	git := "#!/bin/sh\nprintf 'git' >> \"" + log + "\"\nprintf ' %s' \"$@\" >> \"" + log + "\"\nprintf '\\n' >> \"" + log + "\"\nexit 0\n"
	gobuild := "#!/bin/sh\nprintf 'go' >> \"" + log + "\"\nprintf ' %s' \"$@\" >> \"" + log + "\"\nprintf '\\n' >> \"" + log + "\"\n" +
		"prev=\"\"\nfor a in \"$@\"; do\n  if [ \"$prev\" = \"-o\" ]; then touch \"$a\"; fi\n  prev=\"$a\"\ndone\nexit 0\n"
	if err := os.WriteFile(filepath.Join(bin, "git"), []byte(git), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(bin, "go"), []byte(gobuild), 0o755); err != nil {
		t.Fatal(err)
	}
	return log
}

func runActionShell(t *testing.T, script, url, commit string) (string, int) {
	t.Helper()
	bin := t.TempDir()
	calls := stubToolchain(t, bin)
	runnerTemp := t.TempDir()
	cmd := exec.Command("bash", "-e", "-c", script)
	cmd.Env = append(os.Environ(),
		"PATH="+bin+":"+os.Getenv("PATH"),
		"RUNNER_TEMP="+runnerTemp,
		"ENGINE_SOURCE_URL="+url,
		"ENGINE_SOURCE_COMMIT="+commit)
	out, err := cmd.CombinedOutput()
	code := 0
	if ee, ok := err.(*exec.ExitError); ok {
		code = ee.ExitCode()
	} else if err != nil {
		t.Fatalf("running action shell: %v", err)
	}
	log, _ := os.ReadFile(calls)
	return string(out) + "\n--- toolchain calls ---\n" + string(log), code
}

// TestCompositeActionTreatsInputsAsData is the regression test for shell
// injection through the composite action's inputs.
//
// The action used to interpolate `${{ inputs.url }}` and
// `${{ inputs.commit }}` directly into `git remote add` and `fetch`
// lines. Inputs must instead arrive strictly as data through the
// environment, be validated against an allowlist before git ever runs,
// and stay quoted at every use.
func TestCompositeActionTreatsInputsAsData(t *testing.T) {
	data, err := os.ReadFile(filepath.Join("..", "..", "template/.github/actions/cli/action.yml"))
	if err != nil {
		t.Fatal(err)
	}
	var action struct {
		Runs struct {
			Steps []struct {
				Name string            `yaml:"name"`
				Run  string            `yaml:"run"`
				Env  map[string]string `yaml:"env"`
			} `yaml:"steps"`
		} `yaml:"runs"`
	}
	if err := yaml.Unmarshal(data, &action); err != nil {
		t.Fatal(err)
	}
	for _, s := range action.Runs.Steps {
		if s.Run == "" {
			continue
		}
		if strings.Contains(s.Run, "${{ inputs.") {
			t.Errorf("step %q interpolates ${{ inputs.* }} into shell; pass inputs through env and validate them", s.Name)
		}
	}
	// The env block must carry both inputs; otherwise the run block
	// references variables that are never set.
	joined := ""
	for _, s := range action.Runs.Steps {
		for k := range s.Env {
			joined += k + " "
		}
	}
	for _, want := range []string{"ENGINE_SOURCE_URL", "ENGINE_SOURCE_COMMIT"} {
		if !strings.Contains(joined, want) {
			t.Errorf("no step exports %s; the run block must receive inputs through the environment", want)
		}
	}
}

// TestCompositeActionValidatesItsInputs runs the action's real shell
// against a matrix of hostile and valid inputs.
func TestCompositeActionValidatesItsInputs(t *testing.T) {
	script := actionRunBlock(t, "Fetch that commit and build")
	goodSHA := strings.Repeat("a", 40)

	cases := []struct {
		name    string
		url     string
		commit  string
		wantOK  bool
		wantMsg string
	}{
		{"canonical URL", "https://github.com/FernSyntax/repoforge", goodSHA, true, ""},
		{"explicit .git suffix", "https://github.com/FernSyntax/repoforge.git", goodSHA, true, ""},
		{"different host", "https://github.com/EvilOrg/repoforge", goodSHA, false, "unexpected engine source URL"},
		{"ssh URL", "git@github.com:FernSyntax/repoforge.git", goodSHA, false, "unexpected engine source URL"},
		{"file URL", "file:///etc/passwd", goodSHA, false, "unexpected engine source URL"},
		{"empty URL", "", goodSHA, false, "unexpected engine source URL"},
		{"command substitution in URL", "https://github.com/FernSyntax/repoforge$(id)", goodSHA, false, "unexpected engine source URL"},
		{"semicolon in URL", "https://github.com/FernSyntax/repoforge;id", goodSHA, false, "unexpected engine source URL"},
		{"empty commit", "https://github.com/FernSyntax/repoforge", "", false, "not a 40-character hex SHA"},
		{"short commit", "https://github.com/FernSyntax/repoforge", "abc123", false, "not a 40-character hex SHA"},
		{"long commit", "https://github.com/FernSyntax/repoforge", strings.Repeat("a", 41), false, "not a 40-character hex SHA"},
		{"uppercase hex", "https://github.com/FernSyntax/repoforge", strings.Repeat("A", 40), false, "non-hex"},
		{"non-hex commit", "https://github.com/FernSyntax/repoforge", strings.Repeat("g", 40), false, "non-hex"},
		{"metacharacters in commit", "https://github.com/FernSyntax/repoforge", strings.Repeat("a", 39) + ";", false, "non-hex"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			out, code := runActionShell(t, script, tc.url, tc.commit)
			if tc.wantOK {
				if code != 0 {
					t.Errorf("valid inputs rejected (exit %d):\n%s", code, out)
					return
				}
				// The fetch must have used the normalized URL and the
				// exact SHA — quoted, as single arguments.
				if !strings.Contains(out, " remote add origin https://github.com/FernSyntax/repoforge\n") {
					t.Errorf("fetch did not use the normalized canonical URL:\n%s", out)
				}
				if !strings.Contains(out, " fetch --quiet --depth 1 origin "+goodSHA+"\n") {
					t.Errorf("fetch did not use the exact commit SHA:\n%s", out)
				}
				return
			}
			if code == 0 {
				t.Errorf("invalid inputs accepted:\n%s", out)
				return
			}
			if !strings.Contains(out, tc.wantMsg) {
				t.Errorf("rejection does not name the problem %q:\n%s", tc.wantMsg, out)
			}
			if strings.Contains(out, "git remote add") || strings.Contains(out, "git fetch") {
				t.Errorf("git ran before validation rejected the input:\n%s", out)
			}
		})
	}
}

// releaseFixture builds a minimal git repository that release-check.sh
// can run against: the module path the script's ldflags expect, a CLI
// that prints the stamped version, and a CHANGELOG. The version-stamp
// check therefore executes for real instead of being stubbed out.
func releaseFixture(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	write := func(name, body string) {
		t.Helper()
		p := filepath.Join(dir, name)
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	write("go.mod", "module github.com/FernSyntax/repoforge\n\ngo 1.24\n")
	write("internal/version/version.go", "package version\n\nvar Version = \"0.0.0-dev\"\n")
	write("cmd/repoforge/main.go", "package main\n\nimport (\n\t\"fmt\"\n\n\t\"github.com/FernSyntax/repoforge/internal/version\"\n)\n\nfunc main() { fmt.Println(version.Version) }\n")
	write("CHANGELOG.md", "# Changelog\n\n## [9.9.9] - 2026-10-11\n\n### Added\n\n- Test fixture.\n")
	git := func(args ...string) string {
		t.Helper()
		cmd := exec.Command("git", args...)
		cmd.Dir = dir
		cmd.Env = append(os.Environ(),
			"GIT_AUTHOR_NAME=t", "GIT_AUTHOR_EMAIL=t@example.invalid",
			"GIT_COMMITTER_NAME=t", "GIT_COMMITTER_EMAIL=t@example.invalid",
			"GIT_CONFIG_NOSYSTEM=1", "HOME="+t.TempDir(),
			"GIT_CONFIG_COUNT=2",
			"GIT_CONFIG_KEY_0=commit.gpgsign", "GIT_CONFIG_VALUE_0=false",
			"GIT_CONFIG_KEY_1=tag.gpgsign", "GIT_CONFIG_VALUE_1=false")
		out, err := cmd.CombinedOutput()
		if err != nil {
			t.Fatalf("git %s: %v\n%s", strings.Join(args, " "), err, out)
		}
		return strings.TrimSpace(string(out))
	}
	git("init", "--quiet", "-b", "main", dir)
	git("add", "-A")
	git("commit", "--quiet", "-m", "chore(release): initialize")
	return dir
}

func runReleaseCheck(t *testing.T, dir, tag string) (string, int) {
	t.Helper()
	script, err := filepath.Abs(filepath.Join("..", "..", "scripts", "release-check.sh"))
	if err != nil {
		t.Fatal(err)
	}
	cmd := exec.Command("bash", script, tag)
	cmd.Dir = dir
	out, err := cmd.CombinedOutput()
	code := 0
	if ee, ok := err.(*exec.ExitError); ok {
		code = ee.ExitCode()
	} else if err != nil {
		t.Fatalf("running release-check.sh: %v", err)
	}
	return string(out), code
}

func gitIn(t *testing.T, dir string, args ...string) {
	t.Helper()
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	cmd.Env = append(os.Environ(),
		"GIT_AUTHOR_NAME=t", "GIT_AUTHOR_EMAIL=t@example.invalid",
		"GIT_COMMITTER_NAME=t", "GIT_COMMITTER_EMAIL=t@example.invalid",
		"GIT_CONFIG_NOSYSTEM=1", "HOME="+t.TempDir(),
		"GIT_CONFIG_COUNT=2",
		"GIT_CONFIG_KEY_0=commit.gpgsign", "GIT_CONFIG_VALUE_0=false",
		"GIT_CONFIG_KEY_1=tag.gpgsign", "GIT_CONFIG_VALUE_1=false")
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("git %s: %v\n%s", strings.Join(args, " "), err, out)
	}
}

// TestReleaseCheckAcceptsMainlineTag is the happy path: an annotated
// tag on a mainline commit with a changelog section and a correct stamp.
func TestReleaseCheckAcceptsMainlineTag(t *testing.T) {
	if testing.Short() {
		t.Skip("release-check builds the CLI; skip in short mode")
	}
	dir := releaseFixture(t)
	gitIn(t, dir, "tag", "-a", "v9.9.9", "-m", "v9.9.9")
	out, code := runReleaseCheck(t, dir, "v9.9.9")
	if code != 0 {
		t.Errorf("mainline annotated tag must pass (exit %d):\n%s", code, out)
	}
}

// TestReleaseCheckRejectsTagOffMainline is the regression test for
// releases published from commits the branch ruleset never reviewed: an
// annotated tag on an unmerged branch must fail, even with a changelog
// section and a correct stamp.
func TestReleaseCheckRejectsTagOffMainline(t *testing.T) {
	if testing.Short() {
		t.Skip("release-check builds the CLI; skip in short mode")
	}
	dir := releaseFixture(t)
	gitIn(t, dir, "checkout", "--quiet", "-b", "side")
	if err := os.WriteFile(filepath.Join(dir, "side.txt"), []byte("x\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	gitIn(t, dir, "add", "-A")
	gitIn(t, dir, "commit", "--quiet", "-m", "side work")
	gitIn(t, dir, "tag", "-a", "v9.9.9", "-m", "v9.9.9")
	out, code := runReleaseCheck(t, dir, "v9.9.9")
	if code == 0 {
		t.Errorf("tag on an unmerged branch must fail:\n%s", out)
	}
	if !strings.Contains(out, "not reachable from") {
		t.Errorf("rejection must name the ancestry problem:\n%s", out)
	}
}

// TestReleaseCheckRejectsLightweightTag proves the annotation rule.
func TestReleaseCheckRejectsLightweightTag(t *testing.T) {
	if testing.Short() {
		t.Skip("release-check builds the CLI; skip in short mode")
	}
	dir := releaseFixture(t)
	gitIn(t, dir, "tag", "v9.9.9")
	out, code := runReleaseCheck(t, dir, "v9.9.9")
	if code == 0 {
		t.Errorf("lightweight tag must fail:\n%s", out)
	}
	if !strings.Contains(out, "annotated tag object") {
		t.Errorf("rejection must name the annotation rule:\n%s", out)
	}
}

// TestReleaseCheckRejectsBadShapeAndMissingChangelog covers the
// remaining input validation: malformed versions and releases with no
// recorded section.
func TestReleaseCheckRejectsBadShapeAndMissingChangelog(t *testing.T) {
	if testing.Short() {
		t.Skip("release-check builds the CLI; skip in short mode")
	}
	dir := releaseFixture(t)
	gitIn(t, dir, "tag", "-a", "v9.9.9", "-m", "v9.9.9")
	for _, tag := range []string{"9.9.9", "v9.9", "v9.9.9-rc1", "latest"} {
		if out, code := runReleaseCheck(t, dir, tag); code == 0 {
			t.Errorf("malformed tag %q must fail:\n%s", tag, out)
		}
	}
	// An annotated mainline tag with no changelog section must reach the
	// changelog check and fail there — not earlier.
	gitIn(t, dir, "tag", "-a", "v1.2.3", "-m", "v1.2.3")
	if out, code := runReleaseCheck(t, dir, "v1.2.3"); code == 0 {
		t.Errorf("tag without a changelog section must fail:\n%s", out)
	} else if !strings.Contains(out, "CHANGELOG.md") {
		t.Errorf("rejection must name the changelog:\n%s", out)
	}
}

// workflowStepNames returns the ordered step names of the publish job.
func workflowStepNames(t *testing.T) []string {
	t.Helper()
	data, err := os.ReadFile(filepath.Join("..", "..", publicationWorkflowFile))
	if err != nil {
		t.Fatal(err)
	}
	var wf struct {
		Jobs map[string]struct {
			Steps []struct {
				Name string `yaml:"name"`
				Run  string `yaml:"run"`
			} `yaml:"steps"`
		} `yaml:"jobs"`
	}
	if err := yaml.Unmarshal(data, &wf); err != nil {
		t.Fatal(err)
	}
	var names []string
	for _, s := range wf.Jobs["publish"].Steps {
		names = append(names, s.Name)
	}
	return names
}

func stepIndex(names []string, want string) int {
	for i, n := range names {
		if n == want {
			return i
		}
	}
	return -1
}

// TestDuplicatePublicationIsIdempotent proves the guards that make a
// repeated run safe are present and ordered before any remote write:
// the initial path refuses a non-empty target, the routine path no-ops
// when synchronized, and the tag/release steps run only after the
// template's own CI is green.
func TestDuplicatePublicationIsIdempotent(t *testing.T) {
	data, err := os.ReadFile(filepath.Join("..", "..", publicationWorkflowFile))
	if err != nil {
		t.Fatal(err)
	}
	body := string(data)
	names := workflowStepNames(t)
	at := func(name string) int {
		i := stepIndex(names, name)
		if i < 0 {
			t.Fatalf("step %q not found in the publish job", name)
		}
		return i
	}

	// Guards precede writes.
	refuse := at("Refuse a target that is not empty")
	publish := at("Publish the initial root commit")
	if refuse > publish {
		t.Errorf("the emptiness refusal (step %d) runs after the root-commit push (step %d)", refuse, publish)
	}
	if !strings.Contains(body, "already synchronized") {
		t.Errorf("the routine path has no synchronized no-op guard; a duplicate run would open a duplicate PR")
	}
	wait := at("Wait for the template's hosted CI")
	tag := at("Publish the initial annotated tag and release")
	if wait > tag {
		t.Errorf("the tag/release step (step %d) runs before the CI wait (step %d); a red template would still get tagged", tag, wait)
	}
	verify := at("Verify the initial publication state")
	if tag > verify {
		t.Errorf("the final state check (step %d) runs before the tag/release step (step %d)", verify, tag)
	}
}

// goreleaserMatrix computes the expected archive set from .goreleaser.yaml:
// every (goos, goarch) pair minus ignores, with the per-OS archive format.
func goreleaserMatrix(t *testing.T) map[string]bool {
	t.Helper()
	data, err := os.ReadFile(filepath.Join("..", "..", ".goreleaser.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	var cfg struct {
		Builds []struct {
			Goos   []string `yaml:"goos"`
			Goarch []string `yaml:"goarch"`
			Ignore []struct {
				Goos   string `yaml:"goos"`
				Goarch string `yaml:"goarch"`
			} `yaml:"ignore"`
		} `yaml:"builds"`
		Archives []struct {
			Formats         []string `yaml:"formats"`
			FormatOverrides []struct {
				Goos    string   `yaml:"goos"`
				Formats []string `yaml:"formats"`
			} `yaml:"format_overrides"`
		} `yaml:"archives"`
	}
	if err := yaml.Unmarshal(data, &cfg); err != nil {
		t.Fatalf("cannot parse .goreleaser.yaml: %v", err)
	}
	if len(cfg.Builds) == 0 || len(cfg.Archives) == 0 {
		t.Fatal(".goreleaser.yaml has no builds or no archives")
	}
	b := cfg.Builds[0]
	ignored := map[string]bool{}
	for _, ig := range b.Ignore {
		ignored[ig.Goos+"/"+ig.Goarch] = true
	}
	extFor := map[string]string{}
	for _, o := range b.Goos {
		extFor[o] = cfg.Archives[0].Formats[0]
	}
	for _, fo := range cfg.Archives[0].FormatOverrides {
		extFor[fo.Goos] = fo.Formats[0]
	}
	out := map[string]bool{}
	for _, o := range b.Goos {
		for _, a := range b.Goarch {
			if ignored[o+"/"+a] {
				continue
			}
			ext, ok := extFor[o]
			if !ok {
				t.Fatalf("no archive format for GOOS %s", o)
			}
			out[o+"/"+a+"/"+ext] = true
		}
	}
	return out
}

// documentedAssets finds repoforge_<ver>_<os>_<arch>.<ext> examples in
// user-facing docs, normalizing the version away.
func documentedAssets(t *testing.T, paths ...string) []string {
	t.Helper()
	re := regexp.MustCompile(`repoforge_[0-9X.]+\.[0-9X.]+(?:\.[0-9X.]+)?_([A-Za-z0-9]+)_([A-Za-z0-9]+)\.(tar\.gz|zip)`)
	var found []string
	for _, p := range paths {
		data, err := os.ReadFile(filepath.Join("..", "..", p))
		if err != nil {
			t.Fatal(err)
		}
		for _, line := range strings.Split(string(data), "\n") {
			trimmed := strings.TrimSpace(line)
			if strings.HasPrefix(trimmed, "#") {
				continue
			}
			for _, m := range re.FindAllStringSubmatch(line, -1) {
				found = append(found, p+": "+m[1]+"/"+m[2]+"/"+m[3])
			}
		}
	}
	return found
}

// TestDocumentedAssetNamesMatchTheReleaseMatrix proves the install and
// verify examples name files the release actually produces. A wrong case
// (`Linux` for `linux`), a wrong arch per OS, or a wrong extension
// means a copy/pasted command fails on a correct release.
func TestDocumentedAssetNamesMatchTheReleaseMatrix(t *testing.T) {
	matrix := goreleaserMatrix(t)
	docs := []string{
		"README.md",
		"docs/development/release-process.md",
		"docs/security/release-integrity.md",
	}
	found := documentedAssets(t, docs...)
	if len(found) == 0 {
		t.Fatal("no documented asset examples found; the install procedure must name real files")
	}
	for _, f := range found {
		parts := strings.SplitN(f, ": ", 2)
		if !matrix[parts[1]] {
			t.Errorf("%s names an asset the release matrix never produces; wanted one of %v", f, sortedKeys(matrix))
		}
	}
}

func sortedKeys(m map[string]bool) []string {
	var out []string
	for k := range m {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}
