# RepoForge

Opinionated repository engineering for GitHub: a declarative standard plus a CLI that enforces it.

RepoForge is an open-source standard for how GitHub repositories should be structured, governed, validated, and released — and a Go CLI (`repoforge`) that bootstraps repositories to the standard, audits drift read-only, and validates checkouts deterministically. It is for solo developers and small teams who want every repository to start governed and stay governed, without babysitting settings pages.

## Quick start

```sh
# 1. Create a repository from the template.
gh repo create <owner>/<name> --template FernSyntax/repoforge-template --public --clone
cd <name>

# 2. Preview convergence (changes nothing).
repoforge bootstrap --dry-run

# 3. Apply (plan → confirm → apply → verify).
repoforge bootstrap

# 4. Audit any time (read-only, never mutates).
repoforge doctor
```

New to the project? Start with [docs/development/getting-started.md](docs/development/getting-started.md).
Maintaining the standard itself? See [docs/development/maintainer-workflow.md](docs/development/maintainer-workflow.md).
The normative specification is [spec.md](spec.md).

## Architecture

Two repositories, strictly separated ([why](docs/architecture/why-two-repositories.md)):

- **repoforge** (this repository): CLI, policy specification,
  schemas, tests, documentation, release tooling, template builder.
- **repoforge-template**: the generated baseline developers
  consume. It contains no implementation source — verified by tests,
  not by promise.

```
cmd/repoforge        CLI entry point
internal/bootstrap      convergence to policy (idempotent)
internal/config         declarative policy and governance loading
internal/doctor         read-only drift audit
internal/github         GitHub API client boundary
internal/gitrules       commit/branch/PR conventions (single rule source)
internal/template       template generation, manifest, verification
internal/validate       deterministic local validation
internal/wiki           GitHub wiki synchronisation
internal/brand          product-identity leak gate (shared)
internal/version        build version stamping
tests/e2e               opt-in end-to-end lifecycle (never in normal CI)
schemas/                policy and governance schemas (canonical + derived)
configs/github/         governance baseline and profiles
docs/                   architecture, decisions, development, operations, reference
template/               canonical source of the generated template
```

## What you get

- A repository created governed: merge policy, rulesets, labels,
  Dependabot, secret scanning, and code scanning configured from a
  policy file instead of clicked together.
- A control loop that keeps it governed: `bootstrap` converges drift,
  `doctor` reports it read-only, `validate` checks the checkout
  deterministically — in CI, which is authoritative.
- No silent mutation: every external change is planned, confirmed,
  and re-verified; dry-run paths exist for destructive operations.

## Repository standard

Every managed repository declares itself in `repoforge.yaml`
(contract schema v1): standard version, repository identity,
merge settings, git conventions, GitHub features, security posture,
release strategy, and path ownership
(`managed` / `scaffolded` / `user-owned`). Unknown fields are
rejected. See [docs/reference/configuration.md](docs/reference/configuration.md)
and the field reference
[docs/architecture/configuration-model.md](docs/architecture/configuration-model.md).

## Git standard

Trunk-based development: `main` is the only permanent branch; topic
branches are `<type>/<issue>-<description>`; squash merge only; no
force pushes. Commits follow a restricted Conventional Commits
subset, checked by `repoforge git commit-check` and
`branch-check`. Details: [docs/development/](docs/development/)
git workflow guides.

## GitHub governance

Repository settings are declarative data (`configs/github/`:
baseline plus solo/team/strict profiles), applied by bootstrap and
audited by doctor. Ownership is explicit: two deterministically
named rulesets (`repoforge:main`, `repoforge:release-tags`),
19 canonical labels, and a never-touch-anything-else rule for
unmanaged state. Plan-dependent features degrade to explicit
diagnostics, never silent skips. Compatibility:
[docs/reference/github-support-matrix.md](docs/reference/github-support-matrix.md).

## Security

- Least-privilege automation: empty top-level workflow permissions,
  explicit per-job grants, credential-less checkouts, no
  `pull_request_target`.
- Everything third-party pinned to immutable SHAs (actions) or
  SHA-256 (binaries, modules); Dependabot proposes updates, humans
  merge them.
- Secret detection on push and PR; `validate` rejects
  credential-shaped material; tokens are never stored and are
  redacted from errors.
- Releases carry SHA-256 checksums, SPDX SBOMs, and Sigstore
  build-provenance attestations (`gh attestation verify`).
- Report vulnerabilities privately per [SECURITY.md](SECURITY.md).
  Full model: [docs/architecture/security.md](docs/architecture/security.md).

## Verification

```sh
scripts/verify fast     # formatting, vet, build, tests, validation, schemas
scripts/verify full     # adds race detection and the template determinism proof
```

Every failing check exits non-zero. A missing tool is reported with
installation instructions rather than skipped. CI is the authoritative
merge gate; local checks are developer feedback.

## CLI

```sh
repoforge validate [--json]          # deterministic local audit (exit 0/1/2)
repoforge doctor [--repo o/r]        # read-only local + remote audit
repoforge bootstrap [--dry-run] [--yes] [--repo o/r]   # converge to policy
repoforge template validate [--path template/]
repoforge template build --out DIR [--force] [--source-ref vX.Y.Z --source-commit <sha>]
repoforge template verify [--require-source]   # manifest + rebuild from the recorded commit
repoforge template provenance                   # flags a reproducible rebuild needs
repoforge template adopted                       # is this an adopted generated repository?
repoforge template adopt --var key=value ...   # in a generated repository
repoforge wiki sync [--apply]                  # dry run by default
repoforge git commit-check | branch-check
repoforge pr check
repoforge version
```

Exit codes are uniform: 0 success, 1 a finding or pending change,
2 an invocation or configuration error. A missing prerequisite is
exit 2, never a skipped check.
Reference: [docs/reference/cli.md](docs/reference/cli.md).

## Installation

Prebuilt static binaries are attached to every release. Supported
build matrix: Linux and macOS on amd64 and arm64; Windows on amd64
only. Archive names use lowercase OS spellings, exactly as emitted:

```sh
TAG=v0.1.0
gh release download "$TAG" --repo FernSyntax/repoforge \
  repoforge_0.1.0_linux_amd64.tar.gz \
  repoforge_0.1.0_linux_amd64.tar.gz.sbom.json \
  checksums.txt --dir ./repoforge-dist
cd ./repoforge-dist

# Verify integrity. --ignore-missing covers exactly the files
# downloaded: checksums.txt lists every release asset, so a plain
# `sha256sum -c` would fail on the files not fetched.
sha256sum --ignore-missing -c checksums.txt

# Verify build provenance for the archive being installed.
gh attestation verify repoforge_0.1.0_linux_amd64.tar.gz --repo FernSyntax/repoforge

# Extract and confirm the binary reports its own version.
tar -xzf repoforge_0.1.0_linux_amd64.tar.gz repoforge
./repoforge version
```

Or build from source (the `go` directive in `go.mod` is the floor):

```sh
go install github.com/FernSyntax/repoforge/cmd/repoforge@latest
```

## Example workflow

```sh
gh issue create --title "Add search" --body "What/why."
git checkout -b feat/12-add-search
# ... work ...
git add . && git commit -m "feat(search): add query parser"
git push -u origin feat/12-add-search
gh pr create --title "feat(search): add query parser" --body "Closes #12."
# CI runs: validation, tests, security, dependency review, secret detection.
gh pr checks --watch
gh pr merge --squash --delete-branch   # after green
```

## Configuration

Adopters edit one file: `repoforge.yaml` (or
`.repoforge/config.yml` in generated projects). Pick a
governance profile (`solo`, `team`, `strict`), set merge methods and
security posture, declare path ownership. The schema rejects
unknown fields; `validate` explains violations with file and rule.
See [docs/reference/configuration.md](docs/reference/configuration.md).

## Customization

Configure, don't fork: policy fields, governance profiles, and
template variables (`ProjectName`, `Owner`, `RepoURL`,
`Description`, `StandardVersion`) cover adoption. Template sources
contain only intentional placeholders from that closed set —
anything else fails validation. Project code, docs, and workflows
beyond the managed baseline are yours; user-owned paths are never
touched by tooling.

## Compatibility

- Git: any recent version (tests shell out to it).
- Go toolchain: see `go.mod` (`go` directive is the floor).
- GitHub: REST API behavior verified against live documentation
  during implementation; plan-dependent features are detected, not
  assumed — see the support matrix.
- Runners: `ubuntu-latest` for CI; scripts are POSIX `sh`.

## Contributing

See [CONTRIBUTING.md](CONTRIBUTING.md): trunk-based flow
(issue → branch → PR → squash merge), Conventional Commits, green
CI before merge, no self-approving. Good first reads:
[docs/development/getting-started.md](docs/development/getting-started.md)
and [docs/architecture/overview.md](docs/architecture/overview.md).

## Support

See [SUPPORT.md](SUPPORT.md).

## License

[Apache-2.0](LICENSE)
