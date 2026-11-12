# Builds from master and releases

Master builds and tagged releases run the same build, defined once in the
reusable workflow `.github/workflows/build.yml`:

- `master-builds.yml` runs it on every push to master and on manual start. Its
  downloadable Actions artifacts are retained for 14 days and identified by
  source commit. It does not publish GitHub Releases.
- `release.yml` runs it for every pushed `v*` tag, checks that every asset was
  produced, writes `SHA256SUMS` and publishes the assets to the GitHub Release
  for that tag (uploading into a release created beforehand, or creating one
  with generated notes). Started manually on a branch, it runs the whole
  pipeline, checksums included, and keeps the result as an Actions artifact
  without publishing anything.

Each run produces:

| Asset | Contents |
| --- | --- |
| `mahpastes-macos-universal.zip` | Signed, notarized and stapled `mahpastes.app` with Finder integration, universal `mp`, plugins, examples |
| `mahpastes-windows-amd64.zip`, `mahpastes-windows-arm64.zip` | `mahpastes.exe`, `mp.exe`, plugins, examples |
| `mahpastes-windows-amd64-installer.exe`, `mahpastes-windows-arm64-installer.exe` | NSIS installer (`build/windows/installer/project.nsi`) for `mahpastes.exe` and `mp.exe` |
| `mahpastes-linux-amd64.tar.gz` | `mahpastes`, `mp`, `install.sh`, `mahpastes.png`, plugins, examples |
| `mp-darwin-universal`, `mahpastesd-darwin-universal` | Signed and notarized universal command-line tools |
| `mp-<os>-<arch>`, `mahpastesd-<os>-<arch>` | Standalone tools for linux and windows, amd64 and arm64 (`.exe` on Windows) |
| `SHA256SUMS` | Checksums of all of the above (releases only) |
| `mahpastes.json`, `mahpastes.rb` | Scoop manifest and Homebrew cask rendered from `packaging/` with those checksums (releases only; see `packaging/README.md`) |

Every binary is stamped with `-X go-clipboard/internal/buildinfo.Version=<v>
-X go-clipboard/internal/buildinfo.Commit=<sha>`. A tag build uses the tag name;
other runs use `git describe --tags --always`. Go, Node and Wails versions are
pinned at the top of `build.yml` and match the other workflows and go.mod's
toolchain.

The Linux app is built on Ubuntu 24.04 and needs GTK 3, WebKitGTK 4.1 and glibc
2.39 or newer (Ubuntu 24.04, Debian 13, Fedora 40 and later). The Windows arm64
app is cross-compiled from the x64 runner, since WebView2 builds need no cgo.
Darwin `mahpastesd` uses cgo, so both darwin tools are built on the macOS
runner (two architectures joined with `lipo`); everything else standalone is
built with cgo disabled on Linux.

macOS includes Finder integration. Signing is mandatory: missing secrets fail the
macOS job rather than silently producing an unsigned download. On master,
Windows and Linux continue independently; a release needs every job, so a failed
macOS job publishes nothing. The macOS job submits the signed app to Apple, waits
for notarization, staples and validates the ticket, and checks Gatekeeper before
packaging. It then signs `mp` and `mahpastesd` with the same identity and
notarizes them in one more submission. Plain executables cannot hold a stapled
ticket, so Gatekeeper checks their notarization online the first time a
downloaded copy runs. A failed notarization or validation prevents uploading the
macOS artifact. Apple processing time is included in the 90-minute job timeout.

## One-time repository secrets

In Keychain Access, under My Certificates, export your Developer ID Application
identity (certificate and its private key) as a password-protected `.p12` file.
Do not commit the export or its password. Configure these GitHub Actions secrets:

| Secret | Value |
| --- | --- |
| MACOS_CERTIFICATE_P12 | Base64-encoded `.p12` export |
| MACOS_CERTIFICATE_PASSWORD | Export password |
| MACOS_HOST_PROFILE | Base64-encoded host provisioning profile |
| MACOS_EXTENSION_PROFILE | Base64-encoded File Provider provisioning profile |
| MACOS_TEAM_ID | Apple developer team ID |
| MACOS_SIGN_IDENTITY | Developer ID Application identity name or certificate SHA-1 |
| APPLE_ID | Apple account email used for notarization |
| APPLE_APP_SPECIFIC_PASSWORD | Apple app-specific password for notarization |

For file secrets, pipe the encoded bytes directly to GitHub CLI, for example:

```sh
base64 < /path/to/identity.p12 | gh secret set MACOS_CERTIFICATE_P12
base64 < /path/to/host.provisionprofile | gh secret set MACOS_HOST_PROFILE
base64 < /path/to/extension.provisionprofile | gh secret set MACOS_EXTENSION_PROFILE
gh secret set MACOS_CERTIFICATE_PASSWORD
gh secret set MACOS_TEAM_ID
gh secret set MACOS_SIGN_IDENTITY
gh secret set APPLE_ID
gh secret set APPLE_APP_SPECIFIC_PASSWORD
```

The signing job imports the identity into a temporary runner Keychain and removes
that Keychain and decoded files on exit. Both callers pass `secrets: inherit` to
the shared build; signing secrets are used only by these push, tag and manual
workflows, never the pull-request build workflow. The shared build only reads
repository contents; the release job alone gets permission to write releases.
