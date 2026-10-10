# Project Specification

<!--
Authoring guide — delete this comment block before sharing.

`spec.md` is the normative description of what this project is and must
do. It is the contract every issue, pull request, test, and ADR points
back to. It is plain Markdown so it reads well in any editor and on
GitHub.

How to use this template:

1. Fill in the metadata block below. Replace every <<...>> token.
2. Answer each section in your own words. Delete the italic
   "How to write this" notes as you go — they are instructions to you,
   not content for the reader.
3. Give every functional requirement a stable ID (FR-001, FR-002, ...)
   and every non-functional requirement an NFR ID. Never renumber an
   existing ID: retire it instead, with a note saying when and why.
4. Anything marked MUST or SHALL is enforced by CI or review. Anything
   marked SHOULD is expected practice. Anything marked MAY is optional.
5. Link each requirement to the tests that prove it in
   §15 Traceability. A requirement with no test and no explicit reason
   is not finished.

Do not invent requirements you do not intend to meet, and do not claim
a requirement is met until a check proves it.

Note: RepoForge adoption already substituted the project name, owner,
repository URL, and description into the files that carry them. Those
values are not repeated here on purpose — this document describes the
project, not its branding.
-->

| Field | Value |
| --- | --- |
| Project | <<PROJECT NAME>> |
| Repository | <<REPOSITORY URL>> |
| Owner | <<OWNER OR TEAM>> |
| Status | Draft / Active / Maintenance / Retired |
| Specification version | 1 |
| Last reviewed | <<YYYY-MM-DD>> |

## 1. Problem and context

*How to write this:* two or three paragraphs a new engineer can read
cold. What is wrong or missing today, who feels it, and why the obvious
alternatives are not enough. Name the existing system this replaces or
extends, if any.

## 2. Users and actors

| Actor | Description | What they need to succeed |
| --- | --- | --- |
| <<ACTOR>> | <<WHO THIS IS>> | <<THEIR GOAL>> |

Primary use cases, in priority order:

1. <<USE CASE 1>>
2. <<USE CASE 2>>
3. <<USE CASE 3>>

## 3. Goals and success criteria

*How to write this:* state outcomes, not activities. Each goal needs a
signal that says it was met.

| ID | Goal | Success criterion |
| --- | --- | --- |
| G-1 | <<GOAL>> | <<MEASURABLE SIGNAL>> |

## 4. Non-goals

*How to write this:* be explicit about what this project deliberately
does not do. This section prevents scope arguments later.

- <<NON-GOAL>>
- <<NON-GOAL>>

## 5. Functional requirements

*How to write this:* one requirement per line, each with a stable ID, a
normative keyword, and an observable acceptance condition. Prefer "the
CLI MUST exit 2 when the configuration file is absent" over "the CLI
handles bad configuration".

| ID | Requirement | Priority |
| --- | --- | --- |
| FR-001 | The system MUST <<OBSERVABLE BEHAVIOUR>>. | MUST |
| FR-002 | The system SHOULD <<OBSERVABLE BEHAVIOUR>>. | SHOULD |

## 6. Non-functional requirements

*How to write this:* quality attributes with a measurable bound. "Fast"
is not a requirement; "p95 latency under 200 ms at 50 rps" is.

| ID | Attribute | Requirement |
| --- | --- | --- |
| NFR-001 | Performance | <<BOUND>> |
| NFR-002 | Availability | <<BOUND>> |
| NFR-003 | Security | <<BOUND>> |
| NFR-004 | Accessibility | <<BOUND, or "not applicable" and why>> |
| NFR-005 | Observability | <<WHAT IS LOGGED OR METRICED>> |

## 7. Architecture and trust boundaries

*How to write this:* name the components and where the trust boundaries
fall. A short ASCII diagram in a fenced block is fine and survives every
editor.

```
<<COMPONENT A>> --> <<COMPONENT B>> --> <<DATA STORE>>
```

Trust boundaries and what crosses them:

- <<BOUNDARY AND WHAT IS ALLOWED ACROSS>>

External dependencies, and what happens when each is unavailable:

| Dependency | Purpose | Failure behaviour |
| --- | --- | --- |
| <<DEPENDENCY>> | <<WHY>> | <<WHAT THE SYSTEM DOES>> |

## 8. Data model and interfaces

*How to write this:* the entities this project owns, their key fields,
and any interface it exposes or consumes. Link to generated
documentation rather than duplicating it.

- Entity: <<NAME>> — <<FIELDS AND MEANING>>
- Public interface: <<CLI COMMAND / HTTP ROUTE / LIBRARY ENTRY POINT>>
- Consumed interface: <<UPSTREAM API AND CONTRACT>>

## 9. Security, privacy, and sensitive data

*How to write this:* the data classification, where secrets come from,
and the abuse cases you have considered. Never put a real credential in
this file.

- Data classification: Public / Internal / Confidential
- Secrets: <<HOW THEY ARE SUPPLIED AND ROTATED>>
- Abuse cases considered: <<THREAT AND MITIGATION>>
- Private vulnerability reporting: see `SECURITY.md`.

## 10. Error handling, reliability, and operations

*How to write this:* the error contract (what a caller sees), the
recovery story, and what an on-call engineer does when it breaks.

- Error contract: <<EXIT CODES / ERROR SHAPE>>
- Retry and idempotence: <<RULES>>
- Rollback: <<HOW A BAD RELEASE IS UNDONE>>
- Runbook: <<LINK TO RUNBOOK>>

## 11. Testing strategy and quality gates

*How to write this:* the layers, what each one proves, and the command
that runs them. Link to the actual verification entry point.

| Layer | Scope | Command |
| --- | --- | --- |
| Unit | <<SCOPE>> | `<<COMMAND>>` |
| Integration | <<SCOPE>> | `<<COMMAND>>` |
| End-to-end | <<SCOPE>> | `<<COMMAND>>` |

Required gates for every pull request: `<<VERIFICATION COMMAND>>`.

## 12. Migrations, deployment, and compatibility

*How to write this:* how state changes, how releases reach users, and
what compatibility promises apply.

- Migration approach: <<HOW>>
- Deployment: <<WHERE AND HOW>>
- Compatibility promise: <<SEMVER POLICY OR EQUIVALENT>>

## 13. Risks, assumptions, and open questions

| ID | Item | Kind | Mitigation or owner |
| --- | --- | --- | --- |
| R-1 | <<RISK>> | Risk / Assumption / Question | <<WHAT HAPPENS NEXT>> |

## 14. Acceptance criteria

*How to write this:* the objective, checkable conditions for calling
this specification met. Each one should be provable by a command, a
review, or a demonstration.

- [ ] <<OBJECTIVE CONDITION>>
- [ ] <<OBJECTIVE CONDITION>>

## 15. Traceability

*How to write this:* every requirement ID maps to the work and evidence
that deliver it. Add rows as the project grows; never leave a
requirement unmapped.

| Requirement | Issue | Pull request | Tests | Documentation | ADR |
| --- | --- | --- | --- | --- | --- |
| FR-001 | <<#N>> | <<#N>> | <<TEST NAMES>> | <<PATH>> | <<NNNN>> |
| NFR-001 | <<#N>> | <<#N>> | <<TEST NAMES>> | <<PATH>> | <<NNNN>> |
