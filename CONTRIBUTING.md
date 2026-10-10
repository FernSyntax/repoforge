# Contributing

RepoForge is an opinionated standard: contributions are welcome, and
the specification ([spec.md](spec.md)) is the arbiter of what is
correct. When in doubt, open an issue before a pull request.

## Ground rules

- Major design decisions require specification text in spec.md or an
  ADR in `docs/decisions/` **before** implementation.
- Every automated behavior ships with tests.
- Every external side effect ships with a dry-run or read-only path.
- CI is authoritative. Local checks are developer feedback.
- Never commit credentials, tokens, or hardcoded identities (users,
  orgs, repository names, emails). Template placeholders such as
  `{{ .Owner }}` are resolved by tooling, not by editing.
- By contributing, you license your work under Apache-2.0 and agree to
  the [Code of Conduct](CODE_OF_CONDUCT.md).

## Setup

Requires a current Go toolchain (the two most recent minor releases are
supported; `go.mod` declares the minimum).

```sh
go build ./...
```

## Workflow

The Git workflow is specified in `docs/development/` and enforced by CI
(see the required checks below):

- [Branching](docs/development/branching.md) — `main` only, topic
  branches named `<type>/<issue>-<description>`.
- [Commit conventions](docs/development/commit-conventions.md) —
  restricted Conventional Commits; the PR title becomes the squash
  commit subject.
- [Pull requests](docs/development/pull-requests.md) — content contract,
  merge policy (squash only).
- [Release process](docs/development/release-process.md) — SemVer,
  annotated tags, changelog, GitHub releases.

## Issues and pull requests

- Use the issue forms (bug / feature / task). They exist to keep
  reports high-signal.
- Every PR uses the pull request template and a CI check enforces its
  content contract: what changed, why (with the linked issue),
  specification requirement IDs, tests, the exact validation commands
  run, risk, documentation and ADR changes, operational impact,
  breaking/migration impact, and known limitations. Fill it in —
  "none" with a reason beats an empty section.
- Two exceptions are recognised and both must be declared in the PR
  body so they are visible in the record: `dependency-pull-request`
  (GitHub-generated dependency update) and `urgent-security-fix`.
  There is no self-applied bypass label.
- Keep PRs scoped to one concern.
- Update CHANGELOG.md for user-visible changes.
- Address review feedback with new commits; do not force-push a branch
  under review. Squash merge keeps history clean.
- Paths in CODEOWNERS require owner review.

## Verification

Run all of these before opening a pull request:

```sh
scripts/verify fast     # formatting, vet, build, tests, validation, schemas, forms
scripts/verify full     # adds race detection, template determinism, and lint
```

`scripts/verify` resolves the repository root, prints each command, and
propagates exit codes. A missing tool is reported with installation
instructions and the run fails — it is never skipped silently.

The same checks run in CI as required status checks. CI is authoritative:
local checks are developer feedback and can be bypassed.

## Developer hooks

```sh
scripts/hooks-install     # idempotent
scripts/hooks-install --uninstall   # removes only the hooks this project installed
scripts/hooks-install --verify      # report what is installed
```

See [docs/development/git-hooks.md](docs/development/git-hooks.md).

## Security

Report vulnerabilities privately per [SECURITY.md](SECURITY.md). Never
file security issues publicly.
