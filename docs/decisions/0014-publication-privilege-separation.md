# ADR-0014: Publication privilege separation — build without the credential

- Status: accepted
- Date: 2026-10-11
- Spec: spec.md §14.3.2 (orchestrated release pipeline), §18.4 (cross-repository authentication)
- Implementation: `.github/workflows/template-publication.yml`, `internal/validate/publication_test.go`, `docs/operations/template-publication.md`, `docs/architecture/security.md`
- Implements: issue #13

## Context

The template publication workflow is the only workflow that holds the
template-publishing GitHub App private key. Until this change it did
two things in one job: it checked out the release commit
(`ref: ${{ github.event.workflow_run.head_sha || inputs.tag }}`) and
ran scripts from that checkout (validate, build, verify), and it minted
the App installation token later in the same job. The workflow header
claimed "untrusted code cannot reach the App private key" because the
trigger graph (Release → publication) limits reachable refs to release
tags — but the claim was stronger than the structure: the private key
and the execution of release-source code lived in the same job, so a
defect or compromise in a release commit could reach the credential.

Two scanners flag exactly that shape, independently:

- CodeQL `actions/untrusted-checkout/critical`: a workflow_run-triggered
  workflow checks out an event-derived ref in a job that holds
  credentials, with run steps after the checkout.
- OpenSSF Scorecard's Dangerous-Workflow check: a textual rule — any
  `actions/checkout` `ref:` containing `github.event.workflow_run` in a
  workflow_run-triggered workflow is reported regardless of job
  privileges.

Both findings are the same defect seen from two directions, and the
privilege split fixes both: the job that executes the release source
holds no credential at all, and no checkout ref carries an event
expression.

## Decision

1. The workflow is split into two jobs. `build` is unprivileged by
   construction — `contents: read`, no secrets, no id-token — and
   executes the release source: it resolves the release identity,
   checks out the resolved commit, validates, builds, verifies, and
   uploads the template. `publish` holds the credential
   (`needs: build`) and executes only trusted code: the engine from the
   default branch, plus the artifact `build` produced. The App private
   key therefore never coexists with the execution of release-source
   code.
2. The release identity is resolved through the API before anything is
   checked out: the tag must exist as a release, its ref must be an
   annotated tag object (a lightweight tag is refused), and the tag
   object must dereference to a commit SHA. The checkout is pinned to
   that resolved commit as data (`ref: ${{ steps.source.outputs.pin }}`),
   never as an event expression. Resolved data carries the same value
   without the shape both scanners flag.
3. The checkout is verified against the identity, not merely asserted:
   the tag object in the checkout must be annotated and must
   dereference to the commit the tree is sitting on (same-SHA), so a
   tree that is not the release commit cannot build.
4. Everything `publish` learns about the release is re-derived with
   trusted tooling and read-only API access before any credential
   exists: the downloaded artifact's manifest must record the commit
   the build job pinned and the digest it computed, and the tag must
   still resolve to that commit through the API. The artifact crosses
   the trust boundary as data; only the default-branch engine code
   executes in the privileged job.
5. The remaining publish steps (destination resolution, token minting,
   initial and routine publication, the CI wait, the tag and release,
   the final state verification, the generated pull request) are
   unchanged, and the publish job keeps its name and step names so the
   ordering and idempotency regression tests keep their meaning.

## Alternatives considered

- One job, checkout ref resolved through a step output. Rejected: the
  credential and release-source execution still share a job. The
  scanners' model is wrong about this trigger graph, but the structure
  is wrong in the same direction — the fix is the privilege split, not
  a ref the scanners cannot see.
- Switching the trigger from `workflow_run` to `release: published`.
  Rejected: it silences both scanners without changing the security
  posture (anyone who can publish a release can already run workflows
  with secrets), and it loses the "only after Release succeeded"
  ordering that spec.md §14.3.2 depends on.
- Suppression comments on the two findings. Rejected: a suppression
  records a decision to accept the finding; the finding describes a
  real design weakness, and the governance standard this repository
  ships must not teach suppression as a remediation.
- Re-verifying the artifact by rebuilding it in the publish job.
  Rejected: rebuilding executes release-source code — the exact thing
  the split removes — and the artifact is already proven byte-for-byte
  reproducible from its recorded source commit by the build job.

## Consequences

- A defect or compromise in a release commit can no longer reach the
  App private key: the credential exists only in a job that executes
  default-branch engine code and the verified artifact.
- The publication gains an independent re-derivation of the release
  identity (manifest + API) before the credential is minted, which the
  single-job design never had.
- The publication workflow grows one job and one verification step;
  `TestPublicationBuildsFromTheTriggeringSHA` now pins the
  no-event-expression checkout and the same-SHA verification, and the
  new `TestPublicationSeparatesBuildFromPublish` pins the privilege
  split itself.
- The build job runs the full validation and build on every
  publication, exactly as before — only its privileges changed.
