# Developer Git hooks

RepoForge installs a small set of local Git hooks. They exist to give
fast feedback **before** a commit leaves the machine, not to replace CI.

> **CI and repository protection rules are the authoritative merge gates**
> (spec.md P9). Hooks can always be bypassed with `--no-verify`; a bypass is
> visible in the commit record and does not weaken any required check.
> Nothing here changes repository settings, merges, pushes, or publishes.

## What runs where

| Hook | Stage | Blocking? | What it does |
| --- | --- | --- | --- |
| `pre-commit` | commit | **blocks** | gofmt on staged Go, staged-file hygiene, commit-scope checks, policy-schema consistency, `repoforge validate`, template-source validation, gitleaks on staged content, workflow and shell lint when those files change |
| `commit-msg` | commit | **blocks** | Conventional Commits structure, branch and commit policy |
| `pre-push` | push | **blocks** | the full local verification suite (`scripts/verify full`) |
| `post-commit` | after commit | informational only | shows the next verification command |
| `post-push` | after push | informational only | shows open PR checks via `gh` |

### Blocking vs informational

A hook that runs *after* an event cannot undo that event. `post-commit`
and `post-push` are therefore feedback only: they never rewrite
commits, stage anything, create commits, force-push, merge a pull
request, create a release, or change repository settings.

`post-push` makes a network call only when `gh` is installed and
authenticated. When it is not, the hook says so and exits 0 — a failure
to reach GitHub is never allowed to imply that local code verification
passed.

## Install

One command, idempotent:

```sh
scripts/hooks-install
```

This runs `pre-commit install` for the three blocking hooks and writes
the two informational hooks into `.git/hooks/`. It never replaces a hook
it did not install: if `post-commit` or `post-push` already exists and
carries no RepoForge marker, the script says so and leaves it alone.

Prerequisites:

- [Go](https://go.dev/dl/) (the version floor is in `go.mod`)
- [pre-commit](https://pre-commit.com/#install) — the maintained,
  pinned framework. Required for the blocking hooks.
- Optional: `actionlint` and `shellcheck` (skipped loudly when absent).

Install them directly instead if you prefer:

```sh
pre-commit install --hook-type pre-commit
pre-commit install --hook-type commit-msg
pre-commit install --hook-type pre-push
scripts/hooks-install --verify
```

## Verify

```sh
scripts/hooks-install --verify
```

Reports every blocking hook and every informational hook as installed,
not-installed, or present but not ours, and exits non-zero if anything
is missing.

## Uninstall

```sh
scripts/hooks-install --uninstall
```

Removes only what this tool installed. Any hook that is not ours —
including one you wrote yourself — is left in place.

## Pinning

Every remote hook in `.pre-commit-config.yaml` is pinned to an immutable
commit SHA, recorded in the `rev:` field with a trailing version
comment. No hook downloads and executes an unpinned remote script.

To update a pinned revision:

```sh
pre-commit autoupdate --repo https://github.com/<org>/<repo>
```

Review the resulting change, and re-run `pre-commit validate` and
`scripts/verify fast` before committing.

## Disabling

Per commit:

```sh
git commit --no-verify
```

Temporarily, without uninstalling:

```sh
SKIP=repoforge-validate,repoforge-template-validate git commit -m "…"
```

## Troubleshooting

`pre-commit: command not found` — install pre-commit (above). The
blocking hooks are not installed without it; `scripts/hooks-install`
says so instead of silently doing nothing.

A hook fails on a file you did not touch — the hook runs against the
staged content, not the working tree. Stage what you want checked:

```sh
git add -p
```

You want the full local suite without committing — run it directly:

```sh
scripts/verify full
```

## Related

- `docs/development/git-workflow.md` — the branch, commit, and PR rules
  the hooks enforce.
- `docs/development/commit-conventions.md` — the Conventional Commits
  subset, including which violations are hard errors and which are
  style warnings.
- `scripts/verify` — the authoritative local verification entry point.
