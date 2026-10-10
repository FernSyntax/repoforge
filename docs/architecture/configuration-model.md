# Configuration Model

Field reference for the RepoForge repository contract, schema
version **v1** (`schemas/repoforge.schema.yaml`). Architecture
document; spec.md §9 is normative.

Conventions used below:

- **Req**: whether the field must appear in the policy document.
- **Default**: the value tooling applies when the field is omitted.
  JSON Schema defaults are annotations; RepoForge tooling
  materializes them, so an omitted field behaves exactly as its default
  (spec.md §9.4). "—" means no default: the field must be explicit.
- Any field not listed here is **rejected** (`additionalProperties:
  false` at every level; see repository-contract.md).

## standard

Identifies the standard and schema version. This object is **required**.

| Field     | Type   | Req      | Default         | Meaning |
| --------- | ------ | -------- | --------------- | ------- |
| `name`    | string | optional | `"repoforge"` | Name of the standard the policy conforms to (pattern `^[a-z0-9][a-z0-9-]*$`). Forks of the standard set their own name. |
| `version` | string (const) | **required** | — | Contract schema version. v1 fixes this to `"v1"`. Tooling must reject unknown versions (spec.md §9.2). |

Compatibility: `version` is the compatibility gate (spec.md §16.2). A v2
schema will require a different constant, so version mismatches are
always explicit errors, never silent reinterpretation.

## repository

Core identity and governance. Object optional; each field independent.

| Field                | Type   | Req      | Default     | Meaning |
| -------------------- | ------ | -------- | ----------- | ------- |
| `governance_profile` | enum   | optional | `"solo"`     | Named governance profile: `solo`, `team`, `strict`. Realized by the GitHub governance model (`configs/github/`, see docs/architecture/github-governance.md) layered onto the baseline. |
| `visibility`         | enum   | optional | `"public"`  | Desired visibility: `public`, `private`, `internal`. |
| `default_branch`     | string | optional | `"main"`    | Default branch name (pattern `^[a-zA-Z0-9][a-zA-Z0-9._/-]*$`). |

Compatibility:

- `governance_profile`: profile definitions are part of a schema
  version's semantics; new profiles are additive. Profile semantics and
  plan restrictions: docs/architecture/github-governance.md and
  docs/reference/github-capabilities.md.
- `visibility`: `internal` is valid only for organization-owned
  repositories on GitHub Enterprise. Changing visibility is
  security-sensitive (spec.md §8.5): doctor audits it; bootstrap must never
  change it without explicit confirmation.
- `default_branch`: the standard's own repositories use `main`
  (spec.md §6.1). Renaming a default branch retargets open PRs; bootstrap
  treats renames as plan-only, confirmed operations.

## merge

Merge methods and post-merge behavior. Object optional. Constraint:
**at least one of `squash`, `merge_commit`, `rebase` must be `true`**
(schema `anyOf`).

| Field           | Type    | Req      | Default | Meaning |
| --------------- | ------- | -------- | ------- | ------- |
| `squash`        | boolean | optional | `true`  | Allow squash merges (GitHub: `allow_squash_merge`). The standard's preferred method (pull-requests.md M1). |
| `merge_commit`  | boolean | optional | `false` | Allow merge commits (GitHub: `allow_merge_commit`). Disabled by default (M2). |
| `rebase`        | boolean | optional | `false` | Allow rebase merges (GitHub: `allow_rebase_merge`). Disabled by default (M3). |
| `delete_branch` | boolean | optional | `true`  | Delete head branches after merge (GitHub: `delete_branch_on_merge`, M4). |
| `linear_history` | boolean | optional | `false` | Require linear history on the default branch (branch protection: `required_linear_history`). |
| `auto_merge`    | boolean | optional | `false` | Allow auto-merge on PRs (GitHub: `allow_auto_merge`). |

Compatibility: GitHub setting names are as documented at schema time
and must be re-verified before implementation (spec.md §7.2).
`linear_history: true` requires branch protection and is incompatible
with `merge_commit: true` (merge commits are non-linear); tooling must
flag that combination. `auto_merge` is only effective when branch
protection requires status checks. These three settings plus `squash`
implement the merge policy in docs/development/pull-requests.md.

## git

Authorship conventions. Object optional.

| Field          | Type | Req      | Default                   | Meaning |
| -------------- | ---- | -------- | ------------------------- | ------- |
| `commit_style` | enum | optional | `"conventional"`          | Commit message convention: the restricted Conventional Commits subset in docs/development/commit-conventions.md. |
| `branch_style` | enum | optional | `"type-issue-description"` | Branch naming convention per docs/development/branching.md (`<type>/<issue>-<description>`). |

Compatibility: v1 defines exactly one value per field; new styles are
additive schema changes (spec.md §16.2). Enforcement is `repoforge
validate` plus required review; the schema records intent, it does not
rewrite history or messages.

## github

Repository feature toggles. Object optional.

| Field         | Type    | Req      | Default | Meaning |
| ------------- | ------- | -------- | ------- | ------- |
| `issues`      | boolean | optional | `true`  | Enable Issues (GitHub: `has_issues`). |
| `discussions` | boolean | optional | `false` | Enable Discussions (GitHub: `has_discussions`). |
| `projects`    | boolean | optional | `false` | Enable repository Projects (GitHub: `has_projects`). |
| `wiki`        | boolean | optional | `false` | Enable the wiki (GitHub: `has_wiki`). MUST be `true` while `repository.governance_profile` is set: the RepoForge governance standard seeds documentation into the wiki (spec.md §13.9). |

Compatibility: organization-level Projects are not modeled in v1.
Disabling a feature with existing content (e.g. turning off Issues on
a repository that has them) hides rather than deletes data; doctor
reports the drift, bootstrap confirms before applying.

## security

Security features. Object optional. Everything here is
security-sensitive (spec.md §8.5): bootstrap plans and confirms each item
individually; doctor audits read-only.

| Field                             | Type    | Req      | Default | Meaning |
| --------------------------------- | ------- | -------- | ------- | ------- |
| `dependabot`                      | boolean | optional | `true`  | Dependabot vulnerability alerts and security updates. |
| `code_scanning`                   | boolean | optional | `false` | Code scanning (CodeQL default setup). |
| `secret_scanning`                 | boolean | optional | `true`  | Secret scanning. |
| `secret_scanning_push_protection` | boolean | optional | `true`  | Push protection for secret scanning. |
| `dependency_review`               | boolean | optional | `true`  | Dependency review on pull requests via CI. |
| `private_vulnerability_reporting` | boolean | optional | `true`  | Private vulnerability reporting (Security tab → Report a vulnerability). |
| `scorecard`                       | boolean | optional | `true`  | OpenSSF Scorecard analysis workflow. |
| `action_pinning`                  | enum    | optional | `"sha"` | Action pinning requirement: `sha` (full-length commit SHAs, spec.md §8.6), `tag`, or `none`. |

Compatibility:

- `code_scanning`: free for public repositories; private repositories
  require GitHub Advanced Security. Requires supported languages.
- `secret_scanning` / push protection: free for public repositories;
  private repositories require GitHub Advanced Security. Push
  protection requires secret scanning enabled — setting
  `secret_scanning_push_protection: true` with `secret_scanning: false`
  is a tooling-reported inconsistency.
- `dependabot`: version updates additionally need a `dependabot.yml`,
  which belongs in `ownership.managed_paths` when the policy manages it.
- `dependency_review`: requires the dependency graph, enabled by
  default on public repositories.
- `private_vulnerability_reporting`: documented for public
  repositories; elsewhere availability is determined by GitHub at
  apply time and failures surface explicitly, never silently.
- `scorecard`: primarily meaningful for public repositories; requires
  a Scorecard workflow file under `ownership.managed_paths`.
- `action_pinning`: enforced by `repoforge validate` and by the
  workflow lint check; the engine repository's own CI complies with
  `sha`.

## release

Release behavior. Object optional. See spec.md §14 and
docs/development/release-process.md.

| Field      | Type | Req      | Default            | Meaning |
| ---------- | ---- | -------- | ------------------ | ------- |
| `strategy` | enum | optional | `"github-release"` | `none`: no releases. `tag`: annotated SemVer tags only. `github-release`: annotated tags plus a GitHub release per tag. |

Compatibility: `tag` and `github-release` both follow SemVer and
annotated tags (release-process.md R1–R5). `none` suits repositories
that version nothing (e.g. `docs`-kind projects).

## project

Purpose classification. Object optional.

| Field  | Type | Req      | Default         | Meaning |
| ------ | ---- | -------- | --------------- | ------- |
| `kind` | enum | optional | `"application"` | One of `application`, `cli`, `library`, `service`, `template`, `docs`, `meta`. |

Compatibility: informational in v1; later schema versions may let it
drive template and doctor behavior. Enum growth is additive. The engine
repository itself is `cli`; the template repository is `template`.

## ownership

Path ownership model (spec.md §5). Object optional. Entries are
gitignore-style glob patterns relative to the repository root (leading
`/` is rejected), unique per list. On overlap, precedence is:
`user_owned_paths` → `managed_paths` → `scaffolded_paths`.

| Field              | Type           | Req      | Default | Meaning |
| ------------------ | -------------- | -------- | ------- | ------- |
| `managed_paths`    | array of globs | optional | `[]`    | Paths the policy owns: bootstrap creates/updates them, validate checks them, manual edits are drift (spec.md §5.2). |
| `scaffolded_paths` | array of globs | optional | `[]`    | Paths generated once (template or first bootstrap) and then user-owned; tooling never modifies them after creation. |
| `user_owned_paths` | array of globs | optional | `[]`    | Paths tooling must never create, modify, or delete (spec.md §5.3). |
| `placeholder_paths` | array of globs | optional | `[]`   | Paths permitted to contain unresolved template placeholders — for template sources such as the engine repository. In generated repositories, placeholders outside this list are validation violations. |

Compatibility: glob semantics are decided — `path.Match` anchored at
the repository root, extended with `**` which matches any number of
path segments including zero. The empty defaults mean a policy that
omits `ownership` manages no files — safe by default, per spec.md §5.3.

## Effective configuration

Because every field except `standard.version` has a default, the
**effective configuration** of a policy is fully determined: omitting a
section equals writing it out with defaults. Tooling materializes this
before planning or auditing, which keeps bootstrap's plan output honest
(no hidden implied changes) and keeps validate deterministic
(spec.md §12.2).
