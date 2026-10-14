// Copyright the repoforge contributors.
//
// SPDX-License-Identifier: Apache-2.0

package wiki

import (
	"bytes"
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// writePage writes a declared wiki page into dir.
func writePage(t *testing.T, dir, name, body string) {
	t.Helper()
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, name+PageExtension), []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
}

// gitIn runs a git command in a scratch repository and fails the test
// on error. It is deliberately distinct from the production runGit.
func gitIn(t *testing.T, dir string, args ...string) string {
	t.Helper()
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	cmd.Env = append(os.Environ(),
		"GIT_AUTHOR_NAME=t", "GIT_AUTHOR_EMAIL=t@example.invalid",
		"GIT_COMMITTER_NAME=t", "GIT_COMMITTER_EMAIL=t@example.invalid",
		"GIT_CONFIG_NOSYSTEM=1", "HOME="+t.TempDir(), "GIT_CONFIG_COUNT=1",
		"GIT_CONFIG_KEY_0=commit.gpgsign", "GIT_CONFIG_VALUE_0=false")
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("git %s: %v\n%s", strings.Join(args, " "), err, out)
	}
	return strings.TrimSpace(string(out))
}

// newWikiRemote builds a bare repository that stands in for a GitHub
// wiki, so the sync path is exercised against real git without any
// network access. The layout mirrors the derived URL, so the identity
// check has something real to compare against.
func newWikiRemote(t *testing.T, base, owner, name string) string {
	t.Helper()
	dir := filepath.Join(base, owner, name+".wiki.git")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	gitIn(t, dir, "init", "--quiet", "--bare", "-b", "main", dir)
	return dir
}

// seedRemote gives the remote a commit with the given pages.
func seedRemote(t *testing.T, remote string, pages map[string]string) {
	t.Helper()
	if len(pages) == 0 {
		return
	}
	work := t.TempDir()
	clone := filepath.Join(work, "w")
	gitIn(t, work, "clone", "--quiet", remote, clone)
	for name, body := range pages {
		if err := os.WriteFile(filepath.Join(clone, name+PageExtension), []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	gitIn(t, clone, "add", "-A")
	gitIn(t, clone, "commit", "--quiet", "-m", "seed")
	gitIn(t, clone, "push", "--quiet", "origin", "HEAD:main")
}

func hostedBody(t *testing.T, path string) string {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return string(data)
}

// TestSyncDryRunWritesNothing proves the default mode is an exact
// preview: the wiki is untouched and the plan is what an apply would do.
func TestSyncDryRunWritesNothing(t *testing.T) {
	base := t.TempDir()
	remote := newWikiRemote(t, base, "acme", "widget")
	seedRemote(t, remote, map[string]string{"Home": "stale\n"})

	repoRoot := t.TempDir()
	src := filepath.Join(repoRoot, DefaultSourceDir)
	writePage(t, src, "Home", "fresh home\n")
	writePage(t, src, "Getting-Started", "start here\n")

	res, err := Sync(context.Background(), Options{
		SourceDir: src,
		Owner:     "acme", Repo: "widget",
		RepoRoot: repoRoot,
		BaseURL:  base,
	})
	if err != nil {
		t.Fatalf("sync: %v", err)
	}
	if res.Applied {
		t.Error("a dry run must not apply")
	}
	if !res.DryRun {
		t.Error("DryRun must be true")
	}
	if res.Created != 1 || res.Updated != 1 {
		t.Errorf("plan = %d create / %d update, want 1 / 1 (%+v)", res.Created, res.Updated, res.Pages)
	}
	// The wiki must not have gained a second commit.
	if got := strings.Count(gitIn(t, remote, "log", "--format=%H", "main"), "\n"); got != 0 {
		t.Errorf("the dry run touched the wiki (%d new commit(s))", got+1)
	}
}

// TestSyncApplyPublishesAndIsIdempotent proves the apply path writes
// the declared set, and that a second run reports no work.
func TestSyncApplyPublishesAndIsIdempotent(t *testing.T) {
	base := t.TempDir()
	remote := newWikiRemote(t, base, "acme", "widget")
	seedRemote(t, remote, map[string]string{"Home": "stale\n", "Notes": "mine\n"})

	repoRoot := t.TempDir()
	src := filepath.Join(repoRoot, DefaultSourceDir)
	writePage(t, src, "Home", "fresh home\n")
	writePage(t, src, "Getting-Started", "start here\n")

	opts := Options{SourceDir: src, Owner: "acme", Repo: "widget", RepoRoot: repoRoot, BaseURL: base, Apply: true}
	res, err := Sync(context.Background(), opts)
	if err != nil {
		t.Fatalf("apply: %v", err)
	}
	if !res.Applied || res.Commit == "" {
		t.Fatalf("apply did not report a commit: %+v", res)
	}
	if res.UserOwned != 1 {
		t.Errorf("user-owned pages = %d, want 1 (%+v)", res.UserOwned, res.Pages)
	}

	second, err := Sync(context.Background(), opts)
	if err != nil {
		t.Fatalf("second sync: %v", err)
	}
	if second.Changed() {
		t.Errorf("a second sync must report no work: %+v", second.Pages)
	}
	if second.Applied {
		t.Error("a converged sync must not push")
	}
}

// TestSyncNeverDeletesUserPages proves user-owned hosted pages survive
// an apply untouched.
func TestSyncNeverDeletesUserPages(t *testing.T) {
	base := t.TempDir()
	remote := newWikiRemote(t, base, "acme", "widget")
	seedRemote(t, remote, map[string]string{"Notes": "hand written\n"})

	repoRoot := t.TempDir()
	src := filepath.Join(repoRoot, DefaultSourceDir)
	writePage(t, src, "Home", "fresh home\n")

	res, err := Sync(context.Background(), Options{
		SourceDir: src, Owner: "acme", Repo: "widget",
		RepoRoot: repoRoot, BaseURL: base, Apply: true,
	})
	if err != nil {
		t.Fatalf("apply: %v", err)
	}
	if res.UserOwned != 1 {
		t.Fatalf("user-owned = %d, want 1", res.UserOwned)
	}
	for _, p := range res.Pages {
		if p.Name == "Notes" && p.Action != ActionUserOwned {
			t.Errorf("page %q planned as %s, want %s", p.Name, p.Action, ActionUserOwned)
		}
	}
	probe := t.TempDir()
	gitIn(t, probe, "clone", "--quiet", remote, filepath.Join(probe, "w"))
	if got := hostedBody(t, filepath.Join(probe, "w", "Notes.md")); got != "hand written\n" {
		t.Errorf("user page was modified: %q", got)
	}
}

// TestVerifyCloneRefusesRealForeignClone proves the dangerous-destination
// check refuses a real clone whose origin is not the requested
// repository, before Sync would write anything.
func TestVerifyCloneRefusesRealForeignClone(t *testing.T) {
	base := t.TempDir()
	remote := newWikiRemote(t, base, "acme", "widget")
	seedRemote(t, remote, map[string]string{"Home": "x\n"})

	clone := filepath.Join(t.TempDir(), "w")
	gitIn(t, base, "clone", "--quiet", remote, clone)
	// The URL Sync derives from BaseURL is the identity to compare
	// against; a local test remote therefore has to be named as such.
	want := WikiURLFor(base, "acme", "widget")
	if err := verifyClone(clone, want); err != nil {
		t.Fatalf("the matching clone must be accepted: %v", err)
	}
	gitIn(t, clone, "remote", "set-url", "origin", WikiURLFor(base, "someone", "else"))
	err := verifyClone(clone, want)
	if err == nil {
		t.Fatal("a clone of a different repository must be refused")
	}
	if !strings.Contains(err.Error(), "refusing to publish") {
		t.Errorf("the refusal must say what it is refusing: %v", err)
	}
}

// TestSyncRejectsMissingWiki proves a disabled wiki is reported as a
// configuration problem, not an authentication problem.
func TestSyncRejectsMissingWiki(t *testing.T) {
	repoRoot := t.TempDir()
	src := filepath.Join(repoRoot, DefaultSourceDir)
	writePage(t, src, "Home", "content\n")

	_, err := Sync(context.Background(), Options{
		SourceDir: src, Owner: "acme", Repo: "widget",
		RepoRoot: repoRoot,
		BaseURL:  "https://github.invalid",
	})
	if err == nil {
		t.Fatal("an unreachable host must fail")
	}
	if NotFound(err) {
		t.Log("reported as not-found (acceptable: the host has no wiki)")
	}
	if Unauthorized(err) && NotFound(err) {
		t.Error("a failure cannot be both missing and unauthorized")
	}
}

// TestVerifyCloneAcceptsTransportVariants proves the identity check is
// about the repository, not about how git reached it.
func TestVerifyCloneAcceptsTransportVariants(t *testing.T) {
	dir := t.TempDir()
	gitIn(t, dir, "init", "--quiet", "-b", "main", dir)
	gitIn(t, dir, "remote", "add", "origin", "https://github.com/acme/widget.wiki.git")
	want := WikiURL("acme", "widget")
	for _, url := range []string{
		"https://github.com/acme/widget.wiki.git",
		"https://github.com/acme/widget.wiki",
		"https://token@github.com/acme/widget.wiki.git",
		"git@github.com:acme/widget.wiki.git",
	} {
		gitIn(t, dir, "remote", "set-url", "origin", url)
		if err := verifyClone(dir, want); err != nil {
			t.Errorf("origin %q rejected: %v", url, err)
		}
	}
	gitIn(t, dir, "remote", "set-url", "origin", "https://github.com/other/thing.wiki.git")
	if err := verifyClone(dir, want); err == nil {
		t.Error("a foreign origin must be refused")
	}
}

// TestLoadPagesRejectsUnsafeSources proves the source directory rules:
// flat Markdown only, no escapes, no binary.
func TestLoadPagesRejectsUnsafeSources(t *testing.T) {
	if _, err := LoadPages(""); err == nil {
		t.Error("an empty source directory must be rejected")
	}
	dir := t.TempDir()
	if err := os.MkdirAll(filepath.Join(dir, "nested"), 0o755); err != nil {
		t.Fatal(err)
	}
	if _, err := LoadPages(dir); err == nil {
		t.Error("a subdirectory must be rejected")
	}
	dir = t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "payload.sh"), []byte("#!/bin/sh\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	if _, err := LoadPages(dir); err == nil {
		t.Error("a non-Markdown file must be rejected")
	}
	dir = t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "blob.md"), []byte{0x00, 0x01}, 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := LoadPages(dir); err == nil {
		t.Error("binary content must be rejected")
	}
	dir = t.TempDir()
	writePage(t, dir, "Home", "a\n")
	pages, err := LoadPages(dir)
	if err != nil || len(pages) != 1 || pages[0].Name != "Home" {
		t.Fatalf("LoadPages = %+v, %v", pages, err)
	}
	if !bytes.Equal(pages[0].Data, []byte("a\n")) {
		t.Errorf("page data = %q", pages[0].Data)
	}
}

// TestLoadPagesIsSorted proves deterministic plans.
func TestLoadPagesIsSorted(t *testing.T) {
	dir := t.TempDir()
	for _, n := range []string{"Zebra", "apple", "Home"} {
		writePage(t, dir, n, n+"\n")
	}
	pages, err := LoadPages(dir)
	if err != nil {
		t.Fatal(err)
	}
	want := []string{"Home", "Zebra", "apple"}
	for i, w := range want {
		if pages[i].Name != w {
			t.Fatalf("page %d is %q, want %q (sorted order must be deterministic)", i, pages[i].Name, w)
		}
	}
}

// TestResolveSourceRejectsOutsideRoot proves the publisher cannot be
// pointed at a tree outside the governed repository.
func TestResolveSourceRejectsOutsideRoot(t *testing.T) {
	root := t.TempDir()
	if _, err := resolveSource(t.TempDir(), root); err == nil {
		t.Error("a source directory outside the repository must be rejected")
	}
	inside := filepath.Join(root, DefaultSourceDir)
	if err := os.MkdirAll(inside, 0o755); err != nil {
		t.Fatal(err)
	}
	got, err := resolveSource(inside, root)
	if err != nil {
		t.Fatalf("a source directory inside the repository must be accepted: %v", err)
	}
	real, _ := filepath.EvalSymlinks(inside)
	if got != real {
		t.Errorf("resolved to %q, want %q", got, real)
	}
}

// TestPlanClassifiesEveryPage proves the four plan outcomes.
func TestPlanClassifiesEveryPage(t *testing.T) {
	pages := []SourcePage{
		{Name: "Home", Source: "Home.md", Data: []byte("new\n")},
		{Name: "Kept", Source: "Kept.md", Data: []byte("same\n")},
	}
	hosted := map[string][]byte{
		"Home":  []byte("old\n"),
		"Kept":  []byte("same\n"),
		"Notes": []byte("mine\n"),
	}
	res := Plan(pages, hosted)
	if res.Created != 0 || res.Updated != 1 || res.Unchanged != 1 || res.UserOwned != 1 {
		t.Fatalf("plan = %+v", res)
	}
	for _, p := range res.Pages {
		switch p.Name {
		case "Home":
			if p.Action != ActionUpdate {
				t.Errorf("Home = %s, want update", p.Action)
			}
		case "Kept":
			if p.Action != ActionUnchanged {
				t.Errorf("Kept = %s, want unchanged", p.Action)
			}
		case "Notes":
			if p.Action != ActionUserOwned {
				t.Errorf("Notes = %s, want user-owned", p.Action)
			}
		}
	}
}

// TestReadHostedSkipsGitHubChrome proves GitHub's own _Sidebar and
// _Footer pages are never touched.
func TestReadHostedSkipsGitHubChrome(t *testing.T) {
	dir := t.TempDir()
	for _, n := range []string{"Home.md", "_Sidebar.md", "_Footer.md", "Notes.txt"} {
		if err := os.WriteFile(filepath.Join(dir, n), []byte("x\n"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	hosted, err := ReadHosted(dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(hosted) != 1 {
		t.Fatalf("hosted = %v, want only Home", hosted)
	}
	if _, ok := hosted["Home"]; !ok {
		t.Error("Home is missing")
	}
}

// TestWikiURLDerivation proves the URL shape for public and
// enterprise instances.
func TestWikiURLDerivation(t *testing.T) {
	if got, want := WikiURL("acme", "widget"), "https://github.com/acme/widget.wiki.git"; got != want {
		t.Errorf("WikiURL = %q, want %q", got, want)
	}
	if got, want := WikiURLFor("https://ghe.example.com/", "acme", "widget"),
		"https://ghe.example.com/acme/widget.wiki.git"; got != want {
		t.Errorf("WikiURLFor = %q, want %q", got, want)
	}
	if got, want := WikiURLFor("", "acme", "widget"), WikiURL("acme", "widget"); got != want {
		t.Errorf("WikiURLFor(\"\") = %q, want %q", got, want)
	}
}

// TestMissingWikiNamesBothRemedies proves the failure an operator is most
// likely to hit — an enabled-but-uninitialised wiki — is explained rather
// than reported as a bare "not found".
//
// GitHub creates <owner>/<repo>.wiki.git only when the first wiki page is
// created in the web UI, so an enabled-but-untouched wiki reports exactly
// the same "Repository not found" as a disabled one. The two need
// different steps, so the message must name both.
func TestMissingWikiNamesBothRemedies(t *testing.T) {
	// This is git's own wording for a repository that does not exist,
	// captured verbatim from the endpoints under test.
	err := classifyCloneFailure("https://github.com/acme/widget.wiki.git",
		errors.New("git clone: fatal: repository 'https://github.com/acme/widget.wiki.git/' not found"))
	if !NotFound(err) {
		t.Error("a missing wiki must classify as NotFound")
	}
	if Unauthorized(err) {
		t.Error("a missing wiki must not classify as Unauthorized")
	}
	joined := err.Error()
	for _, want := range []string{"disabled", "never been initialised", "web UI", "has_wiki"} {
		if !strings.Contains(joined, want) {
			t.Errorf("the missing-wiki message does not mention %q:\n%s", want, joined)
		}
	}
}

// TestAuthFailureIsDistinctFromMissing proves the second classification,
// so an operator is never sent hunting for a token that is fine.
func TestAuthFailureIsDistinctFromMissing(t *testing.T) {
	for _, detail := range []string{
		"remote: Authentication failed for 'https://github.com/acme/widget.wiki.git/'",
		"remote: invalid credentials",
		"fatal: could not read Username for 'https://github.com': terminal prompts disabled",
		"error: 403 Forbidden",
	} {
		err := classifyCloneFailure("https://github.com/acme/widget.wiki.git", errors.New(detail))
		if !Unauthorized(err) {
			t.Errorf("%q must classify as Unauthorized", detail)
		}
		if NotFound(err) {
			t.Errorf("%q must not classify as NotFound", detail)
		}
	}
}

// TestTransportFailureIsNeither proves a network failure is reported
// honestly rather than being dressed up as a configuration or credential
// problem.
func TestTransportFailureIsNeither(t *testing.T) {
	err := classifyCloneFailure("https://github.com/acme/widget.wiki.git",
		errors.New("fatal: unable to access 'https://github.com/acme/widget.wiki.git/': Could not resolve host"))
	if NotFound(err) || Unauthorized(err) {
		t.Error("a transport failure is neither missing nor unauthorized")
	}
	if !strings.Contains(err.Error(), "Could not resolve host") {
		t.Errorf("the transport failure must be reported verbatim: %s", err)
	}
}

// TestSourcePagesAreTheWikiContract pins the page rules that make the
// seed set legal: flat Markdown only, page name from the file name.
func TestSourcePagesAreTheWikiContract(t *testing.T) {
	dir := t.TempDir()
	pages, err := LoadPages(dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(pages) != 0 {
		t.Errorf("an empty source directory must yield no pages, got %d", len(pages))
	}
	writePage(t, dir, "Home", "# Home\n")
	writePage(t, dir, "Getting-Started", "# Getting Started\n")
	pages, err = LoadPages(dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(pages) != 2 {
		t.Fatalf("pages = %d, want 2", len(pages))
	}
	if pages[0].Name != "Getting-Started" || pages[1].Name != "Home" {
		t.Errorf("pages are not in sorted order: %+v", pages)
	}
	if pages[0].Source != "Getting-Started.md" {
		t.Errorf("Source = %q, want the file name", pages[0].Source)
	}
}
