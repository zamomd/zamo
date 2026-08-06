#!/usr/bin/env bash
# Checks the comments a change adds: no narrating what the code used to be,
# and no wall of text where a sentence belongs. Only added lines are judged,
# so existing comments are left alone.
#
#   comment-hygiene.sh            the staged change (what a commit would take)
#   comment-hygiene.sh --branch   every commit on this branch, against main
set -euo pipefail

here=$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)
mode=${1:-staged}

case $mode in
staged)
	diff_args=(--cached)
	blob() { git show ":$1"; }
	;;
--branch)
	tip=${2:-HEAD}
	base=$(git merge-base "$tip" origin/main 2>/dev/null || true)
	if [ -z "$base" ]; then
		echo "comment-hygiene: no origin/main to compare against; skipped" >&2
		exit 0
	fi
	diff_args=("$base...$tip")
	blob() { git show "$tip:$1"; }
	;;
*)
	echo "usage: comment-hygiene.sh [--branch]" >&2
	exit 2
	;;
esac

files=$(git diff "${diff_args[@]}" --name-only --diff-filter=d -- '*.go' '*.svelte' '*.ts' \
	| grep -v '^frontend/bindings/' || true)
[ -z "$files" ] && exit 0

tmp=$(mktemp -d)
trap 'rm -rf "$tmp"' EXIT
status=0

while IFS= read -r file; do
	# The line numbers this change added, read off the hunk headers.
	git diff "${diff_args[@]}" --unified=0 -- "$file" | awk '
		/^@@/ { match($0, /\+[0-9]+/); n = substr($0, RSTART + 1, RLENGTH - 1) + 0; next }
		/^\+\+\+/ { next }
		/^\+/ { print n; n++ }
	' >"$tmp/added"
	[ -s "$tmp/added" ] || continue

	blob "$file" >"$tmp/source"
	awk -v ADDED="$tmp/added" -v NAME="$file" -f "$here/comment-hygiene.awk" \
		"$tmp/added" "$tmp/source" || status=1
done <<<"$files"

if [ "$status" -ne 0 ]; then
	cat >&2 <<'EOF'

BLOCK: a comment is describing what the code used to be. That belongs to the
commit message — see AGENTS.md. WARN lines are for a human to judge and do
not stop anything.
EOF
fi
exit "$status"
