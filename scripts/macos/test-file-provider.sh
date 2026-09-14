#!/usr/bin/env bash
# Run native enumeration lifecycle checks with Command Line Tools (no Xcode).
set -euo pipefail
repo_root="$(cd "$(dirname "$0")/../.." && pwd)"
cd "$repo_root"
test_dir="$(mktemp -d "${TMPDIR:-/tmp}/mahpastes-fp-tests.XXXXXX")"
trap 'rm -rf "$test_dir"' EXIT
cat > "$test_dir/main.swift" <<'SWIFT'
import Foundation
@main struct Runner {
    static func main() async {
        do {
            try await EnumeratorChecks.expiredAnchorCanRestart()
            try await EnumeratorChecks.completedChangesReleaseBaseline()
            try await EnumeratorChecks.pageRetryRetainsBaselineUnlessExpired()
            try ItemChecks.hiddenFoldersRemainReadable()
            print("Native enumeration lifecycle checks passed")
        } catch {
            print("FAIL: \(error)")
            exit(1)
        }
    }
}
SWIFT
xcrun swiftc -parse-as-library -swift-version 5 -target "$(uname -m)-apple-macosx13.0" \
    native/macos/FileProvider/{Item,Client,Enumerator}.swift \
    native/macos/FileProviderTests/{EnumeratorChecks,ItemChecks}.swift \
    "$test_dir/main.swift" -o "$test_dir/enumerator-tests"
"$test_dir/enumerator-tests"
