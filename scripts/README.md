# Scripts

Development and release helper scripts.

## Authoritative verification

- `verify [fast|full] [--with-act] [--with-integration]` — the single
  local verification entry point. `fast` (default) runs formatting,
  build, unit tests, `repoforge validate` and
  `repoforge template validate` offline. `full` adds race detection
  and the template reproducibility proof, plus shellcheck and actionlint
  when installed. Both print the exact command before running it and
  never report success after a failed check.
  `--with-act` and `--with-integration` are explicit opt-ins that touch
  the network and, in the integration case, a real repository.
- `hooks-install` — installs the developer-side Git hooks
  (`--verify` to check, `--uninstall` to remove only ours). Documented
  in `docs/development/git-hooks.md`.
- `check-schema-parity` — proves every `schemas/*.schema.yaml` is
  semantically identical to its `.schema.json` (spec.md §9.1). Exits 2 when
  PyYAML is unavailable rather than passing silently.

## Existing helpers

- `release-check.sh <tag>` — validates a release tag against the
  release policy (SemVer shape, annotated tag, CHANGELOG.md section,
  version-stamped build). Read-only; run by the Release Validation
  workflow and locally before tagging.
- `build-template [dir]` — validates `template/` and renders the
  template baseline into `dir` (default `.template-out/`, ignored
  by Git). See `docs/development/template-publication.md`.
- `verify-template [dir]` — validates a built template directory and
  proves it byte-identical to a fresh build (determinism check).

## `scripts/hooks/`

Helpers invoked from `.pre-commit-config.yaml`. Each is a real,
executable, testable script rather than an inline command, so its
behaviour can be asserted.

- `only-generated-files --template-output|--local-binaries` — refuses
  staged generated output (`.template-out/`) and local binaries
  (`bin/`), naming the exact `git rm --cached` remediation.
- `need-tool <binary> "<install hint>" [-- <command> …]` — reports a
  missing tool with install instructions and fails, so a missing tool
  can never look like a passed check.
- `commit-msg` — bridges pre-commit's commit-message file to
  `repoforge git commit-check`.

## Rules for scripts in this directory

- POSIX sh or bash, `set -euo pipefail` where supported.
- Idempotent: safe to run repeatedly.
- Any script with external side effects must support a dry-run or
  read-only mode.
- No credentials in scripts, ever.
- Resolve the repository root rather than assuming `$PWD`, so a script
  works from any directory.
- Print a useful failure and an actionable remediation; never swallow a
  non-zero exit.
