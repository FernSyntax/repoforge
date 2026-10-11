#!/bin/sh
# release-check.sh validates a release tag against the release policy
# (docs/development/release-process.md): strict SemVer tag shape,
# annotated tag object, a matching CHANGELOG.md section, and a
# version-stamped build that reports the tag.
#
# Read-only: inspects git, files, and a throwaway build. No tagging,
# pushing, or publishing happens here.
set -eu

tag=${1:?usage: release-check.sh <tag> (e.g. release-check.sh v0.1.0)}

# R3: tags are named vMAJOR.MINOR.PATCH, nothing else.
if ! printf '%s' "$tag" | grep -Eq '^v[0-9]+\.[0-9]+\.[0-9]+$'; then
	echo "error: tag $tag is not strict vMAJOR.MINOR.PATCH" >&2
	exit 1
fi

# R3: annotated, never lightweight.
if [ "$(git cat-file -t "$tag")" != "tag" ]; then
	echo "error: tag $tag is not an annotated tag object" >&2
	exit 1
fi

# R3: the tag must target a commit reachable from the protected main
# history. A tag on an unmerged branch, a stale checkout, or any commit
# outside mainline would otherwise publish a release GitHub cannot
# relate to the branch the ruleset protects — and the release notes,
# provenance, and template publication would all describe a commit the
# project never reviewed as mainline.
#
# This needs the full history the Release workflow fetches
# (fetch-depth: 0). A shallow clone cannot prove ancestry either way,
# so it fails closed with the remedy instead of passing blind.
tag_commit=$(git rev-parse --verify --quiet "$tag^{commit}") || {
	echo "error: tag $tag does not resolve to a commit" >&2
	exit 1
}
if [ "$(git rev-parse --is-shallow-repository 2>/dev/null)" = "true" ]; then
	echo "error: cannot prove $tag ($tag_commit) is on mainline from a shallow clone; fetch full history (fetch-depth: 0)" >&2
	exit 1
fi
mainline=""
if git show-ref --verify --quiet refs/remotes/origin/main; then
	mainline="refs/remotes/origin/main"
elif git show-ref --verify --quiet refs/heads/main; then
	mainline="refs/heads/main"
else
	echo "error: no main branch ref found; cannot prove $tag ($tag_commit) is on mainline" >&2
	exit 1
fi
if ! git merge-base --is-ancestor "$tag_commit" "$mainline" 2>/dev/null; then
	echo "error: tag $tag ($tag_commit) is not reachable from $mainline; releases publish mainline commits only" >&2
	exit 1
fi

# R4: every release is recorded in CHANGELOG.md. Keep a Changelog
# writes the version without the leading "v", so both spellings are
# accepted; the tag keeps the "v".
bare=${tag#v}
if ! grep -qE "^## \[(${tag#v}|$tag)\]" CHANGELOG.md; then
	echo "error: CHANGELOG.md has no section for $tag (looked for '## [$tag]' or '## [$bare]')" >&2
	exit 1
fi

# R7: the CLI version is stamped from the tag at build time.
tmp=$(mktemp -d)
trap 'rm -rf "$tmp"' EXIT INT TERM
go build -o "$tmp/repoforge" \
	-ldflags "-X github.com/FernSyntax/repoforge/internal/version.Version=$tag" \
	./cmd/repoforge
reported=$("$tmp/repoforge" version)
if [ "$reported" != "$tag" ]; then
	echo "error: stamped build reports $reported, want $tag" >&2
	exit 1
fi

echo "release $tag is valid"
