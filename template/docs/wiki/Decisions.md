# Decisions

An Architecture Decision Record (ADR) captures one significant choice,
the forces behind it, and what it costs. The index of accepted records
lives in
[`docs/decisions/`](https://github.com/<<OWNER>>/<<REPOSITORY>>/blob/main/docs/decisions/);
this page is the short summary.

## How to add a decision

1. Open an issue describing the decision that needs making.
2. Copy `docs/decisions/0000-template.md` to
   `docs/decisions/NNNN-kebab-case-title.md`, where NNNN is the next
   number.
3. Set the status to `Proposed` and open a pull request.
4. After the pull request merges, set the status to `Accepted` and add a
   row to the index in `docs/decisions/README.md`.

Never edit an accepted record's decision. If the decision changes, write
a new record that supersedes it and say so in both.

## Rules

- Every ADR records the alternatives that were genuinely considered, and
  why they lost. "We picked the first option" is not a decision record.
- An ADR states its consequences, including the ones that hurt.
- A decision that only affects one implementation detail does not need
  an ADR; a decision that constrains future work does.

## What is not an ADR

- Bug fixes, refactors, and dependency bumps. Those are ordinary issues.
- Plans. An ADR records a decision that was made, not one being
  considered.
