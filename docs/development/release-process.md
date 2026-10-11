# Release Process

Part of the [Git workflow](git-workflow.md). Implements spec.md §14
(Release model). Rule levels (invariant, enforced, recommended,
project-specific) are defined in spec.md.

This document is the authoritative release procedure. There is no
second release guide.

## Policy

**R1. Versions follow Semantic Versioning 2.0.0.** [Invariant]
Given `MAJOR.MINOR.PATCH`: breaking changes increment MAJOR,
backward-compatible capabilities increment MINOR, backward-compatible
fixes increment PATCH.

**R2. Pre-1.0, minor versions may break compatibility.** [Enforced]
Every such break is recorded in CHANGELOG.md (spec.md §14.1) and
declared in the pull request that introduces it (pull-requests.md).

**R3. Release tags are named `vMAJOR.MINOR.PATCH`, are annotated, and
point at `main`.** [Enforced]
Annotated (`git tag -a`), never lightweight: the tag object carries
the release identity and message. A tag is never moved or reused once
published (spec.md §14.6).

**R4. Every release is recorded in CHANGELOG.md.** [Enforced]
Keep a Changelog format: the `Unreleased` section is moved to
`[vX.Y.Z] - YYYY-MM-DD` with the change categories preserved.

**R5. Every tag has a GitHub release whose notes are the tag's
`CHANGELOG.md` section.** [Enforced]
The section is extracted by `scripts/release-notes.sh` and passed to the
publisher. Release notes are never generated from commit subjects: the
changelog is the release record a human wrote and reviewed.

**R6. Release tooling supports dry-run for every external action and
prints every action it takes.** [Enforced — spec.md §14.5]
Tagging, pushing, and publishing are external side effects; the release
validation workflow is a read-only preview of what the release job will
do.

**R10. The release pipeline is orchestrated.** [Invariant — spec.md §14.3.2]
A pushed `v*` tag MUST NOT publish binaries merely because it exists.
Release validation is a required job of the release workflow, and
template publication runs only after the release workflow succeeded and
only against the exact commit that release targeted.

**R7. The git tag is the version source of truth.** [Invariant]
The CLI version, commit, and commit date are stamped from it at build
time via `-X .../internal/version.*` (spec.md §14.3). There is no
hand-maintained version constant.

**R8. Hotfixes ship as new patch releases from `main`.**
[Project-specific]
No release branches pre-1.0 (branching.md).

**R9. A failed or incomplete release is not a release.** [Invariant]
A tag, a draft, or a release with missing assets does not count as a
published release, and the release count is never reported as though it
does (spec.md §14.6).

## Procedure

1. **Prepare.** A release pull request moves `CHANGELOG.md` entries into
   the new version section, is merged through the normal issue-first
   process, and leaves `main` CI-green.
2. **Tag.** Create the annotated tag on the merge commit:

   ```sh
   scripts/release-check.sh vX.Y.Z   # shape, annotation, ancestry, changelog, version stamp
   git tag -a vX.Y.Z -m "vX.Y.Z"
   git push origin vX.Y.Z
   ```

   Pushing the tag is the single firing action. The check proves the
   tag commit is reachable from protected `main` history
   (`git merge-base --is-ancestor`), so a tag on an unmerged branch can
   never become a release; it fails closed on shallow clones that cannot
   prove ancestry either way.
3. **Automated, on the tag.**
   - `Release Validation` re-runs `scripts/release-check.sh` against
     the tagged commit and must pass before anything is published.
   - `Release` runs GoReleaser: static version-stamped binaries for the
     supported matrix, `checksums.txt` (SHA-256), one SPDX SBOM per
     archive, and a build-provenance attestation over the checksum
     file. The GitHub release is created with the changelog section as
     notes.
   - `Template Publication` builds the template from the tagged source
     with immutable provenance and publishes it. See
     [operations/template-publication.md](../operations/template-publication.md).
4. **Verify.** Confirm the artifacts correspond to the tagged commit:

   ```sh
   sha256sum -c checksums.txt
   gh attestation verify repoforge_X.Y.Z_Linux_amd64.tar.gz --repo FernSyntax/repoforge
   ```

5. **Failure handling.** If a release fails partway, no release exists.
   Investigate, correct the candidate locally, delete nothing, and
   re-run. There is no corrective commit after publication (spec.md §14.6).

## Relationships

- Branching: releases point at `main` only (branching.md).
- Commits: `BREAKING CHANGE:` footers drive MAJOR/MINOR selection and
  the changelog's breaking-change notes (commit-conventions.md).
- spec.md §14 is normative; this document operationalizes it.
