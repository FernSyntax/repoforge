# ADR-0008: Template publication with a dedicated GitHub App

- Status: accepted
- Date: 2026-10-10
- Spec: spec.md §13.8, §18.4
- Implementation: `.github/workflows/template-publication.yml`,
  `.github/workflows/template-preflight.yml`, `docs/operations/`

## Context

The engine repository must write to the template repository. The obvious
answer — a personal access token in a secret — gives a long-lived,
over-scoped credential to a workflow, and that credential would be
reachable by anything that can run in the repository.

There is also a bootstrap problem: the first template publication has
to land in an empty repository, which cannot receive a pull request
against a `main` that does not exist yet.

## Decision

A dedicated GitHub App, installed on the template repository alone, is
the publishing principal. The engine repository stores its App ID and
private key as secrets, and an installation access token is minted only
inside the trusted publication job.

Requested permissions, each with the operation that requires it:

| Permission | Level | Why |
| --- | --- | --- |
| Repository metadata | read | Identify the target and confirm its identity. |
| Contents | write | Generated commits, the version tag, and the release. |
| Workflows | write | Generated content includes `.github/workflows/`; `Contents: write` alone is not sufficient to commit workflow files. |
| Pull requests | write | Create and update generated update PRs. |

No administration permission is granted. Repository configuration that
requires administration is performed once by the owner through a
documented, auditable procedure, and the publishing bot's scope is never
escalated to make the tool's life easier.

The token-minting action is pinned to a full commit SHA. Pull-request
workflows never receive the private key or an installation token, and
`pull_request_target` is never used to run untrusted code with a
privileged credential. The publication job verifies the target
owner/repository against the expected destination and fails closed on
mismatch, an unverifiable target, or the wrong installation scope.

**Initial publication** is a distinct path: verify the target is the
intended empty repository with no unrelated history, build and validate
from the tagged engine source, publish one root commit on `main`, let
the template's hosted CI run, and only then publish the template
release. No second initialization commit is permitted.

**Routine updates** open or update a generated PR from a
`repoforge-sync/<version>` branch, wait for required checks, and publish
the template release only after merge. Re-running a publication for an
already-applied source version reports that the target is synchronized
and creates nothing.

## Alternatives considered

| Alternative | Why it was rejected |
| --- | --- |
| A fine-grained PAT in a secret | Long-lived, over-scoped, and reachable by any workflow that runs code. |
| A classic PAT with `repo` scope | Grants every repository, not just the target. |
| An App installed on every repository the owner has | Defeats the point of least privilege. |
| Grant administration so the tool can configure everything | The tool does not need it, and an administrator bot is a much larger blast radius. |
| Force-push the template on each release | Destroys history and bypasses review. |

## Consequences

- Setup is a one-time owner task with a documented runbook: register the
  app, set permissions, install on the one repository, create the two
  secrets, verify the names exist, rotate or revoke.
- Publication cannot run until those secrets exist, and the preflight
  workflow says exactly which one is missing.
- The template repository is administered by its owner; the bot only
  writes generated content.

## Security, maintainability, and operational implications

- The blast radius of a compromised key is one repository and the
  content path, not the account.
- Token minting happens inside the trusted job only, on the expected
  repository and immutable tag, after required checks.
- A missing wiki or an unavailable plan feature is reported, not worked
  around with broader permissions.
- Rotation is: mint a new key, update the secret, revoke the old one.
  Nothing is stored locally.

## Links

- Workflows: `.github/workflows/template-publication.yml`,
  `.github/workflows/template-preflight.yml`
- Runbook: `docs/operations/template-publication.md`
- Tests: `internal/wiki/wiki_test.go`, `internal/template/manifest_test.go`
