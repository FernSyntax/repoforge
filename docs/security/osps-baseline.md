# OpenSSF Baseline Conformance Statement

This document records how RepoForge satisfies each OpenSSF Baseline
Level 3 control, with the evidence a verifier can check. It is the
reference behind the project's self-certified badge entry at
[bestpractices.dev](https://www.bestpractices.dev/). The security
model it implements is [spec.md](../../spec.md) §8 and
[security.md](../architecture/security.md).

Criteria version: v2026.08.28 (Baseline Level 3).

## Build and release controls

| Control | Status | Evidence |
| --- | --- | --- |
| OSPS-AC-04.02 — least-privilege CI/CD | Met | Every workflow sets top-level `permissions: {}` and each job declares exactly what it needs. The permission baseline is machine-checked by `TestWorkflowPermissionsBaseline` in `internal/validate/security_test.go`, which allowlists every write grant with a justification. |
| OSPS-BR-01.04 — collaborator inputs sanitized | Met | Manual workflow inputs (the publication tag) are validated against a SemVer shape before use and passed to scripts through the environment, never interpolated into shell. The composite action's inputs are validated against an allowlist before any git command runs (`TestCompositeActionValidatesItsInputs`). |
| OSPS-BR-02.02 — release identifiers | Met | Releases are annotated SemVer tags on `main` (spec.md §14.1–14.2); `scripts/release-check.sh` enforces tag shape, annotation, and changelog presence, and `.goreleaser.yaml` names every archive with the version, OS, and architecture. |
| OSPS-BR-07.02 — secrets and credentials policy | Met | See "Secrets and credentials" in [SECURITY.md](../../SECURITY.md): the two repository secrets (the App ID and its private key), how they are stored (GitHub Actions secrets, never in the repository), how they are used (minted only in the publication job), and how they are rotated (`docs/operations/template-publication.md` → "Rotating or revoking"). `validate` rejects token- and key-shaped material, and secret scanning with push protection is part of the standard. |
| OSPS-DO-03.01 — verify release integrity | Met | [docs/security/release-integrity.md](../security/release-integrity.md) documents the commands and expected output for verifying release assets; `scripts/release-check.sh` verifies the stamped version against the tag. |
| OSPS-DO-03.02 — verify release identity | Met | Releases carry GitHub build-provenance attestations (the release job holds `attestations: write` and `id-token: write`), verifiable with `gh attestation verify`; the verification procedure is documented in [docs/security/release-integrity.md](../security/release-integrity.md). |
| OSPS-DO-04.01 — support scope and duration | Met | [SUPPORT.md](../../SUPPORT.md) and the "Supported versions" section of [SECURITY.md](../../SECURITY.md): pre-1.0, only the latest release receives fixes; best-effort maintenance, no SLA. |
| OSPS-DO-05.01 — security-update policy | Met | [SECURITY.md](../../SECURITY.md): only the latest release receives security fixes, vulnerabilities are reported privately, and findings follow coordinated disclosure. |
| OSPS-QA-02.02 — SBOM with releases | Met | `.goreleaser.yaml` generates an SBOM (CycloneDX) for every archive; the release workflow publishes them with the assets. |

## Governance controls

| Control | Status | Evidence |
| --- | --- | --- |
| OSPS-GV-04.01 — review before escalated permissions | Met | [CONTRIBUTING.md](../../CONTRIBUTING.md) → "Collaborator access": collaborators are added only after a reviewed, linked issue, receive the minimum role, and never administration. No collaborator has ever been added without that process. |
| OSPS-QA-04.02 — subproject strictness | Met | The project ships two repositories (engine and generated template). The template is generated one-way from the engine with an immutable provenance manifest, and its CI rebuilds and verifies it byte-for-byte from the recorded source commit (`TestTemplateCIProvenanceGateIsRepositoryScoped`). Its ruleset requires the same review shape as the engine. |
| OSPS-QA-06.02 — how tests are run | Met | [CONTRIBUTING.md](../../CONTRIBUTING.md) → "Verification": the exact commands (`scripts/verify fast|full`), what each checks, and that CI is authoritative. The test strategy is spec.md §22.3. |
| OSPS-QA-06.03 — test policy for major changes | Met | [CONTRIBUTING.md](../../CONTRIBUTING.md) → "Test policy": major changes (behavior, policy, or security-relevant configuration) must add or update automated tests in the same change; a change with no tests must say why in the pull request. |
| OSPS-QA-07.01 — non-author approval before merge | Met | The main ruleset requires one approval from a non-author (team profile: `required_approving_review_count: 1`, code-owner review, stale-review dismissal, last-push approval). Every change reaches `main` through a reviewed pull request. |
| OSPS-SA-03.02 — threat modeling | Met | [security.md](../architecture/security.md) → "Threat model" enumerates assets, actors, and out-of-scope threats; spec.md §18 is the normative model. The model is revisited for new features (ADR 0014 updated the publication boundary from a code-scanning finding). |

## Vulnerability management controls

| Control | Status | Evidence |
| --- | --- | --- |
| OSPS-VM-04.02 — VEX for non-exploitable findings | Not applicable | No release has shipped yet, and no vulnerability currently affects a component the project does not use. When the first release ships, the release notes will state the exploitability status of any dependency findings at that time. |
| OSPS-VM-05.01 — SCA remediation threshold | Met | [CONTRIBUTING.md](../../CONTRIBUTING.md) → "Dependency and scan policy": any high- or critical-severity vulnerability in a dependency blocks merge and release; medium findings get a documented exception with an expiry; low findings are triaged on a monthly cadence. |
| OSPS-VM-05.02 — SCA before release | Met | Dependency review runs on every pull request (`dependency-review.yml`, a required status check) and `govulncheck` scans the module graph in CI; `scripts/release-check.sh` gates every tag on a green validation run. |
| OSPS-VM-05.03 — SCA status check | Met | `dependency-review` is a required status check on the main ruleset; a failing dependency review blocks the merge. |
| OSPS-VM-06.01 — SAST remediation threshold | Met | [CONTRIBUTING.md](../../CONTRIBUTING.md) → "Dependency and scan policy": any error-severity SAST finding blocks merge; warnings are fixed or justified in the pull request within one release; findings that are false positives are dismissed with a recorded justification, never silenced globally. |
| OSPS-VM-06.02 — SAST status check | Met | CodeQL default setup analyzes Go and GitHub Actions on every push and pull request, and the analyses (`Analyze (go)`, `Analyze (actions)`) are required status checks on the main ruleset (spec.md §7.6), so a failing analysis blocks the merge. |
