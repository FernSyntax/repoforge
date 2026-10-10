# Release strategy

Semantic Versioning 2.0.0, from `main`, with a changelog and a GitHub
release per tag.

## Versioning

`vMAJOR.MINOR.PATCH`:

- Breaking changes → MAJOR.
- Backward-compatible capabilities → MINOR.
- Backward-compatible fixes → PATCH.

Pre-1.0, minor versions may break compatibility; every such break is
declared in CHANGELOG.md and in the PR that introduces it.

## Cutting a release

1. **Changelog**: move `Unreleased` entries into
   `## [X.Y.Z] - YYYY-MM-DD` (the tag keeps the `v`), keeping the
   change categories. Land this via a normal PR.
2. **Verify**: full CI green on the release commit.
3. **Tag**: annotated, never lightweight:

   ```sh
   git tag -a vX.Y.Z -m "vX.Y.Z"
   git push origin vX.Y.Z
   ```

   Release tags are immutable (protected by ruleset): never move or
   delete one. A bad release is fixed by a new patch release.
4. **Publish**: create the GitHub release with the tag's changelog
   section as notes.

## Hotfixes

Fix on `main`, ship a new patch release. No release branches pre-1.0
(see [branch-strategy.md](branch-strategy.md)).
