#!/usr/bin/env bash
set -euo pipefail

if [[ "$(uname -s)" != Darwin ]]; then
    echo "File Provider bundles can only be built on macOS." >&2
    exit 1
fi
mode="${1:---signed}"
if [[ "$mode" != --signed && "$mode" != --unsigned && "$mode" != --sign-existing ]]; then
    echo "Usage: $0 [--signed|--unsigned|--sign-existing]" >&2
    exit 1
fi
repo="$(cd "$(dirname "$0")/../.." && pwd)"
cd "$repo"

if [[ "$mode" != --unsigned ]]; then
    : "${TEAM_ID:?Set TEAM_ID to your Apple developer team identifier}"
    : "${SIGN_IDENTITY:?Set SIGN_IDENTITY to your Developer ID Application identity}"
fi
export FP_BUNDLE_ID="${FP_BUNDLE_ID:-io.github.egeozcan.mahpastes}"
export FP_APP_GROUP="${FP_APP_GROUP:-${TEAM_ID:-group}.io.github.egeozcan.mahpastes}"
export FP_KEYCHAIN_GROUP="${FP_KEYCHAIN_GROUP:-${TEAM_ID:-unsigned}.io.github.egeozcan.mahpastes.fileprovider}"
export FP_BUILD_DIR="$repo/build/file-provider"
mkdir -p "$FP_BUILD_DIR"

app="$repo/build/bin/mahpastes.app"
extension="$app/Contents/PlugIns/MahpastesFileProvider.appex"
if [[ "$mode" == --sign-existing ]]; then
    # The CI archive preserves the app bundle and executable permissions.
    # Signing only needs macOS tools; no Xcode project or compilation is involved.
    if [[ ! -x "$app/Contents/MacOS/mahpastes" || ! -x "$extension/Contents/MacOS/MahpastesFileProvider" ]]; then
        echo "Extract the unsigned CI archive into build/bin before signing." >&2
        exit 1
    fi
else
    command -v xcodegen >/dev/null
    command -v xcodebuild >/dev/null
    wails_bin="${WAILS_BIN:-$(go env GOPATH)/bin/wails}"
    # Bindings intentionally use the non-native implementation. Ordinary builds
    # never invoke Xcode or enable the fileprovider tag.
    "$wails_bin" generate module
    MACOSX_DEPLOYMENT_TARGET=13.0 "$wails_bin" build -platform darwin/universal -tags fileprovider -skipbindings
    xcodegen generate --spec native/macos/project.yml --project "$FP_BUILD_DIR"
    xcodebuild -project "$FP_BUILD_DIR/MahpastesFileProvider.xcodeproj" \
        -scheme MahpastesFileProvider -configuration Release \
        -derivedDataPath "$FP_BUILD_DIR/DerivedData" \
        ARCHS="arm64 x86_64" ONLY_ACTIVE_ARCH=NO CODE_SIGNING_ALLOWED=NO \
        "FP_BUNDLE_ID=$FP_BUNDLE_ID" "FP_APP_GROUP=$FP_APP_GROUP" \
        "FP_KEYCHAIN_GROUP=$FP_KEYCHAIN_GROUP" build

    mkdir -p "$app/Contents/PlugIns"
    rm -rf "$extension"
    ditto "$FP_BUILD_DIR/DerivedData/Build/Products/Release/MahpastesFileProvider.appex" "$extension"

fi

# Finalize both bundles and entitlements before signing any nested code.
python3 - <<'PY'
import os, pathlib, plistlib
root = pathlib.Path('build/bin/mahpastes.app')
bundle = os.environ['FP_BUNDLE_ID']
group = os.environ['FP_APP_GROUP']
keychain = os.environ['FP_KEYCHAIN_GROUP']
info = root / 'Contents/Info.plist'
with info.open('rb') as f:
    # Wails emits XML starting with DOCTYPE, without an XML declaration.
    # plistlib's format sniffing rejects that valid shape unless told XML.
    data = plistlib.load(f, fmt=plistlib.FMT_XML)
data.update(CFBundleIdentifier=bundle, MahpastesAppGroup=group,
            MahpastesKeychainGroup=keychain, LSMinimumSystemVersion='13.0')
with info.open('wb') as f:
    plistlib.dump(data, f)
extension_info = root / 'Contents/PlugIns/MahpastesFileProvider.appex/Contents/Info.plist'
with extension_info.open('rb') as f:
    extension_data = plistlib.load(f)
extension_data.update(CFBundleIdentifier=bundle + '.FileProvider',
                      MahpastesAppGroup=group, MahpastesKeychainGroup=keychain)
for key in ('CFBundleShortVersionString', 'CFBundleVersion'):
    if data.get(key):
        extension_data[key] = data[key]
with extension_info.open('wb') as f:
    plistlib.dump(extension_data, f)
team = os.environ.get('TEAM_ID')
for target in ('host', 'extension'):
    entitlements = {'com.apple.security.application-groups': [group],
                    'keychain-access-groups': [keychain]}
    if team:
        entitlements['com.apple.developer.team-identifier'] = team
        entitlements['com.apple.application-identifier'] = team + '.' + bundle + ('.FileProvider' if target == 'extension' else '')
    if target == 'extension':
        entitlements.update({'com.apple.security.app-sandbox': True,
                             'com.apple.security.network.client': True})
    with (pathlib.Path(os.environ['FP_BUILD_DIR']) / (target + '.entitlements')).open('wb') as f:
        plistlib.dump(entitlements, f)
PY

lipo "$app/Contents/MacOS/mahpastes" -verify_arch arm64 x86_64
lipo "$extension/Contents/MacOS/MahpastesFileProvider" -verify_arch arm64 x86_64
plutil -lint "$app/Contents/Info.plist" "$extension/Contents/Info.plist"

if [[ "$mode" != --unsigned ]]; then
    if [[ -n "${HOST_PROFILE_PATH:-}" ]]; then cp "$HOST_PROFILE_PATH" "$app/Contents/embedded.provisionprofile"; fi
    if [[ -n "${EXTENSION_PROFILE_PATH:-}" ]]; then cp "$EXTENSION_PROFILE_PATH" "$extension/Contents/embedded.provisionprofile"; fi
    codesign --force --options runtime --timestamp --sign "$SIGN_IDENTITY" \
        --entitlements "$FP_BUILD_DIR/extension.entitlements" "$extension"
    codesign --force --options runtime --timestamp --sign "$SIGN_IDENTITY" \
        --entitlements "$FP_BUILD_DIR/host.entitlements" "$app"
    codesign --verify --deep --strict --verbose=2 "$app"
    if [[ -n "${NOTARY_PROFILE:-}" ]]; then
        archive="$FP_BUILD_DIR/mahpastes-notarization.zip"
        ditto -c -k --keepParent "$app" "$archive"
        xcrun notarytool submit "$archive" --keychain-profile "$NOTARY_PROFILE" --wait
        xcrun stapler staple "$app"
        xcrun stapler validate "$app"
        spctl --assess --type execute --verbose=2 "$app"
    fi
else
    echo "Unsigned compile artifact created. A signed build is required for Finder registration."
fi
echo "$app"
