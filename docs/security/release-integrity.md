# Release Integrity

What makes a RepoForge release trustworthy, what it does not
promise, and how to verify it. Complements the release procedure
([release-process.md](../development/release-process.md)) and the
[security baseline](../architecture/security.md).

## Threats

- **Tampered binary**: an archive whose bytes differ from what CI
  built (mirror compromise, upload interception, release-asset
  replacement).
- **Impersonated release**: notes or assets published outside the
  tag pipeline.
- **Downgrade / confusion**: a stale or wrong-version binary
  presented as current, or two different bytes under one version.
- **Silent build drift**: unrecorded inputs (dirty tree, ambient
  toolchain, wall-clock timestamps) making the binary
  unverifiable against its source.

## The chain

Each layer covers the previous one's gaps; no single layer is the
whole story.

1. **Annotated tag on `main`** (R3). The tag object — not a
   lightweight pointer — names the exact commit. Tags are
   immutable: never moved, never re-pushed.
2. **Gated inputs.** Release Validation enforces tag shape,
   annotation, and changelog presence on every pushed tag.
   Releases only happen from commits that passed full CI.
3. **Constrained inputs.** `CGO_ENABLED=0` (static binaries),
   `-trimpath` (no local paths in build output), toolchain fixed
   by `go.mod`, dependencies locked by `go.sum`, and the embedded
   timestamp is the *commit date* — source-derived, never wall
   clock. `go version -m` shows the exact `-ldflags`, including
   version, full commit SHA, and date.
4. **Checksums.** `checksums.txt` (SHA-256) covers every released
   file: the five archives and the five SBOMs. Verify first, trust
   nothing before this step passes.
5. **Build provenance attestation.** One SLSA provenance
   attestation over `dist/checksums.txt`, minted in the Release
   workflow via OIDC (Sigstore) and stored on the GitHub
   attestations API. Because every released file is hashed in
   `checksums.txt`, that single attestation binds all of them —
   separate per-file attestations would add steps without adding
   coverage. Verify with the `gh` CLI (example below).
6. **SBOMs.** One SPDX JSON SBOM per archive, generated at build
   time and uploaded with the release, so consumers can audit the
   (single-dependency) supply chain of the exact bytes they run.

## Verifying a release

Substitute the real owner, repository, version, and asset names.
No credentials are involved — verification is fully public.

```sh
# 1. Fetch the release assets (archives, SBOMs, checksums.txt).
gh release download v0.1.0 --repo <owner>/<repo> --dir ./repoforge-0.1.0

cd ./repoforge-0.1.0

# 2. Verify file integrity against the published checksums.
sha256sum -c checksums.txt

# 3. Verify build provenance: the archive was built from the
# tagged source by this repository's Release workflow.
gh attestation verify repoforge_0.1.0_Linux_amd64.tar.gz \
  --repo <owner>/<repo>

# 4. Confirm the binary reports its own version.
tar -xzf repoforge_0.1.0_Linux_amd64.tar.gz repoforge
./repoforge version
```

`gh attestation verify` checks the Sigstore signature, the SLSA
predicate (repository, workflow, and commit it names), and that the
subject digest matches the file on disk. It fails closed: any
mismatch is an error, not a warning.

## Plan and platform boundaries

Artifact attestations work in public repositories on all current
plans. They are **not** available for private or internal
repositories except on GitHub Enterprise Cloud, and not on GitHub
Enterprise Server at all. On a repository where attestations are
unavailable, the Release workflow's attest step fails loudly — the
release does not silently ship unattested. Checksums and SBOMs are
plan-independent and always published.

## Deliberately not done

- **GPG-signed artifacts.** Sigstore OIDC attestations replace
  long-lived maintainer keys: nothing to generate, store, rotate,
  or leak. Revisit only if consumers require it.
- **Bit-for-bit reproducible builds.** Inputs are constrained and
  recorded (see above), but byte-identical reproduction across
  environments is not claimed or tested. Provenance attestation —
  *who built what from which commit* — is the guarantee offered
  instead.
- **SLSA generator / external builders.** The build runs in the
  repository's own Release workflow so the provenance subject is
  first-party; no third-party builder stands between source and
  artifact.
