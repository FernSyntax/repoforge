# v0.2.0 baseline audit

Point-in-time planning record for issue #28. Not normative: where this
document disagrees with [spec.md](../spec.md), spec.md wins.

- Recorded at: `main` 936a258 (2026-10-12), isolated worktree
  `/tmp/opencode/repoforge-v020`, branch `docs/28-v020-baseline-audit`.
- Published template listed read-only via `gh api`; never cloned here.

## Excluded active work (untouched)

- Branch `test/23-statement-coverage`, PR #27 (closes #23, statement
  coverage to 85%), including untracked
  `internal/github/path_edges_test.go` and
  `internal/validate/edges_test.go`.
- Issue #26 (`docs/README.md` index, no branch yet).
- Verified before and after every step in this issue: original tree
  stays on `test/23-statement-coverage` with both untracked files
  present; no `reset`, no `clean`, no commit, no push.

## Baseline verification (isolated worktree, `main` HEAD)

| Command | Result |
| --- | --- |
| `gofmt -l .` | clean |
| `go vet ./...` | clean |
| `go test ./...` | green, all packages |
| `git diff --check` | clean |
| `go run ./cmd/repoforge validate --path .` | OK |

No pre-existing failures. Coverage work (#23, 85% on its branch) is
not yet merged; `main` coverage was not re-measured here.

## What currently works

- Deterministic Go CLI (`go 1.27.2`, single dependency `yaml.v3`):
  `validate`, `doctor` (read-only GET), `bootstrap` (plan/apply,
  idempotent, dry-run makes zero mutations), `template`, `wiki`,
  `git`, `pr`, `issue`, `release`, `hook`, `version`.
- Declarative policy: versioned schema pair
  `schemas/repoforge.schema.yaml` / `.json` with parity check
  (`scripts/check-schema-parity`).
- One-way generation: canonical `template/` source builds the
  `FernSyntax/repoforge-template` baseline; published output carries
  `repoforge.manifest.json` provenance (confirmed via read-only API
  listing: no engine source, manifest present).
- Governance gates: branch naming (`B3`), Conventional Commits, PR
  traceability, release validation, blocking hooks; 15 ADRs
  (`docs/decisions/0001`-`0015`); spec §§20-22 requirement
  traceability.

## What is absent (relative to the v0.2.0 platform vision)

No Python, no LangGraph, no FreeLLMAPI client, no provider
abstraction, no model registry, no RAG/retrieval, no MCP server, no
Astro/TypeScript template. `template/` is a governance baseline
(`AGENTS.md`, `spec.md`, `SECURITY.md`, …), not a marketing site.
There is no `docs/development/roadmap.md` despite AGENTS.md listing it.

## What conflicts with the v0.2.0 vision (spec-first blockers)

The vision (AI orchestration, SaaS-like inference gateway, generated
product code) contradicts normative spec v0.1.0: §4 non-goals (no
hosting/SaaS, language-agnostic, no management of repository
*content* beyond owned baseline files), P10 (useful to humans without
AI), P11 (small dependency surface; every new dependency needs ADR
justification). Per DESIGN.md these need spec amendments plus ADRs
*before* implementation — they are the first work items, not code.

## Proposed implementation sequence

1. #28 (this issue): baseline audit — done when this document and the
   verification table are reviewed.
2. Spec amendment scoping v0.2.0 (new issue): which platform
   capabilities enter scope, which stay non-goals; requirement IDs.
3. ADRs (new issues, one per decision): Python/LangGraph boundary,
   FreeLLMAPI provider abstraction, MCP tool surface, RAG
   local-first design, template source-of-truth if Astro is adopted.
4. Deterministic-engine hardening per §§11/16 before any agent layer.
5. Provider abstraction with mocked tests (no live calls in CI).
6. MCP tools, then orchestration with persistent checkpoints, then
   RAG, then GitHub workflow integration, then acceptance tests.
7. Release readiness per spec §14; release only on explicit
   authorization.

## Deferred / blocked

- Live FreeLLMAPI verification: blocked until a gateway endpoint is
  configured; mocks only until then. No paid fallback, ever, without
  explicit opt-in.
- Published-template drift check: `repoforge-template` was listed,
  not cloned; byte-identity of a fresh build is proven in template CI
  (G-4), not re-proven here.
