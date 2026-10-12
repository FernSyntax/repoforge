# v0.2.0 release readiness

Recorded 2026-10-12, updated after the #47/#49/#50 mergers. This
document is the release-readiness assessment; it is not an
authorization to tag, publish, or release.

## What `main` actually contains

Merged and verified: project model (`internal/project` + schema pair,
FR-031), inference provider (`internal/provider`, FR-032–FR-034,
FR-038), MCP stdio server with four tools (`internal/mcp`, FR-035),
Go staged orchestration with checkpoints (`internal/orchestrate`,
FR-036), local lexical retrieval (`internal/retrieve`, FR-037),
Astro product template with deterministic builder
(`template.BuildProduct`, FR-039), and the baseline-audit record.
ADRs 0016–0019 are accepted; 0020 remains proposed.

## What the release adds if the open PRs merge

| PR | Contents | State |
| --- | --- | --- |
| #47 | World-class template: SEO/AEO/GEO foundation, normalizers, JSON-LD, `llms.txt` | merged |
| #49 | LangGraph runtime (`orchestration/`), `product_build` MCP tool, Python CI, ADR-0021 | merged |
| #50 | Offline acceptance test for the composed slice | merged |
| #53 | ADR statuses, spec requirement rows, v0.2.0 changelog | open, CI green |


Merged 2026-10-12 in that order, as recommended. #53 completes the
conformance the merges required and is open for review.

## Acceptance criteria evidence

### Satisfied, with evidence in the suite

- **Generation engine:** builds, validates, output paths constrained,
  no silent overwrite, deterministic rebuild, provenance manifest —
  `internal/template`, `internal/project`, plus the `product_test.go`
  refinements.
- **Template:** `astro build` operator-verified (`npm ci`,
  `tsc --noEmit`, static output with two pages, valid JSON-LD, one
  h1, zero placeholders). Browser-level accessibility tests are not
  in CI.
- **FreeLLMAPI:** contract inspected from the deployed docs; routing
  centralized; free-only default with no paid fallback; bounded
  classified retry; pause-on-exhaustion; credentials from env,
  redacted. Mock-gateway coverage exists. **No live-gateway test** —
  no operator-configured gateway exists in this environment.
- **Orchestration:** explicit stages, approval gate, bounded repair,
  file checkpoints, resume without repeating external actions.
  #49 adds the interrupt-based LangGraph journey and SQLite threads.
- **RAG:** provenance, hash freshness, exclusions, inertness.
- **MCP and GitHub:** protocol-level tests, containment boundaries,
  least-privilege CI, issue-linked branches and PRs, signed commits,
  no self-merge, no publication.
- **Quality:** all local checks green; both CI jobs green.

### Not satisfied — do not create a release tag before these land

1. #53 (ADR statuses, spec rows, changelog) is merged-ward but not yet
   merged. Until it lands, `main` still reads the shipped slices as
   `planned` and carries no v0.2.0 changelog section.
2. Live FreeLLMAPI verification is blocked: no gateway is configured
   here. Everything inference-related is mock- or operator-verified.
   This is a permanent gap for this release unless an operator
   configures a gateway and runs the live path.
3. #26 (documentation index at `docs/README.md`) is open and belongs to
   another workstream; it is listed here, not taken over. `docs/` grew
   new surfaces (orchestration runtime, decisions, reference) with no
   root index.

## External side effects performed

None. No tag created, no release published, no template repository
write, no merge of any pull request, no force-push, no history
rewrite.

## Verdict

**BLOCKED on one review.** The slices the mandate asked for are merged,
tested, and now traceable: spec rows point at implementations and
tests, ADR-0020 and ADR-0021 are accepted, the v0.2.0 changelog
section exists, and both tree-level preconditions of the release
check pass (a build stamped `v0.2.0` reports `v0.2.0`; the changelog
section matches). Remaining before the release candidate is complete:
merge #53, and confirm #26 is closed by its own workstream. Then run
`repoforge release ready --tag v0.2.0` and create the annotated tag on
`main` — tagging and publishing remain authorized human actions.
This document is superseded by each merge, not edited in place forever:
read the current checklist, then act.
