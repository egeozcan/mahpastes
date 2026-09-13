#!/usr/bin/env bash
set -euo pipefail

# This imports a private signing identity only on an ephemeral GitHub runner.
[[ "${GITHUB_ACTIONS:-}" == true && "${RUNNER_OS:-}" == macOS ]] || {
    echo "This script requires a GitHub Actions macOS runner." >&2
    exit 1
}
for name in MACOS_CERTIFICATE_P12 MACOS_CERTIFICATE_PASSWORD MACOS_HOST_PROFILE MACOS_EXTENSION_PROFILE TEAM_ID SIGN_IDENTITY; do
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
bash scripts/macos/build-file-provider.sh --sign-existing
