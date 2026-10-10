# First day

From template to first pull request, end to end.

## 1. Understand what you start with

A repository generated from a GitHub template begins with **the
template's file contents and a new, unrelated Git history**: one
initial commit, no shared ancestry with the template. Do not expect
the template's commits, branches, tags, or blame to appear in your
project — they never arrive.

## 2. Create the repository

```sh
gh repo create {{ .Owner }}/{{ .ProjectName }} \
  --template {{ .Owner }}/repoforge-template \
  --public \
  --clone
cd {{ .ProjectName }}
```

## 3. Customize

Adopt the repository: resolve every double-brace placeholder with
real values, explicitly and reviewably (preview with `--dry-run`
first; re-runs are no-ops):

```sh
repoforge template adopt \
  --var ProjectName=<name> \
  --var Owner=<owner> \
  --var RepoURL=<url> \
  --var Description=<description> \
  --var StandardVersion=v1
```

Then review `.repoforge/config.yml`.
`repoforge validate` fails until this is done — deliberately.

## 4. Bootstrap

```sh
repoforge bootstrap
```

Read the plan. It lists every intended change: repository settings,
the `repoforge:main` and `repoforge:release-tags` rulesets,
Dependabot, secret scanning.
Type `yes` to apply. Bootstrap then re-reads the remote state and
verifies convergence. Re-running it later is safe: it is idempotent
and reports zero changes when nothing drifted.

Use `repoforge bootstrap --dry-run` any time to preview without
touching anything.

## 5. Seed the wiki

The wiki is generated from `docs/wiki/` in this repository. Preview,
then publish:

```sh
repoforge wiki sync --repo {{ .Owner }}/{{ .ProjectName }}
repoforge wiki sync --repo {{ .Owner }}/{{ .ProjectName }} --apply
```

Preview first, always. Sync never deletes a page it does not declare,
and never force-pushes. See
[wiki-maintenance.md](../operations/wiki-maintenance.md).

## 6. Verify

```sh
repoforge doctor
```

Expect all `PASS`. `UNSUPPORTED` lines are capability notes (e.g. a
plan-limited feature on a private repository), not failures. `FAIL`
lines are drift: re-run `repoforge bootstrap` or fix by hand.

## 7. First issue

```sh
gh issue create --title "Set up <thing>" --body "First task."
```

## 8. First branch

```sh
git switch -c feat/1-setup-thing
```

Naming is enforced: `<type>/<issue>-<description>`. See
[branch-strategy.md](branch-strategy.md).

## 9. First commit

```sh
git add -A
git commit -m "feat: set up thing"
```

Conventional Commits are enforced by validation. See
[commit-strategy.md](commit-strategy.md).

## 10. First PR

```sh
gh pr create --fill
```

Fill the template sections (validation evidence is required), wait for
green CI and review, then squash merge. See
[pull-request-strategy.md](pull-request-strategy.md).
