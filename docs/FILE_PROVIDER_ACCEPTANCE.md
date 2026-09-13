# Signed File Provider acceptance

Runtime checks performed on 2026-09-12 using macOS 15.6.1 on Apple Silicon.
The tested universal bundle came from implementation commit `73408ea`; the
subsequent `ec36c6c` commit changes documentation only. The application and
extension were signed with Developer ID, provisioned, notarized, and stapled.
Gatekeeper and strict signature verification passed.

The test used a separate application installation and `MAHPASTES_DATA_DIR`.
The ordinary installed application and its data were not modified.

## Results

| Check | Result |
| --- | --- |
| Registration and shared credentials | Passed. Enabled the extension in macOS; Finder displayed Active and Archive. |
| Pagination | Passed. Enumerated 459 fixtures, exceeding the 200-item page size. |
| Downloads | Passed. Text, PNG, PDF, empty and 8 MiB binary contents matched source SHA-256 hashes. All 457 files visible after the first mutation tests also matched. |
| Live SQL mutations | Passed. Rename preserved UUID; content, archive, hidden descendant tags and expiry converged in Finder. |
| Public API mutations | Passed. PATCH rename, PUT content and PUT archive produced the correct Finder name, bytes and parent while preserving UUID. |
| Read-only filesystem | Passed. Create, overwrite, rename and delete returned EPERM. |
| Native editor | Passed. TextEdit opened the file as Locked and created an unsaved copy when editing was attempted. Provider contents remained unchanged. |
| Extension termination | Passed. Subsequent downloads succeeded with no loss of listings. |
| Host termination/relaunch | Passed. Cached content and listings remained available while stopped; fresh downloads succeeded after relaunch. |
| Interrupted read | Passed. Suspended the host during an uncached filesystem read, terminated the reader, and resumed the host. An immediate read returned Operation canceled; a later retry returned the correct bytes. This tests consumer interruption, not instrumentation of the Swift Progress callback. |
| Nonempty restore | Passed through the signed application's REST backup/restore endpoints. Finder converged to 458 visible files, with a new epoch and no reused old UUID filenames; restored content matched. |
| Empty restore | Passed through the same endpoints. Finder converged to empty; restoring the populated backup repopulated it. |
| Disable with downloaded data | Passed. Settings reported the recovery folder, which contained the downloaded fixture with unchanged bytes. |
| Re-enable | Passed. The location returned and its files could be read. |
| Signed upgrade | Passed. Replaced build 1.0.0 with signed/notarized/stapled build 1.0.1 through Finder. All 458 item names/UUIDs survived and an uncached file downloaded correctly. Implementation was identical; this exercises bundle-version replacement. |
| External removal / Retry | Passed. A separate signed native test helper removed the domain using NSFileProviderManager. Reopening Settings reported the missing location; Retry registered it again and downloads succeeded. |

## Scope and remaining release validation

The minimum supported macOS version, the latest macOS release, and Intel
hardware were not available for local runtime testing. CI builds the universal
bundle and tests ordinary macOS, Windows and Linux variants, but that is not a
substitute for the remaining native OS/hardware release matrix.

macOS denied an in-place terminal overwrite of the installed test bundle.
Replacing it through Finder succeeded after user approval. Signature and
staple validation passed on the installed replacement. The temporary REST
API credential was revoked and its server stopped when the test app restarted.

Plugin-specific UI paths and the native download-cancel button were not
separately exercised. Shared SQL mutation behavior and interrupted filesystem
reads were tested as described above. Keep the native variant opt-in; these
results do not authorize a public release or claim full platform coverage.
