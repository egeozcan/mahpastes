#!/usr/bin/env bash
# Fill the Scoop manifest and Homebrew cask templates for one release.
#   packaging/render.sh <tag-or-version> <SHA256SUMS> <output-dir>
# The release workflow runs this and uploads mahpastes.json (Scoop) and
# mahpastes.rb (Homebrew cask) beside the other assets. See packaging/README.md.
set -euo pipefail

if [[ $# -ne 3 ]]; then
  echo "usage: $0 <tag-or-version> <SHA256SUMS> <output-dir>" >&2
  exit 2
fi
version="${1#v}"
sums="$2"
out="$3"
here="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"

sum_of() {
  local hash
  hash="$(awk -v f="$1" '$2 == f || $2 == "*" f { print $1 }' "$sums")"
  if [[ ! "$hash" =~ ^[0-9a-f]{64}$ ]]; then
    echo "no SHA-256 for $1 in $sums" >&2
    exit 1
  fi
  printf '%s' "$hash"
}

win_amd64="$(sum_of mahpastes-windows-amd64.zip)"
win_arm64="$(sum_of mahpastes-windows-arm64.zip)"
macos="$(sum_of mahpastes-macos-universal.zip)"

mkdir -p "$out"
render() {
  sed -e "s/@VERSION@/$version/g" \
      -e "s/@SHA_WIN_AMD64@/$win_amd64/g" \
      -e "s/@SHA_WIN_ARM64@/$win_arm64/g" \
      -e "s/@SHA_MACOS@/$macos/g" "$1" > "$2"
}
render "$here/scoop/mahpastes.json" "$out/mahpastes.json"
render "$here/homebrew/mahpastes.rb" "$out/mahpastes.rb"
