# Decisions

Architecture Decision Records (ADRs). Every significant design decision
gets a numbered record here, and a record is written before the change
it describes is merged.

## Files

ADRs are numbered `NNNN-kebab-case-title.md`. Use
[`0000-template.md`](0000-template.md) as the starting point and take the
next free number.

| ADR | Title | Status |
| --- | --- | --- |
| 0000 | [Decision template](0000-template.md) | Template — not a decision |

## Status values

| Status | Meaning |
| --- | --- |
| Proposed | Open for review; not yet binding. |
| Accepted | Merged and binding. |
| Superseded by NNNN | Replaced by a later record; do not follow it. |
| Deprecated | Still accurate but no longer relevant. |

## Rules

- Records are append-only. A wrong decision is corrected by a new record
  that supersedes it, never by editing the original.
- Every record lists the alternatives that were genuinely considered and
  why they lost.
- Every record states its consequences, including the ones that hurt.
- Every record links the issue, the pull request, the `spec.md` section
  it implements, and the tests that prove it.
- An implementation detail does not need a record. A choice that
  constrains future work does.

This page is the index. It is the only place a record's status is
summarised; the record itself remains the authoritative text.
