# v0.2.0 release readiness

Recorded 2026-10-12 from `main` at `eb98828` plus the three open
pull requests. This document is the release-readiness assessment; it
is not an authorization to tag, publish, or release.

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
| #47 | World-class template: SEO/AEO/GEO foundation, normalizers, JSON-LD, `llms.txt` | CI green, awaiting review |
| #49 | LangGraph runtime (`orchestration/`), `product_build` MCP tool, Python CI, ADR-0021 | CI green, awaiting review |
| #50 | Offline acceptance test for the composed slice | CI green, awaiting review |

Merge order: #47 first (its FAQ contract is a prerequisite of #49's
content pass and of that PR's live proof), then #49, then #50. All
three touch disjoint files, so no conflicts are expected.

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

1. The three open PRs are unmerged and unreviewed. A release cut from
   `main` today would omit the LangGraph runtime and the world-class
   template, so it would not be v0.2.0 as specified.
2. ADR-0020 and ADR-0021 are `proposed`; the ADR process requires the
   status flip to `accepted` at merge. Neither flip has landed.
3. The spec rows FR-031…FR-039 and NFR-007/008 are still marked
   `planned` (17 markers in `spec.md`). When the code lands, those
   rows must point at implementations and tests, or the spec will be
   claiming things that are not true.
4. Live FreeLLMAPI verification is blocked: no gateway is configured
   here. Everything inference-related is mock- or operator-verified.
5. #26 (docs index) is open. It matters before release: `docs/` gained
   several new surfaces (orchestration runtime, decisions, reference)
   with no root index.

## External side effects performed

None. No tag created, no release published, no template repository
write, no merge of any pull request, no force-push, no history
rewrite.

## Verdict

**NOT READY.** The platform slices the mandate asked for exist, are
tested, and are individually green — but three are unreviewed and
unmerged, so the release candidate does not yet contain them. Merge
#47, then #49, then #50; flip ADR-0020 and ADR-0021 to accepted; update
the spec rows to point at the shipped implementations and tests; close
#26; then re-run `repoforge release ready --tag v0.2.0`. Tagging and
publishing remain authorized human actions.
