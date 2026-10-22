#!/usr/bin/env bash
# Prints the change log of a release in Markdown, from the commits since the
# previous v* tag.
#
#   scripts/changelog.sh TAG [PREV]
#
# PREV is the nearest earlier v* tag when it is not given. With no earlier tag,
# the log has all the commits up to TAG. Merge commits and commits that change
# only Markdown files or docs/ are not in the log. The Issues list has the closed
# GitHub issues in the range and the issues that the commits name as #N. It needs
# gh. If gh fails, the list is "None".
set -euo pipefail

tag=${1:?usage: changelog.sh TAG [PREV]}
prev=${2:-$(git describe --tags --abbrev=0 --match 'v*' "$tag^" 2>/dev/null || true)}
range=${prev:+$prev..}$tag

features=() fixes=() changes=()
while IFS=' ' read -r sha subject; do
	# Skip the commits that change only notes, docs, and the website.
	if ! git diff-tree --no-commit-id --name-only -r "$sha" | grep -qvE '\.md$|^docs/'; then
		continue
	fi
	line="- $subject ($sha)"
	case "$subject" in
	Fix*) fixes+=("$line") ;;
	Add\ * | Show\ * | Support\ * | Track\ * | Allow\ * | Enable\ * | Let\ * | Play\ * | Suggest\ *)
		features+=("$line") ;;
	*) changes+=("$line") ;;
	esac
done < <(git log --no-merges --format='%h %s' "$range")

# The closed issues: the issues closed between the dates of PREV and TAG, and the
# issues that the commits name.
issues=()
to=$(git log -1 --format=%cs "$tag")
if [ -n "$prev" ]; then
	search="closed:$(git log -1 --format=%cs "$prev")..$to"
else
	search="closed:<=$to"
fi
numbers=$({
	gh issue list --state closed --limit 200 --search "$search" --json number --jq '.[].number' 2>/dev/null || true
	git log --format=%B "$range" | grep -oE '#[0-9]+' | tr -d '#' || true
} | sort -nu)
for n in $numbers; do
	if line=$(gh issue view "$n" --json state,title,url \
		--jq 'select(.state == "CLOSED") | "- [#\(.url | split("/") | last)](\(.url)) \(.title)"' 2>/dev/null) &&
		[ -n "$line" ]; then
		issues+=("$line")
	fi
done

list() {
	if [ $# -eq 0 ]; then
		echo "None"
	else
		printf '%s\n' "$@"
	fi
}

echo "## Change log"
echo
if [ -n "$prev" ]; then
	echo "The changes from $prev to $tag."
else
	echo "The changes up to $tag."
fi
echo
echo "### New features"
echo
list ${features[@]+"${features[@]}"}
echo
echo "### Bug fixes"
echo
list ${fixes[@]+"${fixes[@]}"}
echo
echo "### Issues"
echo
list ${issues[@]+"${issues[@]}"}
echo
echo "### Changes"
echo
list ${changes[@]+"${changes[@]}"}
