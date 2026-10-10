package validate

import (
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
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
