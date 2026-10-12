// Copyright the repoforge contributors.
//
// SPDX-License-Identifier: Apache-2.0

package wiki

import (
	"bytes"
	"context"
	"encoding/base64"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"strings"
)

// Sentinel errors so callers (and tests) can tell a configuration
// problem from a credential problem without parsing git's prose.
var (
	errWikiNotFound = errors.New("wiki not found or disabled")
	errWikiAuth     = errors.New("wiki authentication failed")
)

// NotFound reports whether err means the wiki does not exist or is
// disabled, rather than being unreachable or unauthorized.
func NotFound(err error) bool { return errors.Is(err, errWikiNotFound) }

// Unauthorized reports whether err means the credential could not write
// the wiki.
func Unauthorized(err error) bool { return errors.Is(err, errWikiAuth) }

// gitEnv returns the environment for a git invocation.
//
// A token is injected as an http.extraHeader entry resolved from the
// environment rather than the URL or the argument list, so it never
// appears in a process listing, a reflog, or the remote URL stored in the
// clone's configuration.
//
// The scheme is HTTP Basic with the documented x-access-token username,
// not a bearer token: git-over-HTTPS rejects "Authorization: bearer"
// with "invalid credentials", and reporting that as an authentication
// failure against a perfectly good token is the failure mode this
// avoids. base64 is transport encoding, not encryption — the secret stays
// in the environment, which is the boundary that matters here.
func gitEnv(token string) []string {
	env := append(os.Environ(), "GIT_TERMINAL_PROMPT=0", "GIT_ASKPASS=true", "GIT_CONFIG_NOSYSTEM=1")
	if token == "" {
		return env
	}
	return append(env,
		"GIT_CONFIG_COUNT=1",
		"GIT_CONFIG_KEY_0=http.extraHeader",
		"GIT_CONFIG_VALUE_0="+authHeader(token),
	)
}

// authHeader builds the Basic credential for the documented git
// username.
func authHeader(token string) string {
	raw := "x-access-token:" + token
	return "Authorization: Basic " + base64.StdEncoding.EncodeToString([]byte(raw))
}

// runGit runs git in dir and returns trimmed stdout.
func runGit(ctx context.Context, dir, token string, args ...string) (string, error) {
	cmd := exec.CommandContext(ctx, "git", args...)
	cmd.Dir = dir
	cmd.Env = gitEnv(token)
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		return "", fmt.Errorf("git %s: %v: %s", strings.Join(args, " "), err, strings.TrimSpace(stderr.String()))
	}
	return strings.TrimSpace(stdout.String()), nil
}

// Clone fetches the wiki repository into a new subdirectory of work and
// returns the clone path.
//
// The two failure modes that matter are separated explicitly, because
// they have different remedies: a wiki that does not exist (disabled,
// or the plan does not include wikis) is a configuration problem, while
// an authentication failure is a credential problem. Reporting the first
// as the second sends an operator hunting for a token that is fine.
func Clone(ctx context.Context, url, work, token string) (string, error) {
	if _, err := exec.LookPath("git"); err != nil {
		return "", fmt.Errorf("wiki: git is required to reach the wiki: %w", err)
	}
	clone := work + "/wiki"
	_, err := runGit(ctx, work, token, "clone", "--quiet", url, clone)
	if err == nil {
		return clone, nil
	}
	return "", classifyCloneFailure(url, err)
}

// classifyCloneFailure separates the three failure classes an operator
// needs to tell apart, because they have different remedies:
//
//   - missing: the wiki is disabled, or enabled but never initialised.
//     GitHub only creates <owner>/<repo>.wiki.git when the first page is
//     created through the web UI, so an enabled-but-untouched wiki
//     reports exactly the same "not found" as a disabled one. The message
//     names both and gives the step for each.
//   - unauthorized: a credential problem, not a configuration one.
//   - transport: anything else, reported verbatim.
//
// It is a pure function of the git output so the three branches can be
// tested without a network.
func classifyCloneFailure(url string, err error) error {
	detail := err.Error()
	switch {
	case strings.Contains(detail, "Repository not found") || strings.Contains(detail, "not found"):
		return fmt.Errorf("wiki: %s does not exist — the wiki is disabled, or it is enabled but has never been initialised. Enable it (has_wiki: true, which the RepoForge governance standard requires) and create one page in the web UI so the wiki git repository comes into existence, then re-run: %w", url, errWikiNotFound)
	case strings.Contains(detail, "Authentication failed") || strings.Contains(detail, "Permission denied") ||
		strings.Contains(detail, "could not read Username") || strings.Contains(detail, "403") ||
		strings.Contains(detail, "401") || strings.Contains(detail, "invalid credentials"):
		return fmt.Errorf("wiki: authentication to %s failed — the credential cannot write this wiki (check the token and that the wiki is enabled): %w", url, errWikiAuth)
	default:
		return fmt.Errorf("wiki: cloning %s failed: %w", url, err)
	}
}

// CommitAndPush writes the staged pages as one commit and pushes it to
// the wiki's default branch. The push is never forced: if the remote
// advanced since the clone, git rejects it and the operator re-plans.
func CommitAndPush(ctx context.Context, clone, subject, token string) (commit, ref string, err error) {
	if err := stageAndCommit(ctx, clone, subject, token); err != nil {
		return "", "", err
	}
	commit, err = runGit(ctx, clone, token, "rev-parse", "HEAD")
	if err != nil {
		return "", "", fmt.Errorf("wiki: resolving the new commit: %w", err)
	}
	ref, err = runGit(ctx, clone, token, "rev-parse", "--abbrev-ref", "HEAD")
	if err != nil {
		return "", "", fmt.Errorf("wiki: resolving the wiki branch: %w", err)
	}
	if _, err := runGit(ctx, clone, token, "push", "--quiet", "origin", "HEAD:"+ref); err != nil {
		return "", "", fmt.Errorf("wiki: pushing to the wiki was rejected (the remote moved since the clone); re-run to re-plan — RepoForge never force-pushes a wiki: %w", err)
	}
	return commit, ref, nil
}

// stageAndCommit commits exactly the pages RepoForge wrote. Nothing is
// deleted, so user-owned pages survive the commit untouched.
func stageAndCommit(ctx context.Context, clone, subject, token string) error {
	if _, err := runGit(ctx, clone, token, "add", "--", "."); err != nil {
		return fmt.Errorf("wiki: staging the pages: %w", err)
	}
	if _, err := runGit(ctx, clone, token, "diff", "--cached", "--quiet"); err == nil {
		// The plan said there was work but git agrees there is none.
		// Report that instead of inventing an empty commit.
		return fmt.Errorf("wiki: no staged changes after writing the pages; re-run to re-plan")
	}
	author := []string{"-c", "user.name=repoforge", "-c", "user.email=repoforge@users.noreply.github.com"}
	args := append(author, "commit", "--quiet", "-m", subject)
	if _, err := runGit(ctx, clone, token, args...); err != nil {
		return fmt.Errorf("wiki: committing the pages: %w", err)
	}
	return nil
}
