# ADR-0013: The agentic framework — AGENTS.md, DESIGN.md, and validated agent files

- Status: accepted
- Date: 2026-10-11
- Spec: spec.md §13 (documentation), §20 (traceability)
- Implementation: `AGENTS.md`, `DESIGN.md`, `template/AGENTS.md`, `internal/validate`, `internal/template`

## Context

Agents improvise process when the process is hard to find: the root
`AGENTS.md` states rules but teaches no workflows, no `DESIGN.md`
exists to orient design work, and `template/AGENTS.md` has drifted
from the root file — so generated repositories inherit the weaker
variant. Each improvisation (wrong branch, body from memory, skipped
gate) costs a red CI run or a review round-trip.

## Decision

1. Root `AGENTS.md` becomes the agent operating contract: truth
   hierarchy (spec.md wins), the exact verification commands, the
   mandatory contribution workflows with copy-pasteable commands
   (scaffold → fill → ready/create), and the prohibitions. It
   teaches; it does not duplicate — normative detail stays in
   spec.md, ADRs, and workflow docs, reached by link.
2. Root `DESIGN.md` becomes the thin front door to design
   authority: what the system is in one page, where each design
   decision lives, and the only legitimate ways to change the
   design (spec-first, ADR-first). Per ADR-0010 it duplicates no
   normative text; every section links to its source.
3. `template/AGENTS.md` is brought to structural parity with the
   root file, adapted to generated repositories (`sh
   scripts/verify`, no engine CLI assumed, template PR/issue flow).
   Generated repos inherit it through the existing template
   manifest — no new mechanism.
4. `repoforge validate` covers the framework so drift fails locally
   and in CI: the root agent files exist and cross-reference their
   sources (AGENTS.md names spec.md and DESIGN.md; DESIGN.md names
   the architecture index and the decisions index), and the
   template agent file keeps its required sections (proven by
   template tests against the generated output). Scoped like the
   decision check: only repositories carrying the decision index
   are held to the framework — minimal managed repositories are
   not penalized for lacking it.
5. No platform-specific skill directories. The contract is
   platform-neutral and structured so any skill generator can
   consume it; guessing the consumer's stack (`.claude/skills`,
   `.cursor`, …) commits the repository to layouts it cannot
   validate.

## Alternatives considered

- One giant AGENTS.md absorbing design content. Rejected: design
  authority belongs to spec.md/ADRs; a second design narrative
  rots, and ADR-0010 forbids the duplication.
- Per-platform skill trees. Rejected above: unvalidatable layout
  guesses. If a platform convention becomes standard, it arrives
  as generated output from this contract, not as hand-kept files.
- Validating prose style or completeness of agent docs. Rejected:
  validators check structure (presence, links), never prose
  quality — style gates produce adversarial compliance.

## Consequences

- An agent reading two files can work the repository correctly;
  generated repositories inherit the same framework.
- Link checks make doc drift a local failure with a file and line,
  not a review observation.
- AGENTS.md grows but stays bounded: new rules arrive as links to
  normative sources, not as pasted paragraphs.
