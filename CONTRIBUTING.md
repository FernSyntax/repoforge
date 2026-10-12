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

## Coding standards

The guides for the languages this repository uses, and how each is
enforced:

- **Go** follows [Effective Go](https://go.dev/doc/effective_go) and
  the formatting produced by `gofmt`; code is checked with `go vet`.
  Both run in `scripts/verify` and as required CI checks, so a
  deviation fails the gate rather than the review.
- **Shell** follows the [Google Shell Style
  Guide](https://google.github.io/styleguide/shellguide.html), checked
  with `shellcheck`.
- **GitHub Actions workflows** follow the `actionlint` rule set; every
  third-party action is pinned to a full commit SHA with a version
  comment.
- **Documentation and specs** follow RFC 2119 for normative language
  and the repository's own link and identity rules (spec §8.4):
  no hardcoded users, orgs, or repository names in shared documents.

Where a guide is silent, the surrounding file decides; consistency
within a file is itself the standard.

## Test policy

A major change must add or update automated tests in the same change.
A change is major when it alters behavior, changes policy or schema,
touches a security boundary, or fixes a regression. Every regression
fix in this repository carries a test that fails without the fix —
that is the house style, not an aspiration.

A change with no new or updated tests must say so in the pull request,
with the reason (for example: documentation-only, or the behavior is
already covered by an existing test the change references). "Tests
come later" is not an accepted reason; the pull request gates require
the statement, so an empty answer fails the gate.

## Dependency and scan policy

Static and compositional analysis runs on every pull request as
required status checks: CodeQL default setup (`Analyze (go)` and
`Analyze (actions)`), dependency review, `govulncheck`, and secret
detection. The remediation thresholds:

- **Error-severity SAST findings block merge.** Warnings are fixed or
  justified in the pull request within one release. A finding that is a
  false positive is dismissed with a recorded justification in the
  code-scanning dashboard — never silenced globally, and never with
  `--no-verify`.
- **High- or critical-severity dependency vulnerabilities block merge
  and release.** Medium findings get a documented exception with an
  expiry date; low findings are triaged on a monthly cadence.
- **Release gating.** `scripts/release-check.sh` runs on every pushed
  tag and refuses to proceed on a failing validation run, so a release
  cannot ship over a red check.

Rotating an action pin or upgrading a dependency is a security-relevant
change: review it as one, in its own pull request (see
[docs/architecture/security.md](docs/architecture/security.md) →
"Dependency pinning guidance").

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
