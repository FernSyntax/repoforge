# Security Policy

## Supported versions

RepoForge is pre-1.0. Only the latest release receives security
fixes.

## Reporting a vulnerability

**Report vulnerabilities privately. Never open a public issue.**

Use GitHub's private vulnerability reporting: this repository's
**Security tab → Report a vulnerability**. This creates a private
advisory visible only to the maintainers and you. GitHub documents the
mechanism at
[Privately reporting a security vulnerability](https://docs.github.com/en/code-security/security-advisories/guidance-on-reporting-and-writing-information-about-vulnerabilities/privately-reporting-a-security-vulnerability).

Please include:

- A description of the vulnerability and its impact.
- Steps to reproduce or a proof of concept.
- Affected versions, if known.
- Whether you are aware of active exploitation.

## What to expect

- **Acknowledgment** of your report as promptly as the maintainers can
  manage; this project has no paid staff and no SLA.
- **Coordinated disclosure**: we investigate, develop a fix in private,
  and agree with you on a public disclosure date. We ask that you do
  not disclose before the fix is released.
- **Credit** in the release notes and the published advisory, unless
  you prefer to remain anonymous.
- During embargo, commits and PRs describe the change generically;
  detail is published only after the fix ships.

There is currently no bounty program.

## Scope

RepoForge handles GitHub credentials and mutates repository state.
Treated as critical:

- Any path that writes a token to disk, logs, error output, templates,
  or fixtures.
- Any mutation performed by a command documented as read-only
  (e.g. `doctor`) or under `--dry-run`.
- Silent mutation of state the policy does not own.
- Template output that embeds credentials or third-party identities.

Out of scope: vulnerabilities in GitHub itself, in dependencies (report
upstream), or requiring physical access to your machine.

## Credential handling (for users)

- RepoForge reads credentials from `GH_TOKEN`, `GITHUB_TOKEN`, or
  the authenticated `gh` CLI session, in that order.
- It never stores credentials. If you believe a token was exposed
  through this tool: **revoke it immediately** (GitHub Settings →
  Applications / Tokens), then report the issue privately as above.
