#!/bin/sh
# Builds Printdeck.curapackage: drag it onto the Cura window to install the plugin. The version is
# the version of plugin.json.
set -eu
cd "$(dirname "$0")"
OUT="${1:-$PWD/Printdeck.curapackage}"
VERSION=$(python3 -c 'import json; print(json.load(open("Printdeck/plugin.json"))["version"])')
TMP=$(mktemp -d)
trap 'rm -rf "$TMP"' EXIT
mkdir -p "$TMP/files/plugins"
cp -r Printdeck "$TMP/files/plugins/"
find "$TMP" -name __pycache__ -prune -exec rm -rf {} +
cat > "$TMP/package.json" <<JSON
{
  "package_id": "Printdeck",
  "package_type": "plugin",
  "display_name": "Printdeck",
  "description": "The roll of filament in the Printdeck, its material, and the print in Preview.",
  "package_version": "$VERSION",
  "sdk_version": 7,
  "sdk_version_semver": "7.0.0",
  "website": "https://beetlebug.org/printdeck/",
  "author": {"author_id": "beetlebug", "display_name": "Printdeck", "website": "https://beetlebug.org/printdeck/"}
}
JSON
rm -f "$OUT"
python3 -c 'import shutil, sys; shutil.make_archive(sys.argv[1], "zip", sys.argv[2])' "$TMP.pkg" "$TMP"
mv "$TMP.pkg.zip" "$OUT"
echo "wrote $OUT"
