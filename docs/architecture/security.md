# Security Baseline

How RepoForge defends its own repository and what it promises —
and does not promise — about managed repositories. The normative
behavior contract is [spec.md](../../spec.md); this document
explains. For plan-dependent behavior, see
[github-capabilities.md](../reference/github-capabilities.md) and
[github-governance.md](github-governance.md).

## Threat model

**Assets:** maintainer and contributor GitHub tokens; repository
settings (merge policy, rulesets, security features); release tags
and the changelog; template output integrity (generated repositories
must not embed credentials or foreign identities); CI results as a
merge signal.

**Actors:** a well-meaning contributor who pastes a secret; a
pull request from a fork (untrusted code, by definition); a
compromised third-party dependency or Action; an exposed token; a
transient runner compromise.

**Out of scope:** vulnerabilities in GitHub itself, in upstream
dependencies (report upstream), in runner infrastructure, or
requiring physical access — see [SECURITY.md](../../SECURITY.md).
Releases are not cut yet
([release-process.md](../development/release-process.md)), so there
are no published artifacts to defend; the integrity story below
covers tags, changelog, and builds as they exist today.

## Two enforcement planes

Security controls live in exactly one of two planes. Confusing them
is how unavailable functionality ends up looking enabled.

| Control | Plane | Plan behavior |
| ------- | ----- | ------------- |
| Dependabot alerts / security updates | GitHub-native (repository setting, bootstrap-managed) | All plans; drift is a failure, never silent |
| Private vulnerability reporting | GitHub-native (repository setting, bootstrap-managed) | Documented for public repositories; elsewhere attempted live with explicit errors, never pre-classified |
| Dependabot malware alerts | UI-only: no REST API exists (open upstream issue `github/rest-api-description#6251`, confirmed 2026-10) — documented here so the baseline never pretends to manage it | Enable manually in Settings → Advanced Security (requires Dependabot alerts on); currently npm-ecosystem only. When GitHub ships an API, model it like private vulnerability reporting: probe, converge, audit. |
| Secret scanning / push protection | GitHub-native (repository setting, bootstrap-managed) | Public: free. Private: requires a Secret Protection product — `required` without it is an explicit `unsupported` diagnostic, never a silent pass |
| Code scanning default setup | GitHub-native (repository setting, bootstrap-managed, public repos) | Public: free. Private: requires a Code Security product — same explicit diagnostic |
| CodeQL advanced-setup workflow | Removed and forbidden: GitHub rejects advanced-configuration SARIF while default setup is on — default setup (row above) is the only CodeQL path |
| Dependency review workflow | Workflow check (Actions) | Free on public via the action; enforcement on private follows the Code Security product |
| Scorecard workflow | Workflow check (Actions) | Runs anywhere; SARIF upload needs code scanning to be visible |
| Secret detection workflow (gitleaks) | Workflow check (Actions) | Plan-independent: scans git history, needs no product |
| `govulncheck` | Workflow check (Actions) | Plan-independent: scans the module graph locally |

GitHub-native settings are desired state: bootstrap converges them
(and refuses silently), doctor audits them with plan-aware
severities. Workflow checks are merge signals: they fail the PR but
change nothing by themselves. A green workflow is not proof that the
corresponding native setting is on — doctor, not CI badges, is the
source of truth for settings.

## CI trust boundaries

The dangerous direction is write access meeting untrusted code.
The baseline holds four lines:

1. **Tokens are least-privilege.** Every workflow sets top-level
   `permissions: {}` and each job declares exactly what it needs.
   Jobs that execute pull-request-   controlled code (`tests`,
   validation, secret detection) hold `contents: read` and nothing
   else. The only writes in the baseline are the Scorecard SARIF
   upload to code scanning, the release job (publishing the release,
   minting and storing attestations), and the publication and
   preflight jobs (minting a GitHub App installation token, which
   requires id-token: write — the actual content writes are carried
   by that token, not by the workflow grant). Everything else is
   read-only.
2. **Checkouts do not persist credentials**
   (`persist-credentials: false`): the automatic token never reaches
   git config, so a compromised build step cannot push with it even
   where the token would otherwise allow it.
3. **No `pull_request_target`.** That trigger runs attacker-
   controlled code with a privileged token. Nothing in the baseline
   needs it: dependency review reads the PR diff through the API,
   and every other PR job is content with a read-only token.
4. **The build and the credential are separate jobs.** Template
   publication splits into an unprivileged `build` job that executes
   the release source (`contents: read`, no secrets, no id-token) and
   a privileged `publish` job that executes only default-branch engine
   code plus the verified artifact (ADR-0014, spec.md §14.3.3). The
   App private key never coexists with the execution of release-source
   code, and no checkout ref in that workflow carries an event
   expression: the release identity is resolved through the API and
   passed as data.

A machine check pins these lines down: `internal/validate`
rejects unpinned actions (spec.md §8.6), and the permissions baseline
(top-level empty, per-job explicit, no write except the Scorecard
SARIF upload, no `pull_request_target`) is asserted by
`internal/validate/security_test.go` over the real workflow files.
The publication privilege split is asserted by
`TestPublicationSeparatesBuildFromPublish`.

## GitHub token permissions

| Workflow / job | Permissions | Why |
| -------------- | ----------- | --- |
| Validation (all jobs), Tests | `contents: read` | Checkout + run local checks |
| Security / govulncheck | `contents: read` | Checkout + local module scan |
| Dependency Review | `contents: read` | Checkout + API diff review |
| Scorecard | `contents: read`, `security-events: write` | Checkout + SARIF upload (results are not published, so no `id-token`) |
| Secret Detection | `contents: read` | Full-history checkout + local scan |
| Release Validation | `contents: read` | Tag/changelog inspection + throwaway build |
| Release (publish) | `contents: write`, `id-token: write`, `attestations: write` | Publish the release and its build-provenance attestations |
| Template Publication — build | `contents: read` | Execute the release source; holds no credential (ADR-0014) |
| Template Publication — publish | `contents: read`, `id-token: write`, `actions: read` | Mint the App installation token and publish with it |
| Template Preflight | `contents: read`, `id-token: write`, `actions: read` | Prove the App and its installation scope |

Credentials resolve `GH_TOKEN` → `GITHUB_TOKEN` → the authenticated
`gh` CLI (spec.md §8.1). Tokens are held unexported, never stored, and
redacted from error output (`internal/github/client.go` replaces the
token with `[REDACTED]` before a body is embedded in an error).

## Third-party Actions

| Action | Version | Pinned SHA | Purpose |
| ------ | ------- | ---------- | ------- |
| `actions/checkout` | v7.0.1 | `3d3c42e5aac5ba805825da76410c181273ba90b1` | Source checkout |
| `actions/setup-go` | v7.0.0 | `b7ad1dad31e06c5925ef5d2fc7ad053ef454303e` | Go toolchain from `go.mod` |
| `actions/dependency-review-action` | v5.0.0 | `a1d282b36b6f3519aa1f3fc636f609c47dddb294` | PR dependency review |
| `ossf/scorecard-action` | v2.4.4 | `2d1146689b8cda280b9bc96326124645441f03bc` | Supply-chain posture |
| `gitleaks/gitleaks-action` | v3.0.0 | `e0c47f4f8be36e29cdc102c57e68cb5cbf0e8d1e` | Secret detection |

SHAs were resolved against the live release metadata at adoption
and re-verified before use — never copied from memory. Where no
trusted action exists (actionlint, shellcheck), the baseline fetches
pinned release binaries with embedded SHA-256 verification instead
of adding another third party.

## Dependency pinning guidance

Rotating a pin (including a Dependabot-proposed one) is a
security-relevant change — review it as one:

1. Read the upstream release notes for behavior and scope changes.
2. Resolve the tag to its commit SHA through the API
   (`/repos/{owner}/{repo}/commits/{tag}`); the SHA in `uses:` and
   the version in the trailing comment must agree.
3. Update both, run `repoforge validate` (pin enforcement is
   spec.md §8.6), and put the rotation in its own pull request so the
   diff stays reviewable.
4. Binary tools: same procedure against the release checksums;
   the hash embedded in the workflow is the pin.

Dependabot watches `github-actions` and `gomod` weekly and proposes
updates as pull requests; each one still passes dependency review,
the full baseline, and human review before merge.

## Dependency supply chain

The Go module has exactly one third-party dependency
(`gopkg.in/yaml.v3`, admitted by ADR 0001); `go.sum` is committed
and the module proxy serves hash-verified zips. Three independent
checks watch the graph: Dependabot proposes updates, dependency
review gates pull requests, and `govulncheck` scans the resolved
module graph in CI. Scorecard observes the repository's own
supply-chain posture weekly.

## Release artifact integrity

Tags are annotated SemVer on `main` (R3), every release is recorded
in `CHANGELOG.md` (R4), and the CLI version is stamped from the tag
at build time — there is no hand-maintained version constant (R7).
`scripts/release-check.sh` (run by Release Validation on every
pushed `v*` tag) verifies tag shape, tag object type, changelog
presence, and that a stamped build reports the tag. The tag push is
a manual maintainership action; everything after it (build,
publish, attest) runs in the Release workflow with verification at
each stage.

## Secrets in the repository

There are none, and three layers keep it that way: `validate`
rejects token- and key-shaped material (`content/secret`) and
absolute machine paths; the secret-detection workflow scans full
history on every push and PR; and fixtures use runtime-constructed,
obviously-fake values that never resemble real credentials. A
finding follows coordinated disclosure per SECURITY.md — findings
are never pasted into public logs or issues.
