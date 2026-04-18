# Manual smoke test — encrypted P2P share (Phase 13)

This document is the hand-off for Step 4 of Task 13.1 in
`docs/superpowers/plans/2026-04-17-encrypted-p2p-share.md`. Automated
coverage (Go unit tests + the 16-spec e2e suite under
`e2e/tests/share/`) validates the happy path in a single-host context
but cannot reliably exercise real cross-machine networking. This
walkthrough covers what the reviewer needs to prove by hand before
merge.

Estimated time: **15–20 minutes** with two machines, or two user accounts
on one machine.

## Pre-requisites

- A branch build of mahpastes installed on BOTH machines. From this
  worktree:
  ```
  cd /Users/egecan/Code/mahpastes-share
  make build
  make install              # installs to /Applications
  ```
- Both machines must be on the same Wi-Fi / Ethernet subnet (mDNS does
  not cross routers). A corporate network with multicast blocked will
  need the WAN bootstrap path (see "Known limitations" below).
- No other mahpastes instances running — the feature's peer-ID is
  derived from `$AppData/share_identity.key`, so two instances using
  the same file will collide. Use separate user accounts or a fresh
  `MAHPASTES_DATA_DIR` override for the second instance.

## Smoke steps

### 1. Share drawer has the 2×2 tab grid

On machine A (the publisher): open mahpastes → sidebar drawer →
confirm the four tabs (`Clips`, `Watch`, `Serve`, `Share`) render in a
2×2 grid, not a flat row.

### 2. Create a share

On machine A:
1. Click the `Share` tab.
2. Click `+ Share a Tag`.
3. In the modal, select any existing tag from the dropdown (create
   one first if the dropdown is empty).
4. Click `Create Share`.
5. The result panel should show a share string of the form
   `mp-share:v1:<base64-url>`. Copy it.

Validation: the string MUST start with `mp-share:v1:` and the remainder
MUST be one line of URL-safe base64 (no newlines, no padding).

### 3. Follow the share on machine B

On machine B (the follower):
1. Open mahpastes → drawer → `Share` tab.
2. Click `+ Follow a Share`.
3. Paste the share string.
4. In the `Local tag` field, type a tag name to route incoming clips
   into (e.g. `inbox/from-alice`). The tag is auto-created if it does
   not yet exist.
5. Click `Follow`.

Validation:
- Within ~15 s the follow entry should flip from `connecting` to
  `connected`. On a quiet LAN it usually happens in 2–5 s.
- If it hangs at `connecting` for more than 30 s, mDNS discovery is
  failing — see "Known limitations" below.

### 4. Publish a clip and confirm delivery

On machine A:
1. Drag an image or any small file into the gallery.
2. Apply the shared tag to the new clip (if auto-tagging via Folder
   mode into the shared tag, the clip is tagged on upload).

On machine B (within 10 s):
1. Clips view should now contain a clip with the follower's local tag.
2. Open the clip — bytes must match what you uploaded on machine A.

Validation: exact byte match. If the follower shows a truncated or
zero-byte clip, the chunked-envelope reassembly path has broken.

### 5. Stop sharing and confirm follower flips offline

On machine A:
1. Share tab → find the share row for the tag → click `Stop`.
2. The share row should disappear from the publisher's list.

On machine B (within ~30 s):
1. The follow row should flip from `connected` to `offline`.
2. Any new clips uploaded and tagged on machine A after Stop should
   NOT arrive on machine B.

## Known limitations

**mDNS is the default LAN discovery mechanism.** On networks that block
multicast (hotel Wi-Fi, some corporate networks, some VLANs) mDNS will
silently fail and the follow will hang at `connecting`. The WAN
fallback is seeded DHT bootstrap peers (libp2p's public bootstrap
nodes) — this is enabled by default in production builds. It is
disabled in e2e tests via `MAHPASTES_SHARE_DISABLE_WAN_BOOTSTRAP=1`.
If you're testing on a network that blocks multicast, WAN bootstrap
picks up within 30–60 s after app start.

**Single-NIC on same host.** You can run two mahpastes instances on
one machine by setting `MAHPASTES_DATA_DIR` to different directories
in each launch. mDNS will bind to loopback and both instances will
find each other via the `mahpastes-share` service tag.

**Identity file lives in app data.** `$AppData/share_identity.key` is
a 32-byte ed25519 private key. If you restore from a backup created
on another machine, the identity-takeover dialog (see Task 11.2)
lets you choose to adopt the backup's identity, keep the local one
(marking restored shares as invalid), or cancel.

**Latent issue to watch for.** A clip uploaded with an auto-tag
pointing at a shared tag currently fires `OnClipCreated` twice (once
from the upload-path hook, once from the `AddTagToClip` hook). The
follower would receive the clip content twice (two envelopes with
different seq numbers but identical chunks). No automated test
currently exercises this path. If you hit it during smoke, file a
`fix(share):` follow-up.

## Rollback

If any of the smoke steps fail and you want to revert the installed
build:

```
make uninstall
git checkout master
make build
make install
```

Then re-launch and confirm the Share tab is gone from the sidebar —
that signals the master-branch build is installed.

## Reporting

If smoke passes: add a note to the Phase 13 PR description stating
which machines / network were used and that all five steps succeeded.

If smoke fails: capture the publisher's console (from `~/go/bin/wails
dev` or mahpastes log) around the failing step, file a `fix(share):`
issue, and hold the merge.
