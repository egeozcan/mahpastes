# Builds from master

The Master builds workflow runs on every push to master and can also be started
manually. Its downloadable Actions artifacts contain Windows amd64, Linux amd64,
and signed, notarized universal macOS desktop apps, plus bundled plugins and examples.
Artifacts are retained for 14 days and identified by source commit. This workflow
does not publish GitHub Releases. Linux requires GTK 3 and WebKitGTK 4.1.

macOS includes Finder integration. Signing is mandatory: missing secrets fail the
macOS job rather than silently producing an unsigned download. Windows and Linux
continue independently. The macOS job submits each signed app to Apple, waits
for notarization, staples and validates the ticket, and checks Gatekeeper before
packaging. A failed notarization or validation prevents uploading the macOS
artifact. Apple processing time is included in the 90-minute job timeout.

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
that Keychain and decoded files on exit. Signing secrets are used only by this
push/manual workflow, never the pull-request build workflow.
