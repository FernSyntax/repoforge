# RepoForge Specification

Status: accepted (v0.1.0)
Normative language: the key words MUST, MUST NOT, SHOULD, SHOULD NOT,
and MAY are to be interpreted as described in RFC 2119/8174.
Terminology: "RepoForge" is the product; `repoforge` is its CLI, module,
and machine-readable identifier. "the standard" is the governance model
this document defines. "a managed repository" is a repository carrying
the RepoForge policy. "the template" is the generated repository at
`FernSyntax/repoforge-template`; "the engine" is the RepoForge
implementation repository at `FernSyntax/repoforge`.

This document is the source of project truth. AGENTS.md, README.md, and
docs/ are derived views; where they conflict with this specification,
this specification wins and the derived view is a bug.

Rule classification: rules in this specification and its companion
documents are classified INVARIANT (must always hold; a violation is a
defect in process or tooling), ENFORCED (blocked in the normal flow by
CI, GitHub settings, or required review), RECOMMENDED (the default
expectation; deviation requires stated justification), or
PROJECT-SPECIFIC (an opinion of RepoForge itself; adopters of the
standard MAY configure these differently).

---

## 1. Purpose, users, and goals

### 1.1 Problem

Repository configuration is normally tribal knowledge: who protects
`main`, which workflow is current, whether secret scanning is on, what
the release procedure is. Nothing detects drift, nothing explains a
setting, and a new contributor inherits the folklore or rediscovered it.
RepoForge replaces that with a declarative policy, a deterministic
validator, a read-only auditor, an idempotent converger, and a
generated baseline repository — all of it reviewable in a diff.

### 1.2 Intended users

| User | What they do with RepoForge |
| --- | --- |
| Repository maintainer | Declares the policy; runs `bootstrap`, `doctor`, `validate`. |
| Contributor | Reads the generated baseline and `spec.md`; works issue-first. |
| Release engineer | Runs the release pipeline; verifies artifacts and the template. |
| Security reviewer | Reads the threat model (§19) and the supply-chain guarantees (§18). |

### 1.3 Goals and measurable outcomes

| ID | Goal | Outcome measure |
| --- | --- | --- |
| G-1 | Repository state is explicit and reviewable | `validate` is offline and deterministic; a second run on the same tree produces a byte-identical report. |
| G-2 | Convergence is safe | `bootstrap --dry-run` performs zero mutations, proven by test. |
| G-3 | Diagnosis never mutates | every client call `doctor` makes is a GET, proven by test. |
| G-4 | The template is reproducible | a fresh build of the recorded source commit is byte-identical to the published template, proven in template CI. |
| G-5 | The standard is traceable | every requirement in §20 and §21 maps to an implementation and a test in §22. |

### 1.4 Product shape

The product consists of two repositories:

1. **repoforge** (the engine) — CLI implementation, policy
   specification, schemas, tests, documentation, release tooling, and
   the template builder.
2. **repoforge-template** (the template) — the generated GitHub
   template repository consumed by developers. It contains only the
   generated baseline and MUST NOT contain RepoForge implementation
   source.

## 2. Principles

P1. **Simple, explicit architecture over clever abstraction.** If a
    design needs a paragraph to justify its indirection, the design is
    wrong.
P2. **Declarative policy.** Desired state is data, version-controlled
    and schema-validated, not code and not convention.
P3. **The GitHub API is the source of truth** for remote repository
    state. Local caches are advisory only.
P4. **Idempotent convergence.** Applying the policy twice converges to
    the same state as applying it once.
P5. **Read-only diagnosis.** Auditing never mutates.
P6. **Deterministic validation.** Same inputs, same verdict, every
    time, on every machine.
P7. **No silent mutation.** Every change is planned, displayed, and —
    for external effects — gated behind explicit confirmation or an
    explicit non-dry-run flag.
P8. **No stored credentials, ever.** Credentials come from the
    environment or the user's own tooling at invocation time.
P9. **CI is authoritative.** Local checks are developer feedback; merge
    gating happens in CI only.
P10. **Useful to humans without AI.** All behavior is documented in
    plain language; no workflow requires an AI assistant.
P11. **Small dependency surface.** Go standard library wherever
    practical; every third-party dependency requires written
    justification in an ADR.

## 3. Scope

In scope:

- Definition of the repository policy and its schemas.
- `repoforge` CLI: `validate`, `doctor`, `bootstrap`, `template`,
  `wiki`, `git`, `pr`, and `version` (see §9).
- Generation of the repoforge-template baseline from the canonical
  `template/` source, with a deterministic, provenance-recording
  manifest.
- Cross-repository publication of the template to
  `FernSyntax/repoforge-template` using a dedicated, narrowly scoped
  GitHub App.
- Synchronisation of GitHub wiki pages from `docs/wiki/`.
- Release tooling for the CLI and the template, including checksums,
  SBOMs, and build provenance.
- Documentation, ADRs, CI, and developer hooks for all of the above.

## 4. Non-goals

- Hosting or SaaS. RepoForge is a CLI; there is no service.
- Language-specific tooling (linters, formatters, build systems) beyond
  wiring the standard CI hooks. RepoForge is language-agnostic.
- Replacing GitHub branch protection, rulesets, or security features
  with parallel mechanisms; RepoForge configures and audits them, it
  does not reimplement them.
- Managing repository *content* (application code, docs prose) beyond
  the baseline files the policy owns.
- Organization-wide fleet dashboards. v0.1 operates on one repository
  at a time.
- Backward compatibility with other repository-management tools.

## 5. Repository model

5.1. A **managed repository** is a GitHub repository that carries the
     RepoForge policy file at the repository root and is subject to
     bootstrap, doctor, and validate.

5.2. The policy **owns** a declared set of files (the baseline). Owned
     files are managed: bootstrap may create or update them, validate
     checks them, and manual edits to them are drift.

5.3. Files not owned by the policy MUST NOT be created, modified, or
     deleted by any RepoForge command.

5.4. The policy file and every owned file MUST be expressible in plain
     text. No binary artifacts in the baseline.

5.5. The baseline MUST NOT contain credentials, tokens, email
     addresses, or hardcoded user/organization/repository names. All
     ownership-specific values are inputs to bootstrap, not constants in
     the standard.

## 6. Git model

6.1. The default branch is `main`.

6.2. RepoForge never rewrites published history: no force-push, no
     rebase of shared branches, no amend of pushed commits.

6.3. RepoForge commands never create commits unless the invoking
     task or user explicitly supplies the commit message. Autonomous
     "fixup" commits are forbidden.

6.4. RepoForge never pushes unless explicitly requested.

6.5. Tracked files MUST have deterministic line endings (LF) enforced
     via `.gitattributes`.

6.6. Local working-tree state (uncommitted changes) is data, not an
     obstacle: commands MUST NOT silently discard, stash, or overwrite
     it. A command that cannot proceed without touching it MUST stop and
     report.

6.7. **Branching.** `main` is the only permanent branch. Topic branches
     MUST match `<type>/<issue>-<description>` with type from the set
     {feat, fix, refactor, perf, test, docs, build, ci, security,
     chore}, a numeric issue reference, and a lowercase kebab-case
     description. Normative detail: docs/development/branching.md.

6.8. **Commits.** Commit messages MUST follow the restricted
     Conventional Commits subset defined in
     docs/development/commit-conventions.md, with types limited to
     {feat, fix, docs, refactor, perf, test, build, ci, chore, revert,
     security}. Breaking changes MUST carry both the `!` marker and a
     `BREAKING CHANGE:` footer.

6.9. **Merge policy.** Squash merge is the default; merge commits and
     rebase merges are disabled by default (PROJECT-SPECIFIC); source
     branches are deleted after merge. `main` is the historical record
     of completed changes: every commit on it is reviewed and CI-green
     (INVARIANT). Normative detail: docs/development/pull-requests.md.

6.10. The workflow documents in docs/development/ (git-workflow.md,
      branching.md, commit-conventions.md, pull-requests.md,
      release-process.md) are normative extensions of this section and
      of section 14. Each rule they define is classified INVARIANT,
      ENFORCED, RECOMMENDED, or PROJECT-SPECIFIC.

## 7. GitHub model

7.1. The GitHub REST API is the source of truth for remote state:
     repository settings, branch protection, rulesets, collaborators,
     and template configuration.

7.2. Implementations MUST NOT invent API fields. Before relying on a
     field, verify it against current official GitHub documentation or
     API schemas. Undocumented or preview fields require an ADR noting
     the stability risk.

7.3. All remote mutations are explicit, auditable, and dry-runnable:

- Every API-mutating operation MUST have a dry-run mode that prints the
  exact calls it would make.
- Mutating operations MUST log what changed, before and after, at a
  verbosity the user controls.
- No mutation is ever a side effect of a read (see section 11).

7.4. Rate-limit and permission failures MUST surface as clear errors,
     never as silent skips.

7.5. Convergence MUST be achieved with the minimum set of API calls:
     compare before mutate, and mutate only on actual drift.

7.6. When the contract requires code scanning, the desired main
     ruleset MUST include the CodeQL default-setup analysis contexts as
     required status checks (docs/architecture/github-governance.md).
     A SAST that runs but does not gate merges does not satisfy the
     security model: the analysis must be required for every change,
     which is what blocks a merge on a finding.

## 8. Security model

8.1. **Credential sources.** In priority order: `GH_TOKEN`, then
     `GITHUB_TOKEN`, then the authenticated GitHub CLI (`gh`) session.
     RepoForge reads credentials at invocation time and MUST NOT
     persist them to disk, config, logs, or fixtures.

8.2. **Token hygiene.** Tokens MUST NOT appear in stdout, stderr, log
     files, error messages, test fixtures, or CI artifacts. Any output
     path that could contain a token MUST redact it.

8.3. **Least privilege.** Documentation MUST state which token scopes
     each command requires. The tool MUST fail with an actionable error
     when the credential lacks a required scope.

8.4. **No hardcoded identities.** Source, schemas, templates, and tests
     MUST NOT hardcode users, organizations, repository names, email
     addresses, tokens, or ownership values. Such values are runtime
     inputs.

8.5. **Explicit, auditable security operations.** Any operation that
     changes a security-relevant setting (branch protection, rulesets,
     collaborator access, visibility) MUST be individually listed in the
     plan output and individually confirmed in non-dry-run execution.

8.6. **Supply chain.** Third-party Go dependencies require ADR
     justification (P11). GitHub Actions used in workflows MUST be
     pinned by commit SHA once CI stabilizes beyond the skeleton phase.

8.7. **Reporting.** Vulnerabilities are reported privately per
     SECURITY.md, never in public issues.

## 9. Configuration model

9.1. Policy is a single declarative file, machine-validated against a
     versioned JSON Schema in `schemas/`. Accepted locations, probed in
     order: `repoforge.yaml`, `.yml`, `.json` at the repository
     root, then `.repoforge/config.yml`, `.yaml`, `.json` (the
     directory form used by generated projects). At most one location
     is used per repository. The canonical schema is
     `schemas/repoforge.schema.yaml`;
     `schemas/repoforge.schema.json` is a derived translation and
     both MUST parse to identical documents. Field semantics are
     documented in docs/architecture/configuration-model.md. Ownership
     glob semantics are `path.Match` extended with `**` (recursive,
     matches any number of path segments including zero).

9.2. The policy schema is versioned. The policy file declares its
     schema version explicitly in `standard.version`. Tools MUST reject
     policies with unknown schema versions. Unknown fields are
     detectable: every schema object sets `additionalProperties:
     false`, and validators MUST surface unknown fields as errors.

9.3. Policy semantics are deterministic: no timestamps, randomness,
     environment-dependent values, or network lookups influence
     validation results.

9.4. Defaults are part of the schema, not hidden in code. An omitted
     field and the field set to its schema default MUST produce
     identical behavior.

9.5. The configuration model MUST be usable by humans directly:
     hand-writable, reviewable in diffs, documented in docs/reference/.

## 10. Bootstrap model

10.1. `repoforge bootstrap` converges a repository — local files and
      GitHub settings — to the policy.

10.2. **Idempotence.** Running bootstrap twice in a row MUST converge
      to the same state and MUST report zero changes on the second run.

10.3. **Phases.** Bootstrap proceeds in two explicit phases:

  a. **Plan**: compute the full diff between current state (local
     working tree + GitHub API state) and desired state; print every
     intended change.
  b. **Apply**: execute the plan, item by item, reporting each outcome.

10.4. **Dry-run.** `--dry-run` executes only the plan phase. It MUST
      make no local modifications and no API mutations.

10.5. **Ownership boundary.** Bootstrap touches only policy-owned files
      and policy-described GitHub settings (5.2, 5.3).

10.6. **Failure behavior.** On error, bootstrap stops, reports what was
      already applied and what remains, and leaves the repository in a
      state from which re-running bootstrap converges. Partial
      application is resumable, never corrupting.

10.7. Bootstrap MUST NOT create commits or push (6.3, 6.4).

## 11. Doctor model

11.1. `repoforge doctor` audits a repository against the policy and
      reports drift: missing owned files, modified owned files, GitHub
      settings diverging from policy, missing credential scopes.

11.2. **Read-only.** Doctor MUST NOT modify the local working tree,
      the local git state, or any GitHub state. It issues only read
      requests. This is a hard invariant with machine-checkable tests.

11.3. Doctor output is a structured list of findings, each with:
      severity, what drifted, current value, policy value, and the
      remediation (`repoforge bootstrap`).

11.4. Exit codes: 0 = no drift; 1 = drift found; 2 = audit could not
      complete (error). CI treats 1 as a merge blocker where
      configured; doctor itself never blocks locally (P9).

## 12. Validation model

12.1. `repoforge validate` checks the local checkout against the
      standard: policy file validity (against its schema), presence and
      content of owned files, and internal consistency. The concrete
      check inventory — baseline files, ownership declarations, branch
      naming, commit message policy, issue/workflow/GitHub YAML, action
      pinning, template placeholders, secret material, absolute machine
      paths, internal links — is maintained in docs/reference/cli.md;
      that inventory is a normative extension of this section.

12.2. **Determinism.** Validate MUST NOT consult the network, the wall
      clock, randomness, or machine-specific state. Identical checkouts
      MUST produce identical results everywhere. This is a hard
      invariant with machine-checkable tests.

12.3. Validate is local and offline. Anything requiring GitHub state
      belongs to doctor, not validate.

12.4. Validate runs in CI on every pull request and is the primary
      merge gate for the baseline (P9).

12.5. Validate exits 0 on pass, 1 on violation, 2 on tooling error,
      and prints findings in a stable, diffable order.

## 13. Template model

13.1. The repoforge-template repository contains the generated
      baseline: exactly the policy-owned files, rendered for a generic
      new repository, plus template-specific README guidance.

13.2. The template is **generated** by the template builder
      (`internal/template`) from the same policy and schemas the CLI
      uses. It is never hand-maintained: drift between policy and
      template is a bug caught by CI.

13.3. The template MUST NOT contain RepoForge implementation source
      code, Go module files, or development tooling for RepoForge
      itself.

13.4. The template MUST NOT contain hardcoded identities (8.4).
      Ownership values are placeholders filled by the consumer or by
      bootstrap on first run.

13.5. The generated output is deterministic: identical policy input
      produces byte-identical template output.

## 14. Release model

14.1. Versioning: Semantic Versioning 2.0.0. Pre-1.0, minor versions
      MAY break compatibility; each such break MUST be listed in
      CHANGELOG.md.

14.2. Releases are tagged `vMAJOR.MINOR.PATCH` on `main` with
      **annotated** tags (never lightweight). Tags are created only by
      release tooling under docs/operations/, never ad hoc.

14.3. Every release MUST: pass full CI, update CHANGELOG.md (Keep a
      Changelog format), stamp the CLI version via
      `-X .../internal/version.Version`, and publish a GitHub release
      whose notes are the tag's `CHANGELOG.md` section — extracted by
      `scripts/release-notes.sh` and passed to the publisher, never
      generated from commit subjects.

14.3.2. **The release pipeline is orchestrated, not concurrent.** Release
         validation is a required job of the release workflow, so a
         pushed `v*` tag cannot publish binaries merely because it
         exists. Template publication is a separate workflow triggered
         only after the release workflow has succeeded, and it verifies
         that the release for that tag targets the exact commit being
         built.

14.3.1. The operational release procedure is defined in
      docs/development/release-process.md, which is a normative
      extension of this section (6.10).

14.4. Release artifacts: the CLI source (go install-able) and the
      regenerated template repository. Prebuilt binaries MAY be added
      later; they are not required for v0.1.

14.5. Releases are auditable: the release tooling prints every external
      action and supports dry-run for all of them.

## 15. Upgrade philosophy

15.1. Managed repositories upgrade by re-running bootstrap with a newer
      policy version. Upgrades are convergent, not migratory: bootstrap
      computes desired state from the new policy and reconciles.

15.2. No hidden migrations. A schema version change that alters
      semantics MUST be documented in CHANGELOG.md and, when behavior
      changes materially, in an ADR.

15.3. The standard evolves slowly and deliberately. Additions are
      preferred over changes; changes over removals. Removals require a
      deprecation window of at least one minor release.

## 16. Compatibility philosophy

16.1. **CLI**: pre-1.0, command names and flags MAY change with
      changelog notice. Post-1.0, the command surface is stable within a
      major version.

16.2. **Policy schema**: schema versions are additive within a major
      version. Older tools MUST reject newer schema versions cleanly
      (9.2); newer tools SHOULD accept older schema versions within the
      same major version.

16.3. **GitHub API**: RepoForge tracks the documented REST API. When
      GitHub deprecates a field RepoForge relies on, adapting is
      treated as a bug fix, not a feature.

16.4. **Go**: the CLI builds on the two most recent Go minor releases.
      `go.mod` declares the minimum.

## 17. Filesystem safety model

17.1. **Path resolution.** Every write resolves its destination with
       symlinks evaluated before any decision is made, so a symlink
       into a protected tree cannot disguise a target.

17.2. **Refused targets.** A write MUST refuse, with an actionable
       error and no side effect: `.` and `..`; empty or absolute paths
       where a relative one is required; a home directory; the
       filesystem root; any ancestor of the repository; the engine
       checkout; the template source; a path outside the governed
       repository; and a non-directory destination.

17.3. **Non-empty destinations.** Replacing a non-empty destination
       requires an explicit opt-in. The opt-in never bypasses §17.2.

17.4. **Atomic publication.** Output is written to a temporary
       directory beside the destination and swapped into place by
       rename. If the swap fails after the previous content is moved
       aside, the previous content is moved back, and the failure is
       reported with the exact restore instruction. A command reported
       as successful MUST NOT leave a partial tree.

17.5. **Preservation.** Content outside the managed set is preserved
       byte-for-byte. `validate`, `doctor`, and every dry run MUST
       leave the surrounding filesystem untouched, and this is proven
       by tests that snapshot the tree around a refused operation.

## 18. Threat model and supply-chain controls

### 18.1 Assets

| Asset | Why it matters |
| --- | --- |
| GitHub credentials | A leaked or over-scoped token can change any repository the principal can reach. |
| The engine repository | It is the only routine writer of template content. |
| The template repository | Consumed by every adopting project. |
| Release artifacts | Downloaded and executed by users. |
| The wiki | A generated surface that could otherwise become a second source of truth. |

### 18.2 Threats and mitigations

| Threat | Control |
| --- | --- |
| A token is committed | `validate` and `template` scan for token- and key-shaped material; secret scanning with push protection is part of the standard. |
| A token appears in output | Error paths construct messages from fixed text; the token is never interpolated. Tests assert redaction. |
| Template tampering | The immutable manifest plus rebuild-from-source verification (§13.7). |
| An untrusted pull request reaches a publishing credential | Pull-request workflows never receive the template-publishing credential; publication runs only on the trusted release workflow, on the expected repository and tag. |
| A dependency changes under the build | Actions are pinned to full commit SHAs; downloaded tools are verified against published SHA-256 sums; Go module integrity is checked. |
| A diverted wiki destination | The publish path verifies the clone's origin against the requested repository before writing, and never force-pushes. |
| A stale or wrong wiki | `wiki sync` is a dry run by default and never deletes pages outside the declared set. |

### 18.3 Supply-chain guarantees

Published engine releases carry SHA-256 checksums, an SPDX SBOM, and a
build-provenance attestation over the checksum file, so a user can
verify that a downloaded binary came from this repository's tagged
source. Template CI independently rebuilds the template from the source
commit recorded in the manifest, so a template that does not match its
declared provenance fails.

### 18.4 Cross-repository authentication

Template publication uses a dedicated GitHub App installed on the
template repository alone, with a token minted only inside the trusted
publication job. Repository metadata: read. Contents: write, for
generated commits and tags. Workflows: write, because generated content
includes `.github/workflows/`. Pull requests: write, for generated
update PRs. No administration permission is granted: repository
configuration that needs administration is performed once by the owner.
The publishing job verifies the target owner/repository before any
write and fails closed on mismatch.

## 19. CLI surface

19.1. The public commands, their options, and their exit codes are
       maintained in docs/reference/cli.md, which is a normative
       extension of this section.

19.2. Every command exits 0 on success, 1 on a finding or pending
       change, and 2 on an invocation or configuration error. A missing
       prerequisite is an invocation error (2), never a skipped check.

19.3. Commands are read-only unless an explicit flag or a documented
       confirmation asks for a write: `validate` and `doctor` are
       read-only by construction; `bootstrap` and `template adopt`
       require `--yes` or interactive confirmation; `wiki sync` is a
       dry run unless `--apply` is given; `template build` writes only
       to its `--out` destination.

19.4. Every command reports in a stable, diffable order. `--json`
       emits the same information as structured JSON.

## 20. Issue-driven project management

20.1. Issues are the normal unit of work. Every change starts from an
       issue that names the requirement or behaviour it affects, unless
       it is explicitly a maintenance task that creates no requirement.

20.2. Pull requests MUST state the problem, the linked issue
       (`Closes #N`), the affected requirement IDs, the design
       decision, the tests added, the exact validation commands run,
       documentation and ADR changes, compatibility and security
       effects, and known limitations. A CI check enforces linkage and
       the presence of these sections, and validates issue existence
       when the event payload and token permit.

20.3. Exceptions to §20.2 are narrow and auditable: GitHub-generated
       dependency pull requests, and urgent security work, which is
       named as such in the pull request and reviewed by a maintainer.
       There is no general self-applied bypass label. The repository
       initialization commit is a documented exception because a new
       repository has no prior issue to reference.

20.4. Every material feature change MUST update `spec.md` or state in
       the pull request why it does not. CI checks structural
       traceability; semantic adequacy is a human judgement.

20.5. Specifications, issues, and ADRs stay connected: ADRs live in
       `docs/decisions/`, are numbered `NNNN-kebab-case-title.md`, and
       are indexed in `docs/decisions/README.md`. Every reference MUST
       resolve; CI checks the index and the links.

20.6. Comments have engineering value: exported APIs are documented,
       comments explain invariants, security boundaries, and
       non-obvious choices. No comment paraphrases the line beneath it.
       No TODO or FIXME without a linked issue.

## 21. CI/CD, hooks, and release automation

21.1. CI is authoritative (P9). Local hooks are developer feedback and
       are bypassable; CI enforces the same mandatory checks regardless.

21.2. Required checks cover formatting, `go vet`, build, tests, race
       tests, static analysis, schema syntax and pair parity,
       configuration validation, template-source validation, template
       build with manifest and determinism proof, branding and leak
       checks, release-configuration validation, workflow and shell
       linting, issue-form and PR-template validation, and
       specification/ADR structural checks. A missing required tool
       fails with installation instructions; it never degrades to a
       pass.

21.3. Every workflow sets top-level `permissions: {}` and grants the
       minimum per job. Third-party actions are pinned to full commit
       SHAs. Jobs use timeouts and concurrency control. Untrusted pull
       request code is never executed with a privileged credential.

21.4. `scripts/verify` is the single authoritative local entry point.
       It resolves the repository root, prints each command, propagates
       exit codes, and distinguishes required tools from optional ones.

21.5. Developer hooks: a fast blocking pre-commit set; a commit-message
       hook enforcing the Conventional Commits subset; a pre-push hook
       running the full local suite. Post-event hooks are informational
       only and MUST NOT claim to undo an event that already happened.
       Installation and removal are idempotent, and removal touches only
       hooks the project installed.

21.6. Template synchronization (§13.8) is automated end to end: the
       release pipeline builds the template from the tagged source,
       verifies it, and either publishes the initial root commit or
       opens a generated pull request. Re-running a publication for an
       already-applied source version is a no-op that reports the
       target is synchronized.

## 22. Requirements and traceability

### 22.1 Functional requirements

| ID | Requirement | Classification | Implementation | Tests |
| --- | --- | --- | --- | --- |
| FR-001 | `validate` is offline and deterministic | INVARIANT | internal/validate | validate_test.go |
| FR-002 | `doctor` issues only read requests | INVARIANT | internal/doctor | doctor_test.go |
| FR-003 | `bootstrap` is plan → confirm → apply → verify and idempotent | INVARIANT | internal/bootstrap | bootstrap_test.go, lifecycle_test.go |
| FR-004 | `bootstrap --dry-run` mutates nothing | INVARIANT | internal/bootstrap | security_test.go |
| FR-005 | Policy loading rejects unknown fields and unknown schema versions | ENFORCED | internal/config | config_test.go, schema_test.go |
| FR-006 | YAML and JSON policy semantics are identical | ENFORCED | internal/config, scripts/check-schema-parity | schema_test.go, verify_scripts_test.go |
| FR-007 | Ownership glob patterns follow the documented contract | INVARIANT | internal/config | glob_test.go |
| FR-008 | The template builder is deterministic, including file modes | INVARIANT | internal/template | template_test.go, manifest_test.go |
| FR-009 | Template output refuses dangerous destinations and publishes atomically | INVARIANT | internal/template | outpath_test.go, security_test.go |
| FR-010 | Template adoption requires every known variable and validates rendered output | ENFORCED | internal/template | adopt_test.go |
| FR-011 | A built template records an immutable provenance manifest | ENFORCED | internal/template | manifest_test.go |
| FR-012 | A built template verifies against a fresh build of its recorded source commit | ENFORCED | internal/template | template verify, template CI |
| FR-013 | A policy that declares a governance profile keeps Issues and the wiki enabled | ENFORCED | internal/config | governance_test.go |
| FR-014 | Governance profiles require Issues and the wiki | ENFORCED | internal/config, configs/github | governance_test.go |
| FR-015 | `wiki sync` is a dry run by default and never deletes undeclared pages | INVARIANT | internal/wiki | wiki_test.go |
| FR-016 | `wiki sync` refuses a foreign destination and never force-pushes | INVARIANT | internal/wiki | wiki_test.go |
| FR-017 | `wiki sync` distinguishes a missing wiki from an auth failure | ENFORCED | internal/wiki | wiki_test.go |
| FR-018 | `pr check` enforces branch and message policy locally | ENFORCED | internal/gitrules | gitrules_test.go |
| FR-019 | Pull requests state their linked issue and required sections | ENFORCED | CI, internal/gitrules | prcheck_test.go |
| FR-020 | Release tags are strict SemVer, annotated, changelogged, and on mainline history | ENFORCED | scripts/release-check.sh | release validation workflow |
| FR-021 | Release artifacts carry checksums, an SBOM, and provenance | ENFORCED | .goreleaser.yaml, release.yml | release.yml `validate` job, `scripts/release-check.sh` |
| FR-022 | The tracked tree carries no legacy product branding | ENFORCED | internal/brand | brand_test.go, validate_test.go |
| FR-023 | Generated output carries no engine implementation source | ENFORCED | internal/template | template_test.go |
| FR-024 | Local verification is a single entry point that fails closed | INVARIANT | scripts/verify | verify_scripts_test.go |
| FR-025 | `template verify --require-source` refuses an untraceable tree | ENFORCED | internal/template | manifest_test.go, verify.go |
| FR-026 | The GitHub API client escapes every path component | INVARIANT | internal/github | path_test.go |
| FR-027 | Tokens never appear in errors or output | INVARIANT | internal/github | client_test.go, security_test.go |
| FR-028 | An ambiguous mutation is reported as unknown, never as success | INVARIANT | internal/github | client_test.go |
| FR-029 | Generated repositories carry a fill-in-ready `spec.md` | ENFORCED | template/spec.md, internal/template | template_test.go |
| FR-030 | Generated repositories carry issue/PR forms, an ADR process, wiki seed, and a definition of done | ENFORCED | template/ | template_test.go |

### 22.2 Non-functional requirements

| ID | Requirement | Classification | Verification |
| --- | --- | --- | --- |
| NFR-001 | Validation output is stable and diffable | INVARIANT | determinism tests |
| NFR-002 | Remote failures are distinguished from drift | INVARIANT | doctor exit-code tests |
| NFR-003 | The CLI builds on the two most recent Go minors | RECOMMENDED | CI build matrix |
| NFR-004 | Documentation has no broken internal links | ENFORCED | `validate` link check |
| NFR-005 | No credentials, machine paths, or fabricated results in tracked files | ENFORCED | `validate` content checks |
| NFR-006 | The product is usable without an AI assistant | INVARIANT | documentation review |

### 22.3 Test strategy

| Layer | Scope | Gate |
| --- | --- | --- |
| Unit | Every package's public behaviour | `go test ./...` |
| Race | Concurrency-sensitive paths | `go test -race ./...` |
| Fuzz | Parsers, placeholders, and path handling | `go test -run=Fuzz -fuzz` (bounded) |
| Integration | Live GitHub against a disposable repository | `scripts/verify full --with-integration` (opt-in) |
| End-to-end | Full lifecycle against a disposable repository | `REPOFORGE_E2E=1` (opt-in) |
| Template | Build, manifest, determinism, leak checks | `scripts/verify full` |
| Hosted CI | All of the above plus workflow and shell lint | Required checks |

A requirement with no automated verification is either narrowed to
what can be verified, or carries an explicit, documented platform
limitation.

### 22.4 Traceability

Every requirement above maps to at least one file in `internal/` or
`scripts/` and at least one test in the same table. The mapping is
maintained in this section; a requirement whose implementation is
removed MUST be removed from this table in the same change.

## 23. v0.1.0 acceptance criteria

RepoForge v0.1.0 is complete when all of the following hold, each with
observable evidence in the final report:

A1. Every requirement in §22 maps to an implementation and a test, and
    the full test suite passes with `go test ./...` and
    `go test -race ./...`.
A2. The engine repository's initial `main` history is exactly one
    commit containing the complete v0.1.0 baseline, and its only
    initial release is `v0.1.0`, targeting that commit.
A3. `repoforge validate` runs offline and deterministically on the
    engine checkout and exits 0; the branding gate (§8.8) finds no
    legacy product reference in any tracked file.
A4. `repoforge doctor` issues only read requests, proven by test
    (FR-002); its exit codes distinguish drift (1) from an incomplete
    audit (2).
A5. `repoforge bootstrap --dry-run` performs zero mutations and
    `bootstrap` converges and re-converges to zero changes, proven by
    test (FR-003, FR-004).
A6. The schema pairs parse to identical documents
    (`scripts/check-schema-parity`), and unknown fields and unknown
    schema versions are rejected (FR-005, FR-006).
A7. The template builds deterministically from `template/`, carries a
    valid provenance manifest, leaks no engine implementation source,
    and `scripts/verify-template` proves a byte-identical rebuild
    (FR-008, FR-011, FR-023).
A8. `repoforge template verify --require-source` refuses a tree whose
    manifest does not record an immutable source commit (FR-025), and
    template CI rebuilds from that commit and compares byte-for-byte
    (FR-012).
A9. `repoforge wiki sync` defaults to a dry run, never deletes
    undeclared pages, refuses a foreign destination, never force-pushes,
    and distinguishes a missing wiki from an auth failure
    (FR-015…FR-017).
A10. The engine release carries static version-stamped binaries,
     SHA-256 checksums, an SPDX SBOM, and a build-provenance
     attestation over the checksum file (FR-021).
A11. The template repository's initial `main` history is exactly one
     commit, generated from the tagged engine source, containing its
     own `spec.md`, issue/PR forms, ADR process, wiki seed, and
     workflows; its only initial release is `v0.1.0` targeting that
     commit; and its hosted CI is green before the release is
     published.
A12. The template repository is registered as a GitHub template
     repository and its `main` and release tags are protected.
A13. Routine future template updates are generated pull requests; the
     publication workflow has no normal path that writes directly to an
     existing template `main` (§13.8).
A14. No credential, machine-specific path, fabricated result, or
     unverified claim appears in any tracked file or release artifact.
A15. `scripts/verify fast` and `scripts/verify full` both exit 0 on
     the final tree, with missing tools reported rather than skipped.
