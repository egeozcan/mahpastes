#!/usr/bin/env bash
set -euo pipefail

# Usage: scripts/macos/ci-sign.sh [command-line binary...]
#
# Signs, notarizes and staples build/bin/mahpastes.app. Any binaries given as
# arguments (mp, mahpastesd) are signed with the same identity and notarized
# too, while the temporary Keychain still exists.
# This imports a private signing identity only on an ephemeral GitHub runner.
[[ "${GITHUB_ACTIONS:-}" == true && "${RUNNER_OS:-}" == macOS ]] || {
    echo "This script requires a GitHub Actions macOS runner." >&2
    exit 1
}
for bin in "$@"; do
    if [[ ! -f "$bin" || ! -x "$bin" ]]; then
        echo "Not an executable to sign: $bin" >&2
        exit 1
    fi
done
for name in MACOS_CERTIFICATE_P12 MACOS_CERTIFICATE_PASSWORD MACOS_HOST_PROFILE MACOS_EXTENSION_PROFILE TEAM_ID SIGN_IDENTITY APPLE_ID APPLE_APP_SPECIFIC_PASSWORD; do
    if [[ -z "${!name:-}" ]]; then
        echo "Missing signing configuration: $name (see docs/MASTER_BUILDS.md)." >&2
        exit 1
    fi
done

signing_dir="$(mktemp -d "$RUNNER_TEMP/mahpastes-signing.XXXXXX")"
keychain="$signing_dir/signing.keychain-db"
cleanup() {
    security delete-keychain "$keychain" >/dev/null 2>&1 || true
    rm -rf "$signing_dir"
}
trap cleanup EXIT
export HOST_PROFILE_PATH="$signing_dir/host.provisionprofile"
export EXTENSION_PROFILE_PATH="$signing_dir/extension.provisionprofile"
printf '%s' "$MACOS_CERTIFICATE_P12" | base64 --decode > "$signing_dir/identity.p12"
printf '%s' "$MACOS_HOST_PROFILE" | base64 --decode > "$HOST_PROFILE_PATH"
printf '%s' "$MACOS_EXTENSION_PROFILE" | base64 --decode > "$EXTENSION_PROFILE_PATH"
keychain_password="$(openssl rand -hex 32)"
security create-keychain -p "$keychain_password" "$keychain"
security set-keychain-settings -lut 21600 "$keychain"
security unlock-keychain -p "$keychain_password" "$keychain"
security import "$signing_dir/identity.p12" -k "$keychain" \
    -P "$MACOS_CERTIFICATE_PASSWORD" -T /usr/bin/codesign
security set-key-partition-list -S apple-tool:,apple:,codesign: -s \
    -k "$keychain_password" "$keychain" >/dev/null
security list-keychains -d user -s "$keychain"
# Keep notarization credentials in the same temporary Keychain as signing.
export NOTARY_PROFILE=mahpastes-ci
xcrun notarytool store-credentials "$NOTARY_PROFILE" --keychain "$keychain" \
    --apple-id "$APPLE_ID" --team-id "$TEAM_ID" \
    --password "$APPLE_APP_SPECIFIC_PASSWORD"
bash scripts/macos/build-file-provider.sh --sign-existing

(($#)) || exit 0
# Command-line tools are signed one by one (lipo-built universal binaries carry
# a signature per slice) and notarized in a single submission. A bare Mach-O
# cannot hold a stapled ticket, so Gatekeeper fetches it online on first run.
cli_dir="$signing_dir/command-line-tools"
mkdir -p "$cli_dir"
for bin in "$@"; do
    codesign --force --options runtime --timestamp --sign "$SIGN_IDENTITY" \
        --identifier "${FP_BUNDLE_ID:-io.github.egeozcan.mahpastes}.$(basename "$bin")" "$bin"
    codesign --verify --strict --verbose=2 "$bin"
    cp "$bin" "$cli_dir/"
done
cli_archive="$signing_dir/command-line-tools.zip"
ditto -c -k --keepParent "$cli_dir" "$cli_archive"
# notarytool can finish --wait with an Invalid verdict, so check the status
# instead of relying on the exit code alone, and print Apple's log on failure.
# A failed submit is not allowed to end the script before its output and the
# log are printed; the status check below fails the job instead.
submit_rc=0
result="$(xcrun notarytool submit "$cli_archive" --keychain-profile "$NOTARY_PROFILE" \
    --wait --output-format json)" || submit_rc=$?
echo "$result"
json_field() {
    printf '%s' "$result" | python3 -c '
import json, sys
try:
    value = json.load(sys.stdin).get(sys.argv[1], "")
except (ValueError, AttributeError):
    value = ""
print(value)' "$1"
}
status="$(json_field status)"
if [[ "$status" != Accepted || "$submit_rc" -ne 0 ]]; then
    submission="$(json_field id)"
    if [[ -n "$submission" ]]; then
        xcrun notarytool log "$submission" --keychain-profile "$NOTARY_PROFILE" || true
    fi
    echo "Notarization of command-line tools failed: ${status:-no status}" >&2
    exit 1
fi
