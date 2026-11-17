#!/usr/bin/env bash
# Get the zslice package (os/package/printdeck-zslice) into the download folder of Buildroot.
# os/build.sh runs it. GitHub Packages needs a token for each download: NODE_AUTH_TOKEN (the job
# token of the CI), or the token file on this computer. The token needs only the read:packages
# scope. It goes to npm.pkg.github.com only, and stays out of the build container and of the
# command lines. Buildroot checks the package with its hash file.
set -euo pipefail

OS="$(cd "$(dirname "$0")" && pwd)"
CACHE="${PRINTDECK_OS_CACHE:-$HOME/.cache/printdeck-os}"
TOKENFILE="${PRINTDECK_PACKAGES_TOKEN_FILE:-$HOME/.config/printdeck/packages-token}"

v="$(sed -n 's/^PRINTDECK_ZSLICE_VERSION = //p' "$OS/package/printdeck-zslice/printdeck-zslice.mk")"
dir="$CACHE/dl/printdeck-zslice"
file="$dir/zslice-$v.tgz"
[ -f "$file" ] && exit 0
token="${NODE_AUTH_TOKEN:-}"
if [ -z "$token" ] && [ -r "$TOKENFILE" ]; then
  if [ "$(stat -c %a "$TOKENFILE")" != 600 ]; then
    echo "Other users can read $TOKENFILE. Run: chmod 600 $TOKENFILE" >&2
    exit 1
  fi
  token="$(tr -d '[:space:]' < "$TOKENFILE")"
fi
if [ -z "$token" ]; then
  echo "The image needs zslice $v from GitHub Packages. Put a token with only the read:packages" >&2
  echo "scope in $TOKENFILE (chmod 600), or set NODE_AUTH_TOKEN." >&2
  exit 1
fi
mkdir -p "$dir"
meta="$(printf 'header = "Authorization: Bearer %s"\n' "$token" |
  curl -fsS --proto '=https' -K - https://npm.pkg.github.com/@beetlebugorg%2fzslice)"
url="$(printf '%s' "$meta" | python3 -I -c 'import json,sys; print(json.load(sys.stdin)["versions"][sys.argv[1]]["dist"]["tarball"])' "$v")"
case "$url" in
  https://npm.pkg.github.com/*) ;;
  *) echo "The zslice package is not on npm.pkg.github.com: $url" >&2; exit 1 ;;
esac
printf 'header = "Authorization: Bearer %s"\n' "$token" |
  curl -fsSL --proto '=https' --max-redirs 3 -K - -o "$file.part" "$url"
mv "$file.part" "$file"
