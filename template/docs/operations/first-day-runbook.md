# First-Day Runbook

Bring this repository under the standard, and recover it if it drifts.

## Day one

1. Create the repository from the template.
2. Adopt: resolve every placeholder with real values
   (`repoforge template adopt --dry-run` first).
3. `repoforge validate` must be clean.
4. `repoforge bootstrap --dry-run`, read the plan, then
   `repoforge bootstrap`.
5. `repoforge doctor` — no `FAIL`.
6. Seed the wiki: `repoforge wiki sync` then `--apply`.
7. Open the first issue and work issue-first.

## Recovering drift

| Situation | Action |
| --- | --- |
| `validate` reports a violation | Fix the field it names; do not disable the check. |
| `bootstrap` reports a conflict | An unmanaged ruleset needs a human decision. Nothing was changed. |
| `bootstrap` reports unsupported | A plan-limited feature. Lower the setting explicitly or change plan. |
| `doctor` reports `FAIL` | Drift. `repoforge bootstrap` converges it. |
| A partial `bootstrap` | Re-run it; the plan is recomputed from actual state. |
| The wiki diverged | Fix `docs/wiki/<page>.md` and re-run sync. |

## What never happens

- Bootstrap never changes repository visibility.
- Bootstrap never deletes a ruleset, label, or file it does not own.
- `doctor` and `validate` never mutate anything.
- `wiki sync` never deletes a page it does not declare.
