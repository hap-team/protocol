#!/usr/bin/env bash
set -euo pipefail

if [[ $# -ne 1 ]]; then
  echo "usage: scripts/build-release.sh VERSION" >&2
  exit 2
fi

version="$1"
if [[ ! "$version" =~ ^0\.2\.0(-rc\.[1-9][0-9]*)?$ ]]; then
  echo "version must be 0.2.0 or a 0.2.0 release candidate" >&2
  exit 2
fi

repo_root="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
source_date_epoch="${SOURCE_DATE_EPOCH:-1784188800}"
if touch_stamp="$(TZ=UTC date -r "$source_date_epoch" +%Y%m%d%H%M.%S 2>/dev/null)"; then
  :
else
  touch_stamp="$(TZ=UTC date -d "@$source_date_epoch" +%Y%m%d%H%M.%S)"
fi
stage="$(mktemp -d)"
trap 'rm -rf "$stage"' EXIT

package="$stage/hap-protocol-$version"
mkdir -p "$package/schemas" "$package/conformance" "$package/docs"
cp -R "$repo_root/schemas/0.2" "$package/schemas/"
cp -R "$repo_root/conformance/descriptors" "$package/conformance/"
cp -R "$repo_root/conformance/messages" "$package/conformance/"
mkdir -p "$package/conformance/scenarios"
cp "$repo_root"/conformance/scenarios/*.json "$package/conformance/scenarios/"
cp -R "$repo_root/conformance/testagent" "$package/conformance/"
cp "$repo_root/README.md" "$repo_root/CHANGELOG.md" "$package/"
cp "$repo_root/docs/protocol.md" \
  "$repo_root/docs/migrating-from-0.1.md" \
  "$repo_root/docs/release-policy.md" \
  "$package/docs/"

(
  cd "$package"
  find schemas conformance -type f -print | LC_ALL=C sort | xargs sha256sum >SHA256SUMS
)
find "$package" -exec touch -h -t "$touch_stamp" {} +

mkdir -p "$repo_root/dist"
archive="$repo_root/dist/hap-protocol-$version.tar.gz"
file_list="$stage/files.txt"
(
  cd "$stage"
  find "hap-protocol-$version" -type f -print | LC_ALL=C sort >"$file_list"
  COPYFILE_DISABLE=1 tar --format ustar -cf "$archive.tmp" -T "$file_list"
)
gzip -n -c "$archive.tmp" >"$archive"
rm "$archive.tmp"

(
  cd "$repo_root/dist"
  sha256sum "$(basename "$archive")" >SHA256SUMS
)

echo "$archive"
