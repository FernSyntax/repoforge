# CLI Reference

`repoforge` — the RepoForge CLI. Commands: `validate`, `doctor`,
`bootstrap`, `template`, `wiki`, `git`, `pr`, `version`.

Exit codes are uniform across every command: `0` success · `1` a
finding or a pending change · `2` an invocation or configuration error,
including a missing prerequisite.

## repoforge validate

```
repoforge validate [--json] [--commits N] [--path DIR]
```

Checks the local repository checkout against the RepoForge standard.
Deterministic and offline (spec.md §12): no network, no clock, no
randomness; identical checkouts produce identical findings.

Discovery: walks up from `--path` (default `.`) to the nearest `.git`.

Checks performed:

| Rule prefix | What it verifies |
| ----------- | ---------------- |
| `config/*` | Policy file presence (`repoforge.yaml`/`.yml`/`.json`), schema-conformant values, unknown fields, cross-field consistency. |
| `files/required` | Baseline files tracked: `README.md`, `LICENSE`, `CHANGELOG.md`. |
| `files/ownership` | Every `ownership.managed_paths` pattern matches tracked files. |
| `branch/name` | Current branch is the default branch or matches `<type>/<issue>-<description>`. |
| `commit/message` | Subjects of the last `--commits` commits (default 20) follow the restricted Conventional Commits subset. |
| `yaml/issue-template` | Issue forms and `.github/ISSUE_TEMPLATE/config.yml` structure. |
| `yaml/workflow` | Workflow syntax, `on`/`jobs` keys, SHA action pinning (per `security.action_pinning`, default `sha`). |
| `yaml/github-config` | Other `.github` YAML/JSON parses. |
| `content/placeholder` | No unresolved template placeholders outside `ownership.placeholder_paths`. |
| `content/secret` | No token/key-looking material (GitHub token formats, private key blocks, AWS key IDs, Slack tokens). |
| `content/abs-path` | No absolute machine paths (`/home/<user>/…`, `/Users/<user>/…`, Windows `…\Users\…` profile paths). |
| `content/branding` | No legacy product name, module path, or repository URL in any spelling. |
| `decisions/*` | Every ADR under `docs/decisions/` is indexed, and every indexed name resolves. |
| `docs/link` | Relative Markdown links resolve inside the repository. |

Exit codes: `0` valid · `1` violations found · `2` invocation or
configuration error (not a git repository, git missing, malformed
policy file, bad flags).

`--json` emits `{"root", "policy", "findings": [...]}` with findings
sorted by path, rule, message.

## repoforge doctor

```
repoforge doctor [--json] [--repo owner/name] [--path DIR]
```

Audits local and remote state against the policy and reports drift.
**Strictly read-only** (spec.md §11.2): every GitHub call is a GET; local
checks reuse the deterministic validator. Credentials resolve per
spec.md §8.1 (GH_TOKEN → GITHUB_TOKEN → `gh`); the repository slug is
parsed from the origin remote unless `--repo` is given.

Local checks: git repository, policy validity, standard version,
required files, ownership declarations, unresolved placeholders.

Remote checks: repository settings (visibility, default branch), merge
methods, feature toggles, topics (inspection), rulesets (the
RepoForge-owned **`repoforge:main`** branch ruleset and
**`repoforge:release-tags`** tag ruleset, each compared field by
field against the desired canonical payload for the selected
governance profile; unmanaged rulesets are warned about), labels (every
canonical label must exist with the documented color and description),
security settings (Dependabot, private vulnerability reporting,
secret scanning, push protection, code scanning, dependency review)
with plan-aware severities.

Every finding carries a severity:

| Severity | Meaning | Affects exit code |
| -------- | ------- | ----------------- |
| `PASS` | State matches policy. | — |
| `WARN` | Attention needed; not drift. | — |
| `FAIL` | Drift from policy. | exits 1 |
| `UNSUPPORTED` | Capability/plan gap (e.g. GHAS on a private repo). **Never a failure.** | — |

Exit codes (spec.md §11.4): `0` no drift (WARN/UNSUPPORTED allowed) ·
`1` drift found · `2` audit could not complete.

`--json` emits `{"repository", "findings": [{"check", "severity",
"message"}]}` — stable machine-readable fields.

## repoforge bootstrap

```
repoforge bootstrap [--dry-run] [--yes] [--json] [--repo owner/name] [--path DIR]
```

Converges GitHub state to the policy (spec.md §10). Cycle: discover →
load desired state → authenticate → read actual state → plan → display
→ confirm → apply → re-read → verify convergence.

**Idempotent** (spec.md §10.2): a second run plans and applies zero
changes.

- `--dry-run` prints the plan and changes nothing (no mutations, no
  confirmation needed).
- Without `--yes`, application requires typing `yes` on an interactive
  terminal; non-interactive stdin declines (fails safe, spec.md P7).
- `--json` emits `{"repository", "dry_run", "aborted", "plan":
  [{"area", "action", "subject", "detail"}], "applied", "converged"}`.

Managed vs. user-owned state:

- Managed: merge/feature settings, default branch, RepoForge-owned
  rulesets identified by the stable names **`repoforge:main`** and
  **`repoforge:release-tags`**, the canonical label set
  (`configs/github/labels.yaml`), Dependabot alerts/security updates,
  private vulnerability reporting, secret scanning, push protection,
  code scanning (public repos).
- User-owned, never touched: non-canonical labels, topics (unless the
  governance profile declares a non-empty set), unmanaged rulesets.
- An unmanaged ruleset is reported (`conflict`); inherited
  organization rulesets are reported (`report`). Neither is modified
  or deleted.
- Visibility is never changed by automation (spec.md §8.5) — drift is
  reported, never applied.
- Plan-dependent security features on private repositories become
  explicit `unsupported` plan items (docs/reference/github-capabilities.md),
  never silent skips and never doomed mutations.

Exit codes: `0` converged / nothing pending · `1` dry-run with pending
changes, aborted, unsupported capabilities remain, or convergence not
verified · `2` error.

## repoforge git commit-check

```
repoforge git commit-check <message>
```

Validates a commit message subject against the restricted Conventional
Commits subset: allowed types (feat, fix, docs, refactor, perf, test,
build, ci, chore, revert, security), optional lowercase scope, optional
`!` breaking marker, required subject, no trailing period, no generic
subjects ("fix stuff", "updates", "changes", "wip", "misc", "temp",
…). Over-length subjects (>72 chars) and non-imperative starts
produce WARN guidance that never fails; GitHub's synthetic PR
test-merge commits are exempt. Exit `0` valid · `1` invalid ·
`2` usage error.

## repoforge git branch-check

```
repoforge git branch-check <branch>
```

Validates a branch name: the default branch, the
`<type>/<issue>-<description>` pattern (branch types exclude
`revert`), or a Dependabot automation branch. Exit `0` valid ·
`1` invalid · `2` usage error.

## repoforge pr check

```
repoforge pr check [--path DIR]
```

Local, GitHub-free PR pre-flight: on a well-named topic branch (not
`main`), at least one commit ahead of the default branch, every new
commit message valid, working tree clean. Exit `0` ready ·
`1` not ready · `2` error.

## repoforge pr scaffold

```
repoforge pr scaffold --issue N
```

Prints the pull request body skeleton for the issue: every section the
traceability contract requires, with `Closes #N` pre-placed in Why.
Fill prompts are HTML comments, which validation strips, so the
skeleton alone does not validate — fill every section, then prove it
with `pr ready`.

## repoforge pr ready

```
repoforge pr ready --file PATH [--path DIR] [--repo owner/name]
```

The single local gate before opening a pull request: branch readiness
(`pr check`) plus the body contract (`pr check-body`). Exit `0` means
every deterministic check CI runs will pass; anything else names
exactly what is red. With `--repo`, and when a credential resolves, it
also confirms the referenced issue exists.

## repoforge issue scaffold

```
repoforge issue scaffold --type bug|feature|task|security [--path DIR]
```

Prints the skeleton for an issue form: one `### <label>` heading per
required field, derived from `.github/ISSUE_TEMPLATE/<type>.yml`.
Fill prompts are HTML comments, so the skeleton alone does not
validate — fill every field, then prove it with `issue check`.

## repoforge issue check

```
repoforge issue check --type bug|feature|task|security --file PATH [--path DIR]
```

Validates a drafted issue body against the form's required fields:
every required `### <label>` present and non-empty, matched
case-insensitively. No CI check constrains issue bodies; this command
is guidance with teeth, not a new contract. Exit `0` ready ·
`1` not ready · `2` usage error.

## repoforge pr create

```
repoforge pr create --title T --file PATH [--path DIR] [--repo owner/name] [--base BRANCH] [--draft]
```

Gates exactly like `pr ready`, then delegates to `gh pr create`. The
network call is structurally after the gate: a red gate never reaches
GitHub. The title must follow commit conventions (it becomes the
squash-merge subject) and is validated locally. Authentication stays
in `gh`; a missing or unauthenticated `gh` fails with its own message.
Creation targets the checkout's repository. Exit `0` created ·
`1` gate red or `gh` failed · `2` usage error.

## repoforge issue create

```
repoforge issue create --type bug|feature|task|security --title T --file PATH [--path DIR] [--repo owner/name]
```

Validates the drafted body against the form, then delegates to
`gh issue create` with the form's declared labels attached, so
automation files under the same labels a web-filed issue would get.
Exit `0` created · `1` gate red or `gh` failed · `2` usage error.

## repoforge mcp serve

```
repoforge mcp serve --allowed-dir DIR [--template-source DIR]
```

Serves the four approved MCP tools over stdio (newline-delimited
JSON-RPC 2.0, protocol `2025-11-25`): `capabilities`,
`project_validate`, `template_build`, and `template_verify`
(spec.md §24, FR-035; ADR-0017). Writable tool paths resolve inside
`--allowed-dir` through symlink evaluation, on top of the engine's
own output policy; `--template-source` defaults to `template/`. Runs
until stdin closes. There is no remote transport. Exit `0` clean
shutdown · `2` usage, configuration, or serve error.

## repoforge astro build

```
repoforge astro build --out DIR [--product-source DIR] [--var key=value ...] [--force]
```

Renders the canonical Astro product template (`product/astro` by
default) into a new repository tree: required variables
(`ProjectName`, `Description`) applied, unresolved placeholders
refused, provenance manifest recorded, destination published
atomically (spec.md §24, FR-039; ADR-0020). Same safety contract as
`template build`: validated variables, resolved destination, and no
silent overwrite — a non-empty destination needs `--force`. Exit `0`
built · `1` validation findings · `2` usage error.

## repoforge release ready

```
repoforge release ready --tag vX.Y.Z [--path DIR]
```

Runs `scripts/release-check.sh` as the pre-tag gate: shape,
annotation, ancestry, changelog, version stamp. Performs no tagging,
pushing, or publishing — the firing action stays human. Exit `0`
ready · non-zero the script's verdict · `2` usage error.

## repoforge hook install|verify|uninstall

```
repoforge hook install|verify|uninstall [--path DIR]
```

The CLI entry point over `scripts/hooks-install`, discovered from the
repository root. Installs nothing itself: the pre-commit framework
still owns the blocking hooks, and hooks the installer does not own
are still never replaced. See `docs/development/git-hooks.md`.

## repoforge version

Prints the build version (stamped at release time, spec.md §14.3).

## repoforge template validate

```
repoforge template validate [--path template/]
```

Validates a **template source** against the template model (spec.md §13):
required files, no engine leakage (`*.go`, `go.mod`), no credentials, no
machine paths, no hardcoded identities, intentional placeholders from the
closed known set only, parseable YAML, pinned actions. Exit `0` clean ·
`1` issues found · `2` usage error.

Default `--path` is `template`, the canonical template source directory.

## repoforge template build

```
repoforge template build --out DIR [--path template/] [--var key=value ...] [--force]
                       [--source-ref TAG] [--source-commit SHA]
                       [--source-url URL] [--repoforge-version VERSION]
```

Renders the template source into a **dedicated output directory**.
Provided variables render; unprovided ones remain as intentional
placeholders. Identical inputs produce byte-identical output
(spec.md §13.5).

**The output directory is validated, not cleaned** (spec.md §5.3, P7):

1. every variable name and value is validated first — a rejected value
   leaves the destination untouched;
2. the destination is resolved through symlinks and refused if it is the
   filesystem root, the home directory, the working directory or any of
   its ancestors (`.`, `./`, `..`, `../x`), the template source tree, or
   any ancestor of it;
3. the rendered tree is validated, then staged in a temporary directory
   beside the destination and published by rename, so a failure during
   rendering or publication leaves the destination as it was.

A **non-empty** pre-existing destination is refused unless `--force` is
given. `--force` never bypasses the unsafe-target rules above; it only
permits replacing a non-empty destination, and it does so by staging and
rename so no reader observes a partial result.

Because of the above, `scripts/build-template` passes `--force`:
`.template-out/` is a regenerable build artifact it declares itself.

Every build writes `repoforge.manifest.json` into the output, recording
the RepoForge version, the immutable source reference and source
commit, and the path, mode, and SHA-256 of every other file
(spec.md §13.6). The manifest digest is computed over the canonical
encoding with the digest field blanked, so it never hashes itself and a
rebuild from the same inputs is byte-identical.

Exit `0` built · `1` refused target, failed validation, or I/O failure ·
`2` usage error.

## repoforge template adopt

```
repoforge template adopt --var key=value ... [--path DIR] [--dry-run]
```

Adopts a **generated repository** in place (default: the current
directory). Every double-brace placeholder is resolved with the given
values, literally: a value containing `$`, a backslash, quotes, or
Unicode is inserted byte-for-byte and never interpreted as a replacement
expression.

**All five known variables are required** — partial adoption fails
instead of leaving silent placeholders; unknown keys and invalid values
fail (closed set). The rendered result is validated against the
generated-repository contract before anything is written, and only
changed files are written. Writes are atomic with rollback: each file is
published by write-to-temp-and-rename, and any failure restores every
file already published.

Refuses directories without `.repoforge/config.yml`, refuses
symlinked targets and symlinks placed at baseline paths, and refuses the
engine checkout itself. `--dry-run` prints every planned substitution
without writing (exit 1 when changes pending); re-running with the same
values is a no-op. Exit `0` adopted or already adopted · `1` validation
failure, pending dry-run, or a failed write (rolled back) · `2` usage
error.

## repoforge template verify

```
repoforge template verify [--path DIR] [--source-url URL] [--require-source]
```

Proves that a built template tree is both internally consistent and
reproducible from the source commit recorded in its own manifest
(spec.md §13.7). It validates the tree against the template model,
checks the manifest against the tree, fetches the recorded commit over
git, rebuilds from that exact source, and compares byte-for-byte
including file modes and the manifest itself.

`--require-source` fails unless the manifest records an immutable source
commit and URL: a published template that cannot be traced to a release
must never be published. Without the flag, a development build is
reported with a note instead.

Exit `0` verified · `1` differences found · `2` the manifest is not
verifiable, or git/network prevented the rebuild.

### Variable values

| Variable | Accepted |
|---|---|
| `ProjectName` | letters, digits, `.`, `_`, `-`; starts alphanumeric; ≤ 100 chars |
| `Owner` | letters, digits, `_`, `-`; starts alphanumeric; ≤ 100 chars |
| `RepoURL` | an `http://` or `https://` URL with a host |
| `Description` | printable, no newlines; 1–200 chars |
| `StandardVersion` | `vN` (for example `v1`) |

No value may be empty, contain control characters or whitespace
(beyond the description's internal spaces), or contain placeholder
syntax `{{`/`}}` — that would let one value expand another and make
output order-dependent.

## repoforge wiki sync

```
repoforge wiki sync [--path DIR] [--source docs/wiki] [--repo owner/name]
                    [--apply] [--token TOKEN]
```

Converges the hosted GitHub wiki to the declared page set in
`--source` (default `docs/wiki/`, resolved inside `--path`, default the
repository root). One Markdown file per page; the file's base name is
the page name, so `Home.md` becomes the page `Home` (spec.md §13.9).

| Flag | Behaviour |
| --- | --- |
| *(default)* | Dry run. Prints the plan; writes nothing and pushes nothing. |
| `--apply` | Writes the declared pages, commits once, and pushes without force. |
| `--repo` | `owner/name`; default parses the origin remote. |
| `--token` | Explicit credential; default `GH_TOKEN` → `GITHUB_TOKEN` → `gh`. |

Guarantees: pages outside the declared set are user-owned — reported,
never modified, never deleted; GitHub's own `_Sidebar.md` and
`_Footer.md` are never touched; the publish path verifies the clone's
origin against the requested repository before any write; pushes are
never forced, so a wiki that advanced stops the sync and the operator
re-plans; a token is passed to git through the environment, never a URL
or a file; a missing or disabled wiki is reported separately from an
authentication failure.

Exit `0` converged or dry run clean · `1` the wiki is missing, the
credential is refused, or the destination does not match · `2` usage
error (bad source directory, bad slug, missing token with `--apply`).

## repoforge pr check-body

```
repoforge pr check-body --file PATH [--repo owner/name]
```

Validates a pull request body against the traceability contract
(spec.md §20.2): a linked issue (`Closes #N`, `Fixes`, or `Resolves`,
optionally qualified as `owner/repo#N`) and every required section —
What changed, Why, Specification, Tests, Validation, Risk, Documentation
and ADRs, Operational impact, Breaking / migration impact, Known
limitations — present and non-empty. Guidance in HTML comments does not
count as content.

With `--repo`, and when a credential resolves, it also confirms the
referenced issue exists, so a typo'd or deleted issue fails rather than
sailing through. A missing credential is reported, not skipped.

Two exceptions are recognised, and only two, declared literally in the
body so they are visible in the diff: `dependency-pull-request` and
`urgent-security-fix` (spec.md §20.3). There is no self-applied bypass
label.

Exit `0` ready · `1` not ready or the linked issue does not exist ·
`2` usage error (missing `--file`, unreadable body).

## repoforge version

Prints the build version (stamped at release time, spec.md §14.3).
