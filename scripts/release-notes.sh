#!/bin/sh
# Copyright the repoforge contributors.
#
# SPDX-License-Identifier: Apache-2.0

# Release notes extraction: the published release notes come from this
# repository's CHANGELOG.md section for the tag, exactly as spec.md §14
# and docs/development/release-process.md R5 require. GoReleaser's own
# changelog generation is bypassed with `--release-notes`.
#
# Usage: release-notes.sh <tag> > notes.md
# Read-only. Exits 1 when the tag has no recorded section, so a release
# without changelog provenance can never be published.

set -eu

tag=${1:?usage: release-notes.sh <tag> (e.g. release-notes.sh v0.1.0)}

# R3: tags are named vMAJOR.MINOR.PATCH, nothing else.
if ! printf '%s' "$tag" | grep -Eq '^v[0-9]+\.[0-9]+\.[0-9]+$'; then
	echo "error: tag $tag is not strict vMAJOR.MINOR.PATCH" >&2
	exit 1
fi

# Keep a Changelog writes the version without the leading "v"; the tag
# keeps it. Both spellings are accepted.
bare=${tag#v}

# Extract from the section heading up to (but excluding) the next
# section heading. The section is the release record: what changed, in
# categories. The "Links"/reference-definition lines at the foot of the
# file are dropped because they are not release notes.
notes=$(python3 - "$tag" "$bare" <<'PY'
import re
import sys

tag, bare = sys.argv[1], sys.argv[2]
text = open("CHANGELOG.md").read()
heading = re.compile(
    r"^## \[(?:%s|%s)\] .*$" % (re.escape(bare), re.escape(tag)), re.M
)
m = heading.search(text)
if not m:
    sys.exit(3)
rest = text[m.end():]
nxt = re.search(r"^## ", rest, re.M)
body = rest[: nxt.start()] if nxt else rest
body = body.rstrip()
if not body:
    sys.exit(4)
print(body)
PY
) || {
	code=$?
	if [ "$code" = 3 ]; then
		echo "error: CHANGELOG.md has no section for $tag" >&2
		exit 1
	fi
	if [ "$code" = 4 ]; then
		echo "error: CHANGELOG.md section for $tag is empty" >&2
		exit 1
	fi
	echo "error: extracting the CHANGELOG.md section for $tag failed" >&2
	exit 1
}

printf '%s\n' "$notes"
