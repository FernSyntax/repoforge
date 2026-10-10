# Overview

RepoForge answers one question: how should a serious software
repository be initialized, governed, validated, maintained, and
released? The answer has three parts that reinforce each other:
a declarative standard, a CLI that enforces it, and a generated
template that starts every project inside it.

## The control loop

```
policy ──► bootstrap ──► GitHub state ──► doctor ──► drift report ──► bootstrap …
              │                                             ▲
              ▼                                             │
         verify (re-read)                          validate (local, CI)
```

- **Policy** (`repoforge.yaml`, schema v1) declares desired
  state: repository settings, merge methods, features, security
  posture, release strategy, path ownership.
- **Bootstrap** plans the diff, shows it, confirms explicitly,
  applies, re-reads, and verifies convergence. Running it twice
  converges to the same state; the second run reports zero changes.
  Anything it does not own it reports and leaves alone.
- **Doctor** audits local and remote state read-only. Findings carry
  severities (`PASS`/`WARN`/`FAIL`/`UNSUPPORTED`) and exit codes;
  `UNSUPPORTED` (plan gaps) never fails.
- **Validate** checks the local checkout deterministically: policy
  validity, required files, ownership, commits, branches, GitHub
  YAML, secrets, links. CI runs it; CI is authoritative.

## Components

| Component | Role | Invariant |
| --------- | ---- | --------- |
| `internal/config` | Policy + governance loading, strict decoding | Unknown fields rejected |
| `internal/github` | REST client boundary (reads + mutations) | Token never stored/printed; mutations never retried, only via bootstrap |
| `internal/bootstrap` | Plan → confirm → apply → verify | Idempotent; dry-run; owned-only |
| `internal/doctor` | Local + remote audit | Strictly read-only (GET-only, tested) |
| `internal/validate` | Local deterministic checks | Same input → same findings |
| `internal/gitrules` | Commit/branch conventions, single rule source | Consumed by validate, CLI, PR checks |
| `internal/template` | Template build + validation | Deterministic, leak-free output |
| `configs/` | Embedded governance data | Profiles cross-checked against payloads by tests |
| `schemas/` | Contract + governance schemas | Canonical YAML, derived JSON, identical |

## Trust summary

- Credentials: `GH_TOKEN` → `GITHUB_TOKEN` → `gh` CLI; held
  unexported, redacted from errors, never written anywhere.
- Automation: least-privilege tokens, pinned third-party code,
  no `pull_request_target`, credential-less checkouts.
- Supply chain: one third-party Go dependency, hash-verified
  modules, govulncheck + dependency review + Dependabot, Scorecard
  observations, signed release provenance.
- Releases: annotated SemVer tags, checksums, SBOMs, Sigstore
  attestations verified with `gh attestation verify`.

Normative contract: [spec.md](../../spec.md). Security detail:
[security.md](security.md). Threats and boundaries:
[security.md](security.md) and `docs/reference/github-support-matrix.md`.
