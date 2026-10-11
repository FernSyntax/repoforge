# DESIGN.md

Front door to how RepoForge is designed, and the only legitimate ways
to change the design. This file orients; it defines nothing. Normative
behavior lives in [spec.md](spec.md); reasoning lives in
[docs/decisions/](docs/decisions/). If this file disagrees with either,
this file is the bug.

## What the system is

RepoForge answers one question: how should a serious software
repository be initialized, governed, validated, maintained, and
released? Three parts reinforce each other: a **declarative
standard** (spec.md + `repoforge.yaml` schema), a **CLI that enforces
it** (`repoforge validate/doctor/bootstrap`, plus the contribution
gates), and a **generated template** (`template/`) that starts every
project inside it. The full map — control loop, components,
invariants, trust summary — is
[docs/architecture/overview.md](docs/architecture/overview.md).

Two facts shape every design decision here:

- **The engine and the template are separate repositories with a
  one-way relationship** (engine generates template, never the
  reverse). Why:
  [why-two-repositories.md](docs/architecture/why-two-repositories.md).
- **CI is authoritative; local tooling is feedback**
  (spec.md P9). Enforcement lives server-side; the CLI moves
  failures left. Any design that trusts the client is wrong here.

## Where design lives

| Question | Answer lives in |
| --- | --- |
| What should the system do? | [spec.md](spec.md) |
| Why is it this way? | [docs/decisions/](docs/decisions/) (index: [README.md](docs/decisions/README.md)) |
| How is it structured? | [docs/architecture/](docs/architecture/) (index: [README.md](docs/architecture/README.md)) |
| How do we work (branches, commits, PRs, hooks, releases)? | [docs/development/](docs/development/) ([git-workflow.md](docs/development/git-workflow.md) is the index) |
| How do we operate it (publication, releases, security)? | [docs/operations/](docs/operations/), [docs/security/](docs/security/) |
| Exact command contracts? | [docs/reference/cli.md](docs/reference/cli.md) |

Start with the row that matches your question. Do not infer design
from code: code implements decisions, and only the ADR carries the
rejected alternatives.

## How design changes

Two gates, in order, no exceptions:

1. **Spec-first.** A behavior change needs specification text:
   new or amended requirement IDs in spec.md, proposed and reviewed
   like code. `AGENTS.md` states what to read; the workflow docs
   state how to ship.
2. **ADR-first.** A major decision (anything with rejected
   alternatives, cross-cutting impact, or a new invariant) needs an
   ADR in `docs/decisions/` **before** implementation. Take the next
   number after the index's latest, mark status, link the spec
   sections, record the alternatives you rejected and why. "Implement now, document
   later" is how design authority dies; it is not done here.

Small fixes need neither: a typo, a broken link, a stale comment —
change it, verify it, ship it. When in doubt, write the ADR; a
three-paragraph ADR that turns out to be unnecessary costs less than
an undocumented decision that turns out to matter.

## What does not go here

New components, new invariants, new contracts — those go in spec.md
and the architecture docs, linked from here only if they reorient
the map. This file stays one page: if it grows a second, something
below wants restructuring instead.
