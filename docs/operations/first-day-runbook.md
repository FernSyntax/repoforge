# First-Day Runbook

Bringing a repository under the RepoForge standard, and recovering one
that has drifted. Every step is verified before the next one.

## New repository

1. **Create from the template.**

   ```sh
   gh repo create <owner>/<name> --template FernSyntax/repoforge-template --public --clone
   cd <name>
   ```

2. **Adopt.** Resolve every placeholder with real values, preview first:

   ```sh
   repoforge template adopt --dry-run --var ProjectName=<name> --var Owner=<owner> \
     --var RepoURL=<url> --var Description=<description> --var StandardVersion=v1
   repoforge template adopt --var ProjectName=<name> --var Owner=<owner> \
     --var RepoURL=<url> --var Description=<description> --var StandardVersion=v1
   ```

   Then `repoforge validate` must be clean.
3. **Configure.** Edit `.repoforge/config.yml`: `visibility`,
   `governance_profile`, `project.kind`. Remember the governance
   standard requires `issues: true` and `wiki: true`.
4. **Converge.** `repoforge bootstrap --dry-run`, read the plan, then
   `repoforge bootstrap`. Re-run: it must report zero changes.
5. **Audit.** `repoforge doctor` — every finding PASS or WARN, no FAIL.
6. **Seed the wiki.** `repoforge wiki sync --repo <owner>/<name>`,
   then `--apply`.
7. **Open the first issue** and start working issue-first.

## Existing repository

1. Add `repoforge.yaml` (see
   [../reference/configuration.md](../reference/configuration.md)).
2. Run `repoforge validate` until clean. Fix findings in dependency
   order: schema, then ownership, then content.
3. `repoforge bootstrap --dry-run` and read the whole plan. The first
   plan against a grown repository is large; large plans deserve slow
   reading.
4. Apply, then re-run. Convergence means the second run reports zero
   changes.

## Recovery

| Situation | What to do |
| --- | --- |
| `validate` reports a schema violation | Fix the policy field it names. Do not disable the check. |
| `bootstrap` reports `conflict` | An unmanaged ruleset needs a human decision. Nothing was changed; decide, then re-run. |
| `bootstrap` reports `unsupported` | A plan-limited feature. Lower the setting explicitly or change plan; never silently. |
| `doctor` FAIL after green | Drift. `repoforge bootstrap` converges it; re-run `doctor`. |
| A partial `bootstrap` | Re-run it. The plan is recomputed from actual state, so the next run resumes rather than double-applying. |
| A build's `template verify` fails | The tree is not a faithful build of the recorded source. Rebuild from `template/`; never hand-patch the output. |
| The wiki diverged | Fix `docs/wiki/<page>.md` and re-run sync. Never hand-edit the wiki. |

## What never happens

- Bootstrap never changes repository visibility.
- Bootstrap never deletes a ruleset, a label, or a file it does not own.
- `doctor` and `validate` never mutate anything.
- Publication never writes directly to an existing template `main`.
