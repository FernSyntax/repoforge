# DESIGN.md

Front door to how this project is designed, and the only legitimate
ways to change the design. This file orients; it defines nothing.
Normative behavior lives in [spec.md](spec.md); reasoning lives in
[docs/decisions/](docs/decisions/). If this file disagrees with
either, this file is the bug.

## Where design lives

| Question | Answer lives in |
| --- | --- |
| What should the project do? | [spec.md](spec.md) |
| Why is it this way? | [docs/decisions/](docs/decisions/) |
| How do we work (branches, commits, PRs, releases)? | [docs/development/](docs/development/) (start with [first-day.md](docs/development/first-day.md)) |
| How do we operate it? | [docs/operations/](docs/operations/), [docs/security/](docs/security/) |

Start with the row that matches your question. Do not infer design
from code: code implements decisions, and only the ADR carries the
rejected alternatives.

## How design changes

Two gates, in order, no exceptions:

1. **Spec-first.** A behavior change needs specification text in
   spec.md, proposed and reviewed like code.
2. **ADR-first.** A major decision (rejected alternatives,
   cross-cutting impact, new invariants) needs an ADR in
   `docs/decisions/` **before** implementation: take the next
   number after the index's latest, mark status, link the spec
   sections, record what you rejected and why.

Small fixes need neither: a typo, a broken link, a stale comment —
change it, verify it (`sh scripts/verify`), ship it. When in doubt,
write the ADR.

## What does not go here

New requirements, new invariants, new contracts — those go in
spec.md and the docs above, linked from here only if they reorient
the map. This file stays one page.
