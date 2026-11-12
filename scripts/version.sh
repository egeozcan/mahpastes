#!/usr/bin/env bash
# Version information for builds.
#
# Usage: scripts/version.sh [--full|--product|--commit|--ldflags|--stamp]
#
#   --full      the version the binaries report (default): "1.5.0" on a release
#               tag, "1.5.0-12-gabc1234" between releases, "dev" without git
#   --product   the numeric part ("1.5.0"), which is what the macOS Info.plist
#               (CFBundleShortVersionString/CFBundleVersion) and the Windows
#               version resource accept; empty for "dev"
#   --commit    the commit hash, or empty
#   --ldflags   the -X flags that stamp internal/buildinfo
#   --stamp     write --product into wails.json's info.productVersion, which
#               Wails puts into Info.plist and the Windows resource. Meant for
#               CI checkouts: it modifies a tracked file. Does nothing for "dev".
#
# MAHPASTES_VERSION overrides the version (a tag name such as "v1.6.0" is
# fine); otherwise it comes from `git describe` over v* tags.
set -euo pipefail

repo="$(cd "$(dirname "$0")/.." && pwd)"
cd "$repo"

full="${MAHPASTES_VERSION:-}"
if [[ -z "$full" ]]; then
    full="$(git describe --tags --match 'v[0-9]*' 2>/dev/null || true)"
fi
full="${full#v}"
[[ -n "$full" ]] || full=dev
commit="$(git rev-parse --short=12 HEAD 2>/dev/null || true)"

# The leading dotted number: "1.5.0-12-gabc" -> "1.5.0", "dev" -> "".
product="$(printf '%s' "$full" | sed -nE 's/^([0-9]+(\.[0-9]+){0,2}).*/\1/p')"

pkg=go-clipboard/internal/buildinfo
case "${1:---full}" in
--full) echo "$full" ;;
--product) echo "$product" ;;
--commit) echo "$commit" ;;
--ldflags) echo "-X $pkg.Version=$full -X $pkg.Commit=$commit" ;;
--stamp)
    if [[ -z "$product" ]]; then
        echo "No release version to stamp (version is '$full'); wails.json left as is." >&2
        exit 0
    fi
    sed -E -i.bak "s/(\"productVersion\"[[:space:]]*:[[:space:]]*\")[^\"]*\"/\1$product\"/" wails.json
    rm -f wails.json.bak
    grep -q "\"productVersion\": *\"$product\"" wails.json || {
        echo "Could not stamp productVersion into wails.json." >&2
        exit 1
    }
    echo "Stamped wails.json productVersion $product" >&2
    ;;
*)
    echo "Usage: $0 [--full|--product|--commit|--ldflags|--stamp]" >&2
    exit 1
    ;;
esac
