package validate

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// releaseWorkflowPath is the engine release workflow under test.
const releaseWorkflowPath = ".github/workflows/release.yml"

// releaseStep extracts a named step's shell from the release workflow.
// The steps under test take no defaults from GitHub: every variable
// they read (GITHUB_REF_NAME, GITHUB_REPOSITORY, RUNNER_TEMP,
// GITHUB_OUTPUT) is set explicitly by the test.
func releaseStep(t *testing.T, name string) string {
	t.Helper()
	data, err := os.ReadFile(filepath.Join("..", "..", releaseWorkflowPath))
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
	t.Fatalf("step %q not found in %s", name, releaseWorkflowPath)
	return ""
}

// releaseGHStub writes a stub gh that emulates the release API surface
// the state machine touches. Behavior is driven by files, so each test
// declares a world and the stub answers consistently within it:
//
//	$STUB/release-exists   — file presence means `gh release view` succeeds
//	$STUB/is-draft         — file content is echoed for --json isDraft
//	$STUB/assets            — file listing asset names, one per line,
//	                          served for --json assets
//	$STUB/attest-exit      — file content is the exit code for
//	                          `gh attestation verify`
//	$STUB/calls.log        — every invocation is appended here
//	$STUB/state            — mutated by `release edit --draft=false` and
//	                          `release create --draft` to model the state
//	                          transitions the workflow performs
//
// Anything unrecognized fails loudly instead of succeeding silently.
func releaseGHStub(t *testing.T, setup func(stub string)) (bin, stub string) {
	t.Helper()
	bin = t.TempDir()
	stub = t.TempDir()
	script := `#!/bin/sh
STUB=${STUB_DIR:?}
echo "gh $*" >> "$STUB/calls.log"
case "$*" in
  *"release view"*"--json isDraft"*)
    if [ -f "$STUB/state" ]; then cat "$STUB/state"; else cat "$STUB/is-draft"; fi
    exit 0 ;;
  *"release view"*"--json assets"*)
    # the workflow always passes --jq '.assets[].name', so serve bare names
    grep -v '^[[:space:]]*$' "$STUB/assets" ;;
  *"release view"*)
    if [ -f "$STUB/release-exists" ]; then exit 0; else exit 1; fi ;;
  *"release create"*"--draft"*)
    touch "$STUB/release-exists"; printf true > "$STUB/state"; exit 0 ;;
  *"release upload"*)
    # record each uploaded basename: release upload TAG f1 f2 ...
    shift 3
    for f in "$@"; do printf '%s\n' "$(basename "$f")" >> "$STUB/uploaded"; done
    exit 0 ;;
  *"release edit"*"--draft=false"*)
    if [ ! -f "$STUB/no-flip" ]; then printf false > "$STUB/state"; fi
    exit 0 ;;
  *"attestation verify"*)
    exit "$(cat "$STUB/attest-exit")" ;;
  *)
    echo "stub gh: unexpected invocation: gh $*" >&2
    exit 99 ;;
esac
`
	if err := os.WriteFile(filepath.Join(bin, "gh"), []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	setup(stub)
	return bin, stub
}

// runReleaseStep executes a workflow step with the stub gh first on
// PATH. GITHUB_REF_NAME, GITHUB_REPOSITORY, RUNNER_TEMP, and
// GITHUB_OUTPUT are always set; the caller supplies the working
// directory, which doubles as the git repository for steps that read
// git state. A non-empty notesBody is seeded at
// $RUNNER_TEMP/notes/notes.md, standing in for the downloaded
// release-notes artifact the download step places there.
func runReleaseStep(t *testing.T, script, dir, bin, stub, tag, notesBody string) (string, int) {
	t.Helper()
	out := filepath.Join(t.TempDir(), "github-output")
	if err := os.WriteFile(out, nil, 0o644); err != nil {
		t.Fatal(err)
	}
	runnerTemp := t.TempDir()
	if notesBody != "" {
		if err := os.MkdirAll(filepath.Join(runnerTemp, "notes"), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(runnerTemp, "notes", "notes.md"), []byte(notesBody), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	cmd := exec.Command("bash", "-e", "-c", script)
	cmd.Dir = dir
	cmd.Env = append(os.Environ(),
		"PATH="+bin+":"+os.Getenv("PATH"),
		"STUB_DIR="+stub,
		"GITHUB_REF_NAME="+tag,
		"GITHUB_REPOSITORY=FernSyntax/repoforge",
		"RUNNER_TEMP="+runnerTemp,
		"GITHUB_OUTPUT="+out)
	bs, err := cmd.CombinedOutput()
	code := 0
	if ee, ok := err.(*exec.ExitError); ok {
		code = ee.ExitCode()
	} else if err != nil {
		t.Fatalf("running release step: %v", err)
	}
	return string(bs), code
}

// releaseRepo creates a git repository with an annotated tag on HEAD,
// the shape the resolve step expects for the happy path.
func releaseRepo(t *testing.T, tag string) string {
	t.Helper()
	dir := t.TempDir()
	gitIn(t, dir, "init", "--quiet", "-b", "main", dir)
	mustWrite := func(name, body string) {
		t.Helper()
		if err := os.WriteFile(filepath.Join(dir, name), []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	mustWrite("f.txt", "x\n")
	gitIn(t, dir, "add", "-A")
	gitIn(t, dir, "commit", "--quiet", "-m", "release commit")
	gitIn(t, dir, "tag", "-a", tag, "-m", tag)
	return dir
}

// TestReleaseStateAbsentProceedsToDraft proves a fresh run with no
// release at all passes state resolution: everything below creates
// state, and there is nothing to conflict with.
func TestReleaseStateAbsentProceedsToDraft(t *testing.T) {
	script := releaseStep(t, "Resolve the release state")
	dir := releaseRepo(t, "v9.9.9")
	bin, stub := releaseGHStub(t, func(string) {})
	out, code := runReleaseStep(t, script, dir, bin, stub, "v9.9.9", "")
	_ = out
	if code != 0 {
		t.Errorf("absent release must proceed (exit %d):\n%s", code, out)
	}
	if !strings.Contains(out, "a draft will be staged") {
		t.Errorf("absent release must say a draft will be staged:\n%s", out)
	}
}

// TestReleaseStateResumesDraftSameCommit proves the resume path: a draft
// whose tag still dereferences to the checked-out commit is adopted,
// not recreated.
func TestReleaseStateResumesDraftSameCommit(t *testing.T) {
	script := releaseStep(t, "Resolve the release state")
	dir := releaseRepo(t, "v9.9.9")
	bin, stub := releaseGHStub(t, func(stub string) {
		mustWriteFile(t, filepath.Join(stub, "release-exists"), "")
		mustWriteFile(t, filepath.Join(stub, "is-draft"), "true")
	})
	out, code := runReleaseStep(t, script, dir, bin, stub, "v9.9.9", "")
	if code != 0 {
		t.Errorf("resumable draft must proceed (exit %d):\n%s", code, out)
	}
	if !strings.Contains(out, "resuming draft") {
		t.Errorf("resumable draft must be reported as resumed:\n%s", out)
	}
}

// TestReleaseStateRefusesDraftOtherCommit proves a draft is never
// adopted when its tag moved: the tag points at an older commit while
// HEAD advanced, which is contradictory state for a human, not a
// resume.
func TestReleaseStateRefusesDraftOtherCommit(t *testing.T) {
	script := releaseStep(t, "Resolve the release state")
	dir := releaseRepo(t, "v9.9.9")
	mustWriteFile(t, filepath.Join(dir, "later.txt"), "y\n")
	gitIn(t, dir, "add", "-A")
	gitIn(t, dir, "commit", "--quiet", "-m", "later commit")
	bin, stub := releaseGHStub(t, func(stub string) {
		mustWriteFile(t, filepath.Join(stub, "release-exists"), "")
		mustWriteFile(t, filepath.Join(stub, "is-draft"), "true")
	})
	out, code := runReleaseStep(t, script, dir, bin, stub, "v9.9.9", "")
	if code == 0 {
		t.Errorf("draft from a different source commit must fail:\n%s", out)
	}
	if !strings.Contains(out, "different source commit") {
		t.Errorf("refusal must name the source-commit mismatch:\n%s", out)
	}
}

// TestReleaseStateRefusesPublishedRelease proves the immutability rule:
// a tag with a public release ends the run before anything is touched.
func TestReleaseStateRefusesPublishedRelease(t *testing.T) {
	script := releaseStep(t, "Resolve the release state")
	dir := releaseRepo(t, "v9.9.9")
	bin, stub := releaseGHStub(t, func(stub string) {
		mustWriteFile(t, filepath.Join(stub, "release-exists"), "")
		mustWriteFile(t, filepath.Join(stub, "is-draft"), "false")
	})
	out, code := runReleaseStep(t, script, dir, bin, stub, "v9.9.9", "")
	if code == 0 {
		t.Errorf("published release must fail closed:\n%s", out)
	}
	if !strings.Contains(out, "already published") {
		t.Errorf("refusal must name immutability:\n%s", out)
	}
}

// TestReleaseEnsureDraftSkipsWhenPresent proves the ensure step never
// recreates: with a release present it exits before reaching create.
func TestReleaseEnsureDraftSkipsWhenPresent(t *testing.T) {
	script := releaseStep(t, "Ensure the draft release exists")
	dir := releaseRepo(t, "v9.9.9")
	bin, stub := releaseGHStub(t, func(stub string) {
		mustWriteFile(t, filepath.Join(stub, "release-exists"), "")
	})
	out, code := runReleaseStep(t, script, dir, bin, stub, "v9.9.9", "")
	if code != 0 {
		t.Errorf("present draft must be skipped cleanly (exit %d):\n%s", code, out)
	}
	if calls, _ := os.ReadFile(filepath.Join(stub, "calls.log")); strings.Contains(string(calls), "release create") {
		t.Errorf("present draft must not be recreated:\n%s", calls)
	}
}

// TestReleaseEnsureDraftCreatesWithDraftFlag proves a fresh run stages
// a draft — never a public release.
func TestReleaseEnsureDraftCreatesWithDraftFlag(t *testing.T) {
	script := releaseStep(t, "Ensure the draft release exists")
	dir := releaseRepo(t, "v9.9.9")
	bin, stub := releaseGHStub(t, func(string) {})
	out, code := runReleaseStep(t, script, dir, bin, stub, "v9.9.9", "notes\n")
	if code != 0 {
		t.Errorf("absent release must be staged (exit %d):\n%s", code, out)
	}
	calls, _ := os.ReadFile(filepath.Join(stub, "calls.log"))
	if !strings.Contains(string(calls), "--draft") {
		t.Errorf("fresh staging must create a draft:\n%s", calls)
	}
}

// releaseDist creates a dist/ tree with real files and a matching
// checksums.txt. Like GoReleaser, checksums.txt lists every archive
// but never itself.
func releaseDist(t *testing.T, dir string, names ...string) {
	t.Helper()
	d := filepath.Join(dir, "dist")
	if err := os.MkdirAll(d, 0o755); err != nil {
		t.Fatal(err)
	}
	var lines []string
	for _, n := range names {
		mustWriteFile(t, filepath.Join(d, n), "payload:"+n+"\n")
	}
	for _, n := range names {
		if n == "checksums.txt" {
			continue
		}
		lines = append(lines, sumFile(t, filepath.Join(d, n))+"  "+n)
	}
	mustWriteFile(t, filepath.Join(d, "checksums.txt"), strings.Join(lines, "\n")+"\n")
}

func sumFile(t *testing.T, path string) string {
	t.Helper()
	cmd := exec.Command("sha256sum", path)
	cmd.Env = append(os.Environ(), "PATH=/usr/bin:/bin")
	out, err := cmd.Output()
	if err != nil {
		t.Fatal(err)
	}
	return strings.Fields(string(out))[0]
}

// TestReleaseUploadSkipsPresentAssets proves a retry never overwrites:
// only names absent from the release are uploaded, with no --clobber.
func TestReleaseUploadSkipsPresentAssets(t *testing.T) {
	script := releaseStep(t, "Upload the missing release assets")
	dir := releaseRepo(t, "v9.9.9")
	files := []string{"checksums.txt", "a.tar.gz", "a.tar.gz.sbom.json"}
	releaseDist(t, dir, files...)
	bin, stub := releaseGHStub(t, func(stub string) {
		mustWriteFile(t, filepath.Join(stub, "release-exists"), "")
		mustWriteFile(t, filepath.Join(stub, "assets"), "checksums.txt\n")
	})
	out, code := runReleaseStep(t, script, dir, bin, stub, "v9.9.9", "")
	if code != 0 {
		t.Errorf("partial upload must succeed (exit %d):\n%s", code, out)
	}
	uploaded, _ := os.ReadFile(filepath.Join(stub, "uploaded"))
	for _, want := range []string{"a.tar.gz", "a.tar.gz.sbom.json"} {
		if !strings.Contains(string(uploaded), want) {
			t.Errorf("missing asset %s was not uploaded:\n%s", want, uploaded)
		}
	}
	if strings.Contains(string(uploaded), "checksums.txt") {
		t.Errorf("present asset checksums.txt was re-uploaded (overwrite risk):\n%s", uploaded)
	}
	calls, _ := os.ReadFile(filepath.Join(stub, "calls.log"))
	if strings.Contains(string(calls), "--clobber") {
		t.Errorf("upload must never pass --clobber:\n%s", calls)
	}
}

// TestReleaseVerifyAcceptsCompleteStagedRelease is the happy path: the
// exact set matches, checksums verify for real, every archive's
// attestation verifies, and the release is still a draft.
func TestReleaseVerifyAcceptsCompleteStagedRelease(t *testing.T) {
	script := releaseStep(t, "Verify the staged release completely")
	dir := releaseRepo(t, "v9.9.9")
	files := []string{"checksums.txt", "a.tar.gz", "a.tar.gz.sbom.json"}
	releaseDist(t, dir, files...)
	bin, stub := releaseGHStub(t, func(stub string) {
		mustWriteFile(t, filepath.Join(stub, "assets"), "checksums.txt\na.tar.gz\na.tar.gz.sbom.json\n")
		mustWriteFile(t, filepath.Join(stub, "is-draft"), "true")
		mustWriteFile(t, filepath.Join(stub, "attest-exit"), "0")
	})
	out, code := runReleaseStep(t, script, dir, bin, stub, "v9.9.9", "")
	if code != 0 {
		t.Errorf("complete staged release must verify (exit %d):\n%s", code, out)
	}
}

// TestReleaseVerifyRejectsMissingAsset proves an incomplete upload
// fails instead of publishing short.
func TestReleaseVerifyRejectsMissingAsset(t *testing.T) {
	script := releaseStep(t, "Verify the staged release completely")
	dir := releaseRepo(t, "v9.9.9")
	releaseDist(t, dir, "checksums.txt", "a.tar.gz", "a.tar.gz.sbom.json")
	bin, stub := releaseGHStub(t, func(stub string) {
		mustWriteFile(t, filepath.Join(stub, "assets"), "checksums.txt\na.tar.gz\n")
		mustWriteFile(t, filepath.Join(stub, "is-draft"), "true")
		mustWriteFile(t, filepath.Join(stub, "attest-exit"), "0")
	})
	out, code := runReleaseStep(t, script, dir, bin, stub, "v9.9.9", "")
	if code == 0 {
		t.Errorf("missing asset must fail:\n%s", out)
	}
	if !strings.Contains(out, "asset set mismatch") {
		t.Errorf("rejection must name the mismatch:\n%s", out)
	}
}

// TestReleaseVerifyRejectsExtraAsset proves an unexpected file on the
// release fails instead of being silently adopted.
func TestReleaseVerifyRejectsExtraAsset(t *testing.T) {
	script := releaseStep(t, "Verify the staged release completely")
	dir := releaseRepo(t, "v9.9.9")
	releaseDist(t, dir, "checksums.txt", "a.tar.gz", "a.tar.gz.sbom.json")
	bin, stub := releaseGHStub(t, func(stub string) {
		mustWriteFile(t, filepath.Join(stub, "assets"), "checksums.txt\na.tar.gz\na.tar.gz.sbom.json\nsurprise.zip\n")
		mustWriteFile(t, filepath.Join(stub, "is-draft"), "true")
		mustWriteFile(t, filepath.Join(stub, "attest-exit"), "0")
	})
	out, code := runReleaseStep(t, script, dir, bin, stub, "v9.9.9", "")
	if code == 0 {
		t.Errorf("extra asset must fail:\n%s", out)
	}
}

// TestReleaseVerifyRejectsBadChecksum proves corruption fails even when
// the file list is exactly right.
func TestReleaseVerifyRejectsBadChecksum(t *testing.T) {
	script := releaseStep(t, "Verify the staged release completely")
	dir := releaseRepo(t, "v9.9.9")
	releaseDist(t, dir, "checksums.txt", "a.tar.gz", "a.tar.gz.sbom.json")
	mustWriteFile(t, filepath.Join(dir, "dist", "a.tar.gz"), "CORRUPTED\n")
	bin, stub := releaseGHStub(t, func(stub string) {
		mustWriteFile(t, filepath.Join(stub, "assets"), "checksums.txt\na.tar.gz\na.tar.gz.sbom.json\n")
		mustWriteFile(t, filepath.Join(stub, "is-draft"), "true")
		mustWriteFile(t, filepath.Join(stub, "attest-exit"), "0")
	})
	out, code := runReleaseStep(t, script, dir, bin, stub, "v9.9.9", "")
	if code == 0 {
		t.Errorf("corrupt archive must fail:\n%s", out)
	}
}

// TestReleaseVerifyRejectsFailedAttestation proves a missing or invalid
// attestation blocks publication while the release is still a draft.
func TestReleaseVerifyRejectsFailedAttestation(t *testing.T) {
	script := releaseStep(t, "Verify the staged release completely")
	dir := releaseRepo(t, "v9.9.9")
	releaseDist(t, dir, "checksums.txt", "a.tar.gz", "a.tar.gz.sbom.json")
	bin, stub := releaseGHStub(t, func(stub string) {
		mustWriteFile(t, filepath.Join(stub, "assets"), "checksums.txt\na.tar.gz\na.tar.gz.sbom.json\n")
		mustWriteFile(t, filepath.Join(stub, "is-draft"), "true")
		mustWriteFile(t, filepath.Join(stub, "attest-exit"), "1")
	})
	out, code := runReleaseStep(t, script, dir, bin, stub, "v9.9.9", "")
	if code == 0 {
		t.Errorf("failed attestation must block publication:\n%s", out)
	}
}

// TestReleaseVerifyRejectsPublishedRelease proves the verify step
// re-checks draft status: a release that flipped public underneath the
// run is not waved through.
func TestReleaseVerifyRejectsPublishedRelease(t *testing.T) {
	script := releaseStep(t, "Verify the staged release completely")
	dir := releaseRepo(t, "v9.9.9")
	releaseDist(t, dir, "checksums.txt", "a.tar.gz", "a.tar.gz.sbom.json")
	bin, stub := releaseGHStub(t, func(stub string) {
		mustWriteFile(t, filepath.Join(stub, "assets"), "checksums.txt\na.tar.gz\na.tar.gz.sbom.json\n")
		mustWriteFile(t, filepath.Join(stub, "is-draft"), "false")
		mustWriteFile(t, filepath.Join(stub, "attest-exit"), "0")
	})
	out, code := runReleaseStep(t, script, dir, bin, stub, "v9.9.9", "")
	if code == 0 {
		t.Errorf("non-draft at verify time must fail:\n%s", out)
	}
}

// TestReleasePublishFlipsDraftAndConfirms proves publication is explicit
// and confirmed: --draft=false is passed, and a release that stays a
// draft fails instead of being reported as published.
func TestReleasePublishFlipsDraftAndConfirms(t *testing.T) {
	script := releaseStep(t, "Publish the release")
	dir := releaseRepo(t, "v9.9.9")
	newDraft := func(published bool) (string, string) {
		bin, stub := releaseGHStub(t, func(stub string) {
			// every publish starts from a staged draft; the flip case
			// transitions it via `release edit`, the stuck case does not
			mustWriteFile(t, filepath.Join(stub, "release-exists"), "")
			mustWriteFile(t, filepath.Join(stub, "is-draft"), "true")
			if !published {
				mustWriteFile(t, filepath.Join(stub, "no-flip"), "")
			}
		})
		out, code := runReleaseStep(t, script, dir, bin, stub, "v9.9.9", "")
		calls, _ := os.ReadFile(filepath.Join(stub, "calls.log"))
		return out, fmt.Sprintf("%d\n%s", code, calls)
	}
	out, result := newDraft(true)
	if code := strings.SplitN(result, "\n", 2)[0]; code != "0" {
		t.Fatalf("publish must succeed when the draft flips (exit %s):\n%s", code, out)
	}
	if !strings.Contains(result, "--draft=false") {
		t.Errorf("publish must pass --draft=false explicitly:\n%s", result)
	}
	out2, result2 := newDraft(false)
	if code := strings.SplitN(result2, "\n", 2)[0]; code != "1" {
		t.Errorf("a release that stays a draft must fail (exit %s):\n%s", code, out2)
	}
}

func mustWriteFile(t *testing.T, path, body string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
}
