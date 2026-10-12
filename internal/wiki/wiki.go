// Copyright the repoforge contributors.
//
// SPDX-License-Identifier: Apache-2.0

// Package wiki synchronizes a declared set of Markdown pages into a
// GitHub repository wiki.
//
// The wiki is a git repository GitHub serves at
// https://github.com/{owner}/{repo}.wiki.git. RepoForge treats it as
// generated surface: the canonical page set lives in the repository's
// docs/wiki/ directory, and sync converges the hosted wiki to it.
//
// The safety contract mirrors the template builder:
//
//   - The default run is a dry run. Nothing is written, nothing is
//     pushed, and the plan is printed instead.
//   - Pages outside the declared set are user-owned: reported, never
//     modified, and never deleted.
//   - Pushes are never forced. A rejected push means someone else
//     advanced the wiki, and the correct answer is to re-plan, not to
//     overwrite their work.
//   - A missing or disabled wiki is distinguished from an
//     authentication failure, because the two have different remedies.
//   - The credential is passed to git through the environment, never
//     through a URL, a file, or the argument list.
package wiki

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
)

// PageExtension is the only source-file extension RepoForge
// publishes. GitHub renders wiki pages from Markdown, and restricting
// the set keeps binary or executable material out of the wiki.
const PageExtension = ".md"

// HomePage is the wiki landing page GitHub renders at /wiki.
const HomePage = "Home"

// DefaultSourceDir is the canonical wiki source directory relative to
// a repository root.
const DefaultSourceDir = "docs/wiki"

// DefaultBaseURL is the public GitHub instance.
const DefaultBaseURL = "https://github.com"

// Action classifies one planned wiki change.
type Action string

const (
	// ActionCreate adds a page the wiki does not have.
	ActionCreate Action = "create"
	// ActionUpdate replaces a page whose hosted content differs from
	// the declared content.
	ActionUpdate Action = "update"
	// ActionUnchanged needs no work.
	ActionUnchanged Action = "unchanged"
	// ActionUserOwned is a hosted page outside the declared set.
	ActionUserOwned Action = "user-owned"
)

// Page is one planned change.
type Page struct {
	// Name is the wiki page name (the source file's base name without
	// the extension; "Home" for Home.md).
	Name string `json:"name"`
	// Source is the repository-relative source file.
	Source string `json:"source"`
	Action Action `json:"action"`
	// Bytes is the size of the content that would be published.
	Bytes int `json:"bytes"`
}

// Result is the outcome of a sync.
type Result struct {
	Repository string   `json:"repository"`
	WikiURL    string   `json:"wiki_url"`
	DryRun     bool     `json:"dry_run"`
	Applied    bool     `json:"applied"`
	Pages      []Page   `json:"pages"`
	Created    int      `json:"created"`
	Updated    int      `json:"updated"`
	Unchanged  int      `json:"unchanged"`
	UserOwned  int      `json:"user_owned"`
	Commit     string   `json:"commit,omitempty"`
	PushedRef  string   `json:"pushed_ref,omitempty"`
	Notes      []string `json:"notes,omitempty"`
}

// Changed reports whether a plan contains anything to do.
func (r *Result) Changed() bool { return r.Created+r.Updated > 0 }

// Options control a sync.
type Options struct {
	// SourceDir is the local directory holding the declared pages. It
	// must be inside the repository being governed.
	SourceDir string
	// Owner and Repo identify the target repository (owner/name).
	Owner string
	Repo  string
	// RepoRoot is the repository root the source lives in. The source
	// directory must be inside it, so a stray --source-dir cannot
	// publish arbitrary files.
	RepoRoot string
	// Apply performs the push. The zero value is a dry run.
	Apply bool
	// Token authenticates the push. Empty means git's own credential
	// resolution (typically SSH), which works for a maintainer with
	// write access and keeps the tool credential-free for read-only
	// planning.
	Token string
	// BaseURL is the GitHub instance that hosts the wiki. The default is
	// https://github.com; override it for GitHub Enterprise Server.
	BaseURL string
	// CommitMessage overrides the generated commit subject.
	CommitMessage string
}

// WikiURL is the documented wiki git endpoint for a repository.
func WikiURL(owner, repo string) string {
	return WikiURLFor(DefaultBaseURL, owner, repo)
}

// WikiURLFor derives the wiki git endpoint for a GitHub instance.
func WikiURLFor(baseURL, owner, repo string) string {
	base := strings.TrimSuffix(strings.TrimSpace(baseURL), "/")
	if base == "" {
		base = DefaultBaseURL
	}
	return fmt.Sprintf("%s/%s/%s.wiki.git", base, owner, repo)
}

// SourcePage is one declared page read from the source directory.
type SourcePage struct {
	Name   string
	Source string
	Data   []byte
}

// LoadPages reads the declared page set from dir. The result is sorted
// by page name so plans and commits are deterministic. Rejected inputs:
// a missing directory, a subdirectory, a non-Markdown file, a source
// path that escapes dir, and a duplicate page name.
func LoadPages(dir string) ([]SourcePage, error) {
	if dir == "" {
		return nil, fmt.Errorf("wiki: no source directory given")
	}
	info, err := os.Stat(dir)
	if err != nil {
		return nil, fmt.Errorf("wiki: reading wiki source %s: %w", dir, err)
	}
	if !info.IsDir() {
		return nil, fmt.Errorf("wiki: wiki source %s is not a directory", dir)
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil, fmt.Errorf("wiki: reading wiki source %s: %w", dir, err)
	}
	byName := map[string]SourcePage{}
	var names []string
	for _, e := range entries {
		if e.IsDir() {
			return nil, fmt.Errorf("wiki: wiki source %s contains the subdirectory %q; wiki pages are flat files", dir, e.Name())
		}
		if !strings.HasSuffix(e.Name(), PageExtension) {
			return nil, fmt.Errorf("wiki: wiki source %s contains %q; only %s files are published to the wiki", dir, e.Name(), PageExtension)
		}
		name := strings.TrimSuffix(e.Name(), PageExtension)
		if name == "" || strings.ContainsAny(name, "/\\") || name != strings.TrimSpace(name) {
			return nil, fmt.Errorf("wiki: %q is not a usable wiki page name", e.Name())
		}
		data, err := os.ReadFile(filepath.Join(dir, e.Name()))
		if err != nil {
			return nil, fmt.Errorf("wiki: reading %s: %w", e.Name(), err)
		}
		if bytes.IndexByte(data, 0) >= 0 {
			return nil, fmt.Errorf("wiki: %s is binary; wiki pages are Markdown text", e.Name())
		}
		if _, dup := byName[name]; dup {
			return nil, fmt.Errorf("wiki: two source files declare the page %q", name)
		}
		byName[name] = SourcePage{Name: name, Source: e.Name(), Data: data}
		names = append(names, name)
	}
	sort.Strings(names)
	out := make([]SourcePage, 0, len(names))
	for _, n := range names {
		out = append(out, byName[n])
	}
	return out, nil
}

// ReadHosted reads the pages the wiki currently serves from a local
// clone.
func ReadHosted(clone string) (map[string][]byte, error) {
	entries, err := os.ReadDir(clone)
	if err != nil {
		return nil, fmt.Errorf("wiki: reading the wiki clone: %w", err)
	}
	hosted := map[string][]byte{}
	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		if !strings.HasSuffix(e.Name(), PageExtension) {
			continue
		}
		if e.Name() == "_Sidebar.md" || e.Name() == "_Footer.md" {
			// GitHub's own chrome: never rewritten by RepoForge.
			continue
		}
		data, err := os.ReadFile(filepath.Join(clone, e.Name()))
		if err != nil {
			return nil, fmt.Errorf("wiki: reading hosted page %s: %w", e.Name(), err)
		}
		hosted[strings.TrimSuffix(e.Name(), PageExtension)] = data
	}
	return hosted, nil
}

// Plan computes the changes needed to converge the wiki to the declared
// page set. It performs no writes and needs no credentials.
func Plan(pages []SourcePage, hosted map[string][]byte) *Result {
	res := &Result{}
	for _, p := range pages {
		current, ok := hosted[p.Name]
		switch {
		case !ok:
			res.Created++
			res.Pages = append(res.Pages, Page{Name: p.Name, Source: p.Source, Action: ActionCreate, Bytes: len(p.Data)})
		case !bytes.Equal(current, p.Data):
			res.Updated++
			res.Pages = append(res.Pages, Page{Name: p.Name, Source: p.Source, Action: ActionUpdate, Bytes: len(p.Data)})
		default:
			res.Unchanged++
			res.Pages = append(res.Pages, Page{Name: p.Name, Source: p.Source, Action: ActionUnchanged, Bytes: len(p.Data)})
		}
	}
	declared := map[string]bool{}
	for _, p := range pages {
		declared[p.Name] = true
	}
	var extra []string
	for name := range hosted {
		if !declared[name] {
			extra = append(extra, name)
		}
	}
	sort.Strings(extra)
	for _, name := range extra {
		res.UserOwned++
		res.Pages = append(res.Pages, Page{Name: name, Action: ActionUserOwned, Bytes: len(hosted[name])})
	}
	return res
}

// Sync converges a repository wiki to the declared page set.
//
// The flow is: validate the target, load the declared pages, clone the
// wiki, plan, and — only when Apply is set — stage the pages, commit,
// and push without force.
func Sync(ctx context.Context, opts Options) (*Result, error) {
	if opts.Owner == "" || opts.Repo == "" {
		return nil, fmt.Errorf("wiki: target repository must be owner/name")
	}
	sourceDir, err := resolveSource(opts.SourceDir, opts.RepoRoot)
	if err != nil {
		return nil, err
	}
	pages, err := LoadPages(sourceDir)
	if err != nil {
		return nil, err
	}
	if len(pages) == 0 {
		return nil, fmt.Errorf("wiki: %s declares no pages; nothing to publish", sourceDir)
	}
	base := opts.BaseURL
	if base == "" {
		base = DefaultBaseURL
	}
	url := WikiURLFor(base, opts.Owner, opts.Repo)

	work, err := os.MkdirTemp("", "repoforge-wiki-*")
	if err != nil {
		return nil, fmt.Errorf("wiki: creating a workspace: %w", err)
	}
	defer os.RemoveAll(work)

	clone, err := Clone(ctx, url, work, opts.Token)
	if err != nil {
		return nil, err
	}
	if err := verifyClone(clone, url); err != nil {
		return nil, err
	}
	hosted, err := ReadHosted(clone)
	if err != nil {
		return nil, err
	}
	res := Plan(pages, hosted)
	res.Repository = opts.Owner + "/" + opts.Repo
	res.WikiURL = url
	res.DryRun = !opts.Apply

	if !res.Changed() {
		res.Notes = append(res.Notes, "the wiki already matches the declared page set")
		if res.UserOwned > 0 {
			res.Notes = append(res.Notes, fmt.Sprintf("%d user-owned page(s) are left untouched", res.UserOwned))
		}
		return res, nil
	}
	if !opts.Apply {
		res.Notes = append(res.Notes, "dry run: nothing was written or pushed")
		return res, nil
	}

	subject := opts.CommitMessage
	if subject == "" {
		subject = "docs(wiki): sync RepoForge wiki pages"
	}
	for _, p := range pages {
		if err := os.WriteFile(filepath.Join(clone, p.Source), p.Data, 0o644); err != nil {
			return nil, fmt.Errorf("wiki: writing %s: %w", p.Source, err)
		}
	}
	commit, ref, err := CommitAndPush(ctx, clone, subject, opts.Token)
	if err != nil {
		return nil, err
	}
	res.Applied = true
	res.Commit = commit
	res.PushedRef = ref
	return res, nil
}

// resolveSource validates the source directory: it must exist and must
// live inside the repository being governed, so an operator cannot
// point the publisher at an unrelated tree.
func resolveSource(sourceDir, repoRoot string) (string, error) {
	if repoRoot == "" {
		repoRoot = "."
	}
	rootAbs, err := filepath.Abs(repoRoot)
	if err != nil {
		return "", fmt.Errorf("wiki: resolving %s: %w", repoRoot, err)
	}
	rootReal, err := filepath.EvalSymlinks(rootAbs)
	if err != nil {
		return "", fmt.Errorf("wiki: resolving %s: %w", repoRoot, err)
	}
	srcAbs, err := filepath.Abs(sourceDir)
	if err != nil {
		return "", fmt.Errorf("wiki: resolving %s: %w", sourceDir, err)
	}
	srcReal, err := filepath.EvalSymlinks(srcAbs)
	if err != nil {
		return "", fmt.Errorf("wiki: resolving %s: %w", sourceDir, err)
	}
	if srcReal != rootReal && !strings.HasPrefix(srcReal, rootReal+string(filepath.Separator)) {
		return "", fmt.Errorf("wiki: wiki source %s is outside the repository %s; refusing to publish files from outside it", srcReal, rootReal)
	}
	info, err := os.Stat(srcReal)
	if err != nil {
		return "", fmt.Errorf("wiki: reading wiki source %s: %w", srcReal, err)
	}
	if !info.IsDir() {
		return "", fmt.Errorf("wiki: wiki source %s is not a directory", srcReal)
	}
	return srcReal, nil
}

// verifyClone refuses a clone that is not the URL the caller asked for.
// This is the dangerous-destination check: it runs before any write,
// so a misconfigured, redirected, or hijacked remote cannot receive the
// pages.
func verifyClone(clone, wantURL string) error {
	out, err := gitOutput(clone, "remote", "get-url", "origin")
	if err != nil {
		return fmt.Errorf("wiki: reading the wiki remote URL: %w", err)
	}
	got := normalizeWikiURL(out)
	want := normalizeWikiURL(wantURL)
	if got != want {
		return fmt.Errorf("wiki: the clone's origin is %q, expected %q; refusing to publish to a different repository", out, want)
	}
	return nil
}

// normalizeWikiURL strips transport differences so the comparison is
// about repository identity, not about how git reached it.
func normalizeWikiURL(u string) string {
	u = strings.TrimSpace(u)
	u = strings.TrimSuffix(u, ".git")
	u = strings.TrimSuffix(u, "/")
	if i := strings.Index(u, "://"); i >= 0 {
		rest := u[i+3:]
		if at := strings.Index(rest, "@"); at >= 0 {
			rest = rest[at+1:]
		}
		u = rest
	} else if at := strings.Index(u, "@"); at >= 0 && strings.Contains(u, ":") {
		// scp-like SSH syntax: git@github.com:owner/repo.wiki
		u = u[at+1:]
		u = strings.Replace(u, ":", "/", 1)
	}
	return strings.ToLower(u)
}

func gitOutput(dir string, args ...string) (string, error) {
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	out, err := cmd.Output()
	if err != nil {
		return "", fmt.Errorf("git %s: %v: %s", strings.Join(args, " "), err, strings.TrimSpace(stderr.String()))
	}
	return strings.TrimSpace(string(out)), nil
}
