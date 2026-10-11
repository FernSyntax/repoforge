# Roles and responsibilities

Who does what in this project, what each role is required to do, and
who currently holds which role. The role names are stable; the people
holding them change, so the roster lives in the repository's
collaborator permissions (Settings → Collaborators) rather than in this
file — this document defines the contract, GitHub records the holders.

## Maintainer

Owns the project's direction and its release record.

- Declares the policy in `repoforge.yaml`; runs `bootstrap`, `doctor`,
  and `validate` and acts on their findings.
- Reviews and merges pull requests; every merge is a squash with a
  Conventional Commit subject.
- Cuts releases: runs `scripts/release-check.sh`, tags, and publishes.
- Answers security reports; owns the disclosure timeline in
  `SECURITY.md`.
- Keeps this document, the roadmap, and the threat model current.

A maintainer holds write access to the engine repository and is
expected to act on the repository at least monthly.

## Reviewer

A collaborator with write access who reviews changes before merge.

- Reviews pull requests against the standard: scope, tests, traceability,
  and the threat model where security-relevant.
- Approves only what they have actually read; the ruleset dismisses a
  stale approval when new commits land, so an approval always refers to
  the merged content.
- Merges after approval when CI is green; never self-approves.

The engine repository requires one non-author approval on every merge,
so every change that reaches `main` passes through a reviewer who is
not its author. The reviewer roster is the set of collaborators with
write access beyond the author of a given change.

## Contributor

Anyone who proposes a change through an issue or pull request.

- Works issue-first: an issue with the right form, then a branch named
  after it (`<type>/<issue>-<description>`).
- Follows the coding standards and the test policy in `CONTRIBUTING.md`.
- Keeps each pull request to one coherent purpose.

Contributors need no special permission; write access is granted only
through the reviewed path in `SECURITY.md`.

## Vulnerability reporter

Anyone who reports a suspected vulnerability through the private
channel in `SECURITY.md`.

- Reports privately; never through a public issue.
- Receives acknowledgment, coordinated disclosure, and credit, as
  documented.

## Continuity

The project must keep working if any one person becomes unavailable.
The design that guarantees it:

- Every action a role requires is available to any collaborator with
  write access: reviewing, merging, releasing, and publishing are not
  tied to one account. The `main` ruleset requires one non-author
  approval, so with two or more collaborators any single absence leaves
  the repository operable.
- The release and publication credentials live in GitHub Actions
  secrets, not on anyone's machine, so a lost laptop does not cost the
  project its publishing path.
- The policy, the rulesets, and the template all live in git under an
  OSI license; if every maintainer were unavailable at once, the
  history and release record are public and any fork could carry the
  work forward.

A maintainer absence of more than a week is confirmed through any
channel; from that point the remaining collaborators proceed without
waiting.
