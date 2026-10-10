# Getting Started

Adopt RepoForge for a new or existing repository, end to end.
Assumes `gh` (authenticated) and `git`; the CLI builds with any
recent Go toolchain (`go` directive in `go.mod` is the floor).

## 1. Create from the template

```sh
gh repo create <owner>/<name> --template FernSyntax/repoforge-template --public --clone
cd <name>
```

You get the baseline plus a fresh single-commit history. Nothing
of the engine's history, code, or identity comes with it.

## 2. Make it yours (required)

Adopt the repository — resolve every double-brace placeholder with
real values, explicitly and reviewably (preview with `--dry-run`
first):

```sh
repoforge template adopt \
  --var ProjectName=<name> \
  --var Owner=<owner> \
  --var RepoURL=<url> \
  --var Description=<description> \
  --var StandardVersion=v1
```

Then review `.repoforge/config.yml` (`visibility`,
`governance_profile`, `project.kind`) and run:

```sh
repoforge validate   # must be clean before continuing
```

## 3. Converge it

```sh
repoforge bootstrap --dry-run   # review every intended change
repoforge bootstrap             # confirm explicitly, then verify
repoforge doctor                # expect all PASS
```

Bootstrap plans repository settings, owned rulesets and labels, and
security posture; it reports anything it does not own and leaves it
alone. Re-running it is safe and reports zero changes.

## 4. Work the standard way

```sh
gh issue create --title "Add search" --body "What/why."
git checkout -b feat/1-add-search
# ... work ...
git add . && git commit -m "feat(search): add query parser"
git push -u origin feat/1-add-search
gh pr create --title "feat(search): add query parser" --body "Closes #1."
gh pr checks --watch   # all green, then squash-merge
```

Keep PR titles well under 72 characters — GitHub appends ` (#N)` on
squash, and the length gate counts it.

## 5. Seed the wiki

The wiki is generated from `docs/wiki/`. Preview first, then publish:

```sh
repoforge wiki sync --repo <owner>/<name>
repoforge wiki sync --repo <owner>/<name> --apply
```

See [operations/wiki-maintenance.md](../operations/wiki-maintenance.md).

## 6. Adopt an existing repository

Same flow, starting from your checkout: add a
`repoforge.yaml` (see `docs/reference/configuration.md`),
run `validate` until clean, then `bootstrap --dry-run` and review
carefully — the first plan against a grown repository is large, and
large plans deserve slow reading. Bootstrap never touches
user-owned paths, unmanaged rulesets, or visibility; everything
else it lists before changing.

## Troubleshooting

- `validate` fails on placeholders → finish step 2 first.
- `bootstrap` reports `conflict` → an unmanaged ruleset or setting
  needs a human decision; nothing was changed.
- `bootstrap` reports `unsupported` → a plan-limited feature on a
  plan without it; lower the setting or upgrade, explicitly.
- `doctor` FAIL after green → drift; re-run `bootstrap`.
