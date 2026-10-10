<!--
One coherent purpose per PR (docs/development/pull-requests.md).
The title must follow commit conventions, <type>(<scope>): <subject> —
it becomes the squash-merge commit subject on main.

Fill every section; "none" with a reason beats an empty section.
A CI check rejects a PR whose body is missing a section, missing an
issue link, or left a section empty (spec.md §20.2).

Two exceptions are recognised, and only two: write
`dependency-pull-request` (GitHub-generated dependency update) or
`urgent-security-fix` in the body. Both are visible in the diff.
-->

## What changed

## Why

<!-- Link the issue: Closes #123 -->

## Specification

<!-- Requirement IDs from spec.md affected by this change, or "none"
     with the reason. Example: FR-011, NFR-001. -->

## Tests

<!-- Tests added or changed, and what each proves. -->

## Validation

<!-- Exact commands run and their results. Required:
     gofmt -l . · go vet ./... · go test ./... · git diff --check -->

## Risk

<!-- What breaks if this is wrong; blast radius. Include security
     impact, or state "none". -->

## Documentation and ADRs

<!-- docs/ and spec.md changes; ADRs added or superseded (NNNN). -->

## Operational impact

<!-- Effects on CI, release, or day-2 operations; or "none". -->

## Breaking / migration impact

<!-- "None", or: declare the break — commits carry `!` and a
     BREAKING CHANGE: footer, and CHANGELOG.md records it. -->

## Known limitations

<!-- What this change does not do, and why. "None" is a valid answer. -->
