# Template Publication

How `FernSyntax/repoforge-template` receives its content. The engine
repository is the only routine writer: the template repository is never
edited by hand, and every routine change arrives as a generated pull
request (spec.md §13.8).

This document is the authoritative publication runbook. There is no
second one.

## Owner-side setup required BEFORE the first tag push

These steps cannot be performed by the workflow and must be done first,
because the publication job fails closed without them. Nothing may be
pushed to either repository until they are complete.

1. **Create the template repository, empty.** No initial commit, no
   README, no `.gitignore`:

   ```sh
   gh repo create FernSyntax/repoforge-template --public \
     --description "Generated starter repository: the RepoForge project baseline."
   ```

   The initial publication path refuses a destination with any commits,
   branches, tags, or releases.
2. **Create the engine repository, empty,** and push `main` and the
   `v0.1.0` annotated tag.
3. **Register and install the GitHub App** on the template repository
   only (steps below).
4. **Create the two engine secrets** (steps below).
5. **Enable the dependency graph** on the engine repository:
   Settings → Security → Dependency graph. Public repositories normally
   have it automatically; a freshly created one may not, and
   `dependency-review-action` fails with "Dependency review is not
   supported on this repository" until it is on. This is a repository
   setting with no REST API.
6. **Run the preflight** (`Template Preflight` workflow, manual
   dispatch). It mints a token and proves the App, the installation
   scope, and the build.

## One-time GitHub setup (owner, in this order)

## The pipeline

```
RepoForge release tag
        |
        v
Canonical template/ source
        |
        v
Resolve the release identity (API): release exists, annotated tag,
tag object -> commit
        |
        v
Checkout the resolved commit (as data, never an event expression)
        |
        v
Build + render (immutable provenance recorded in the manifest)
        |
        v
Validate: model, manifest, determinism
        |
        v
Upload the verified build
        |
        v
  ┌──────────────────────────────────────────────┐
  │ publish job (needs: build) — trusted code    │
  │ Checkout the default-branch engine           │
  │ Download + re-verify the artifact            │
  │ Re-derive the release identity (manifest+API)│
  │ ... then mint the App token, only here       │
  └──────────────────────────────────────────────┘
        |
        +-------------------------+
        |                         |
        v                         v
Empty target                Target with history
        |                         |
        v                         v
One root commit on main     Generated update branch + PR
        |                         |
        v                         v
Hosted template CI          Required checks
        |                         |
        v                         v
Publish v0.1.0              Publish matching release after merge
```

The workflow is two jobs with one trust boundary (ADR-0014). The
**build** job executes the release source and holds no credential at
all — `contents: read`, no secrets, no id-token. The **publish** job
holds the App private key and executes only trusted code: the engine
from the default branch, plus the verified artifact. The private key
therefore never coexists with the execution of release-source code,
and the artifact's release identity is re-derived with trusted tooling
(manifest source commit and digest, tag resolution through the API)
before the token is minted.

## The credential: one dedicated GitHub App

Publication does not use a personal access token. A dedicated GitHub
App installed on **only** `FernSyntax/repoforge-template` is the
publishing principal, and an installation access token is minted only
inside the trusted publication job.

### One-time setup

Performed once, by the repository owner. Nothing in this procedure is
performed by the tool.

1. **Register the App.**
   GitHub → Settings → Developer settings → GitHub Apps → New GitHub App.
   - Name: `repoforge-template-publisher` (or your own naming).
   - Homepage URL: `https://github.com/FernSyntax/repoforge`.
   - Webhook: **uncheck "Active"**. The App needs no webhooks.
2. **Set the permissions.** Exactly these, and nothing else:

   | Permission | Level | Why |
   | --- | --- | --- |
   | Repository metadata | Read | Identify and verify the target. |
   | Contents | Read and write | Generated commits, version tags, releases. |
   | Workflows | Read and write | Generated content includes `.github/workflows/`. |
   | Pull requests | Read and write | Create and update generated PRs. |

   `Contents: write` alone is **not** sufficient to commit workflow
   files: GitHub requires `Workflows: write` for that, which is why the
   permission is listed. No administration permission is granted.
3. **Generate a private key.** On the App page, "Private keys" →
   "Generate a private key". A `.pem` file downloads.
4. **Install the App on one repository.** App page → "Install App" →
   the owner account → **Only select repositories** →
   `repoforge-template`.
5. **Create the engine repository secrets.** The App page shows the App
   ID. Then:

   ```sh
   gh secret set REPOFORGE_APP_ID --repo FernSyntax/repoforge --body "<app id>"
   gh secret set REPOFORGE_APP_PRIVATE_KEY --repo FernSyntax/repoforge \
     --body "$(cat path/to/private-key.pem)"
   ```

   The private key is never committed and never written to a local
   configuration file.
6. **Verify the names exist without exposing values.**

   ```sh
   gh secret list --repo FernSyntax/repoforge
   ```

   Listing names proves they are configured in a session whose token may
   list secrets. It cannot prove the key is valid, and a token without
   that scope reports "missing" for a secret that is perfectly well
   configured — which is why the preflight does not rely on it.
7. **Run the preflight** (`Template Preflight` workflow, manual
   dispatch). It mints an installation token and uses it, which is the
   only real proof the key is valid.

### Rotating or revoking

- **Rotate**: generate a new private key on the App page, update
  `REPOFORGE_APP_PRIVATE_KEY`, delete the old key from the App.
- **Revoke immediately**: App page → "Advanced" → "Revoke all
  installation tokens". This invalidates every token in flight without
  touching the installation.
- **Remove the App entirely** when publication is retired: uninstall it
  from the repository, then delete the two engine secrets.

### Diagnosing missing permissions

| Symptom | Cause | Fix |
| --- | --- | --- |
| The preflight cannot read the target | App not installed on it, or `Repository metadata: read` missing | Reinstall with the repository selected. |
| The token resolves a different repository | The App is installed on more than the intended repository | Reinstall with "Only select repositories". |
| Publication fails committing `.github/workflows/` | `Workflows: write` missing | Add it and re-run. |
| Publication fails opening a PR | `Pull requests: write` missing | Add it and re-run. |

## Initial publication

The target is empty and cannot receive a pull request against a `main`
that does not exist yet, so the first publication is a distinct path.

1. Confirm the owner-side setup above is complete, and the preflight
   passes.
2. Push the annotated `v0.1.0` tag on the engine. Pushing the tag is the
   single firing action; `Template Publication` runs from
   `workflow_run` only after the engine `Release` workflow succeeded.
3. The workflow builds from the tagged source, writes
   `repoforge.manifest.json` recording the tag, the commit SHA, the
   source URL, and the RepoForge version, and verifies the tree with
   `--require-source` (it fetches the recorded commit and rebuilds).
4. It then refuses anything but a truly empty destination — zero
   commits, branches, tags, and releases — before the first write.
5. It pushes the generated tree as the **first and only ref** on `main`.
   This is a ref creation, not a force-push: there is no history to
   overwrite.
6. The target's hosted CI runs on that commit. The workflow waits, and
   treats a permission failure on reading check runs as an error rather
   than as "pending".
7. Only after CI is green does it create the **annotated tag object**
   (`POST /repos/.../git/tags`) and then the ref that points at it —
   never a lightweight ref directly over the commit.
8. It verifies the tag object's type is `tag`, that it dereferences to
   the intended root commit, and that the final state is exactly one
   commit, one tag, and one release, before publishing the release.

If hosted CI fails after initial publication, no release is published
and no corrective commit is created. Investigate, fix the candidate
locally, and re-establish a clean initial state before claiming
success (spec.md §10.3, A11).

## Routine updates

For a later RepoForge release:

1. The workflow builds the template from the new immutable tag.
2. It reads the target's current manifest and source revision.
3. It creates or reuses `repoforge-sync/<version>`.
4. It applies only the verified generated output and manifest.
5. It opens or updates a generated pull request carrying the source
   tag, source SHA, manifest digest, and a change summary.
6. After the required checks pass and a maintainer merges, the
   corresponding template release is published against the generated
   commit.

`main` is never written directly on this path.

## Manual dispatch: kept, but constrained

The `workflow_dispatch` input exists for one purpose: re-running a
publication that failed after its release already succeeded, without
re-running (and re-publishing) the release itself. It is not a way to
publish an arbitrary ref, and reviewing that question is what keeps it:

- The input must name an existing release tag in strict SemVer form;
  anything else is rejected before any credential is minted.
- The tag must be an annotated tag object resolving to the exact
  commit checked out, and the engine release for that tag must exist.
  A branch, a lightweight tag, or a commit the release does not target
  is refused.
- The publishing destination stays fixed at
  `FernSyntax/repoforge-template` regardless of input; no input can
  broaden it.
- In both trigger modes the workflow file that runs comes from the
  engine's default branch — never from the tag — so a tag supplies
  data (the commit to build) but never code.

The dedicated preflight (`Template Preflight` workflow) remains the
safer first step whenever the App, its installation, or its permissions
are in doubt: it mints a token and proves the scope without publishing
anything. Manual dispatch is for retrying a vetted release, not for
exploring an unvetted one.

## Idempotency

Re-running a publication for an already-applied source version is a
no-op: the diff against the target is empty, so the workflow reports
that the target is synchronized and creates no commit, no PR, no tag,
and no release.

## Recovering an interrupted publication

The concurrency group is keyed on the source release identity
(`template-publication-<tag>`), and `cancel-in-progress` is false, so
an active publication is never killed by a later run for a different
release. If a run nevertheless stops midway — runner failure, API
outage, revoked token — the recovery is always the same: **re-run the
publication for the same tag**. Do not repair by hand.

Why re-running is safe at every stage:

- Before any write, all verification is read-only (tag resolution,
  destination checks, build, provenance proof), so an interruption
  there leaves nothing behind.
- If the root commit landed but the tag did not, the next run sees a
  non-empty target, takes the routine path, and opens a PR that
  contains exactly the generated tree. Review it, merge it, and the
  template converges without a second root commit.
- Tags and releases are created only after the template's own CI is
  green on the published commit, and the final state check verifies
  one commit, one tag, and one release. A run that fails there has
  published nothing irreversible.
- API errors always fail the run. They are never converted into empty
  state or a silent skip: a 409 on an empty repository counts as zero
  commits, and anything else exits non-zero with the endpoint named.

What never helps: force-pushing the template, deleting the tag to
"retry cleanly", or editing the template by hand. All three destroy the
evidence the next run needs to converge safely. If the target is in a
state no documented path covers, open an issue and follow the
break-glass procedure below.

## Break-glass manual repair

Manual editing of the template repository is an exceptional, documented
procedure — not the standard update mechanism.

1. Record why automation could not do it.
2. Make the smallest possible change, by hand, on a branch.
3. Open a pull request describing the divergence from the canonical
   source and the plan to restore it.
4. In the same change or the next one, correct the engine's `template/`
   source so the next publication converges the repository back.

## Registering the target as a template repository

One-time, owner-side, and not automatable through the App's permission
set:

```sh
gh api --method PATCH repos/FernSyntax/repoforge-template -F is_template=true
```

### Template-repository protection (owner-side, not automatable)

The generated template's `.repoforge/config.yml` carries unresolved
placeholders until adoption, so there is no policy `repoforge bootstrap`
can converge to. Protection is therefore configured by the owner:

```sh
# Main branch: pull-request-only, no force-push, no deletion, and the
# three required checks the template's own CI reports.
gh api --method POST repos/FernSyntax/repoforge-template/rulesets --input - <<'JSON'
{
  "name": "repoforge:main",
  "target": "branch",
  "enforcement": "active",
  "bypass_actors": [],
  "conditions": { "ref_name": { "include": ["~DEFAULT_BRANCH"], "exclude": [] } },
  "rules": [
    { "type": "pull_request", "parameters": {
        "allowed_merge_methods": ["squash"],
        "required_approving_review_count": 0,
        "required_review_thread_resolution": true } },
    { "type": "required_status_checks", "parameters": {
        "required_status_checks": [
          { "context": "verify" }, { "context": "repoforge" }, { "context": "provenance" }
        ],
        "strict_required_status_checks_policy": true } },
    { "type": "required_linear_history" },
    { "type": "non_fast_forward" },
    { "type": "deletion" }
  ]
}
JSON

# Release tags: immutable.
gh api --method POST repos/FernSyntax/repoforge-template/rulesets --input - <<'JSON'
{
  "name": "repoforge:release-tags",
  "target": "tag",
  "enforcement": "active",
  "bypass_actors": [],
  "conditions": { "ref_name": { "include": ["refs/tags/v*"], "exclude": [] } },
  "rules": [ { "type": "deletion" }, { "type": "update" } ]
}
JSON

# Register as a template.
gh api --method PATCH repos/FernSyntax/repoforge-template -F is_template=true
```

Until these are applied, the template repository is **not** protected —
say so, do not claim otherwise. The engine repository's protection is
converged by `repoforge bootstrap` from `repoforge.yaml` and can be
asserted; the template's cannot.

### Verified live state

Verified 2026-10-11 via the authenticated session against
`FernSyntax/repoforge-template`:

- `repoforge:main` (branch, active): PR-only with 0 required approvals
  (no independent approval a single maintainer cannot supply), squash
  merges only, `required_status_checks` for exactly `verify`,
  `repoforge`, and `provenance` — the three checks the template's CI
  actually emits — plus linear history, non-fast-forward, and deletion
  protection. Bypass actors: none, including the publication bot.
- `repoforge:release-tags` (tag, active): `deletion` and `update` rules,
  so release tags are immutable.
- `is_template: true`.

The contract above is pinned by
`TestTemplateBranchProtectionContract` and
`TestTemplateTagRulesetIsImmutable` against the canonical ruleset files
and the template CI job names. If the live repository ever diverges
from this section, the section — not the repository — is what must
change first, through a reviewed PR.
