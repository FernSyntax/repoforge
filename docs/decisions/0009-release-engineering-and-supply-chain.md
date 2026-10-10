# ADR-0009: Release engineering and supply-chain guarantees

- Status: accepted
- Date: 2026-10-10
- Spec: spec.md §14, §18.3
- Implementation: `.goreleaser.yaml`, `.github/workflows/release.yml`,
  `.github/workflows/release.yml` (`validate` job), `scripts/release-check.sh`

## Context

Users download and execute binaries. That makes the release path a
security boundary, not a convenience: an artifact that cannot be traced
to source cannot be trusted, and a release process with manual steps
eventually produces a release nobody can reproduce.

## Decision

Semantic Versioning. Tags are strict `vMAJOR.MINOR.PATCH` and annotated,
never lightweight, and never moved once published. A tag is validated by
`scripts/release-check.sh` (shape, annotation, changelog section, and a
version-stamped build that reports the tag) before anything consumes it.

GoReleaser builds static, `-trimpath` binaries for the supported matrix,
name-stamped `repoforge_VERSION_OS_ARCH`, with version, commit, and
commit date stamped via ldflags. Commit date rather than wall clock
keeps builds reproducible. Artifacts: a SHA-256 checksum file binding
every released file, one SPDX SBOM per archive, and a build-provenance
attestation over the checksum file, so one attestation covers every
subject.

Release notes are the tag's `CHANGELOG.md` section plus a verification
footer. A tag, a draft, or a release with missing assets is not a
published release and is never reported as one.

Publication of the template is part of the same pipeline (ADR-0008).

## Alternatives considered

| Alternative | Why it was rejected |
| --- | --- |
| Wall-clock build dates | Breaks reproducibility; two builds of one commit differ. |
| Per-artifact attestations | Redundant when the checksum file already binds every subject by digest. |
| A hand-written release checklist | Unenforceable and eventually wrong. |
| Reusing or moving a published tag | Destroys the provenance chain the checksums depend on. |

## Consequences

- The release workflow is the only path that publishes, and it runs only
  on a tag.
- A release that fails partway leaves no release, not a partial one.
- The changelog is a release input: an unrecorded tag fails validation
  before any artifact is built.

## Security, maintainability, and operational implications

- Users can verify a binary against checksums and then against
  provenance, closing the "did this come from the repo" question.
- Syft, needed for SBOMs, is fetched as a pinned release binary verified
  against its published SHA-256, and lives in `RUNNER_TEMP` so it never
  dirties the checkout and trips GoReleaser's clean-tree check.
- A failed release is investigated and the candidate corrected locally;
  there is no corrective commit after publication.

## Links

- Implementation: `.goreleaser.yaml`, `.github/workflows/release.yml`
- Tests: `.github/workflows/release.yml` (`validate` job), `scripts/release-check.sh`
