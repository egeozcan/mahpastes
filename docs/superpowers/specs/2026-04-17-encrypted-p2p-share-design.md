# Encrypted Peer-to-Peer Tag Sharing — Design

**Date:** 2026-04-17
**Status:** Proposed

## 1. Summary

A new "Share" feature that lets one mahpastes instance broadcast a tag over the internet to one or more other instances, end-to-end encrypted, with a single copy-pasteable share string as the capability. Forward-only: followers receive clips added after they start following, never historical content. Implemented with libp2p-go for P2P transport, identity, and NAT traversal.

## 2. Goals and non-goals

### Goals

- Broadcast a tag's new clips to any number of followers anywhere on the internet.
- End-to-end encryption between publisher and followers; eavesdroppers see only ciphertext.
- A single self-contained share string that encodes address + identity + key; no accounts, no servers to operate.
- Works across NATs by default (hole-punching with relay fallback).
- Followers autonomously decide where incoming clips are filed (local tag of their choice).
- Consistent with existing Serve / Watch UX patterns and design system.

### Non-goals (explicit)

- Historical sync. A new follower does not receive the tag's existing clips.
- Bidirectional sharing. 1:N publisher-to-follower only. Two-way use cases are expressed as two independent shares.
- Propagating edits, renames, or deletes of previously-pushed clips. The stream is append-only.
- Durable per-follower delivery tracking. If a follower is offline beyond the catch-up window, missed clips are permanently missed.
- Forward secrecy against future key leak. Followers have plaintext locally; that content is already "out".
- Writable shares, co-publishers, identity beyond peer-id, follower profiles, reactions, comments.

## 3. User-facing behavior

### 3.1 Share view

Accessible from the nav drawer via a new "Share" tab. The drawer keeps its current `w-64` width; the existing single-row tab strip becomes a 2×2 grid so Clips / Watch / Serve / Share fit without cramping the labels. An emerald indicator dot on the Share tab lights up whenever at least one publication or follow is active.

The Share view has two sections stacked vertically:

**Sharing** — one row per tag the user is currently broadcasting. Each row shows the tag name, stats (`N followers · M clips pushed · since <relative time>`), and two actions: `Copy link` and `Stop`. A dashed add-zone with `+ Share a Tag` opens the Create-Share modal.

**Following** — one row per share the user is subscribed to. Each row shows the user's chosen local tag name as the primary label, the connection status (`Connected`, `Connected (relayed)`, or `Offline · will resume`), counters (clips received, follow age), and an `Unfollow` action. A dashed add-zone with `+ Follow a Share` opens the Follow modal.

Received clips are not rendered inline in the Share view. They appear in the normal Clips view under the follower's chosen local tag, indistinguishable from any other clip once delivered.

### 3.2 Create-Share modal

User picks a tag from a `<select>` populated with their existing tags. Clicking `Create` calls `ShareService.StartShare(tagID)` and renders the resulting share string in a monospace box. Actions: `📋 Copy link`, `Show QR`, `Done`. A warning strip reads "Treat this like a password. It contains the decryption key."

`Show QR` swaps the text box for a client-side-rendered QR of the full string (via a small JS library; no backend round-trip needed). The QR is useful for phone-scans-desktop flows.

### 3.3 Follow modal

A `<textarea>` labeled "Paste share link". Client-side validation: the parser rejects strings that do not match `mp-share:v1:<base64url>` or whose decoded payload is malformed. A valid paste reveals a required `<input>` labeled "Assign incoming clips to local tag:". No pre-filled default — the publisher's tag name is deliberately not transmitted, so the follower must choose a name.

Clicking `Follow` calls `ShareService.Follow(shareString, localTagName)`. The backend creates the local tag if it does not exist, inserts a `follows` row, and dials the publisher in the background. On success the modal closes and a new card appears in the Following list. On failure (unreachable peer, invalid handshake), an inline error shows on the modal and no row is inserted.

### 3.4 Share-string format

```
mp-share:v1:<base64url(CBOR{peer_id, addrs, key})>
```

- `peer_id` — 32 raw bytes, the libp2p Ed25519 public key of this install.
- `addrs` — list of libp2p multiaddrs (direct addrs + relay addrs), populated by libp2p's Identify + AutoRelay.
- `key` — 32 random bytes, freshly generated per publication.

CBOR rather than raw concat because `addrs` is variable-length and versioning is cleaner. Base64url so the whole string is URL-safe and QR-friendly. Publisher's tag name is explicitly **not** included; followers choose a local tag name themselves.

Typical length: ~180–260 characters over three or four wrapped lines.

## 4. Architecture

### 4.1 Components

```
┌────────────────────────────────────────┐
│  Frontend (frontend/js/share.js)       │
│  Share view, modals, cards, events     │
└────────────┬───────────────────────────┘
             │ Wails binding
┌────────────▼───────────────────────────┐
│  ShareService (share_service.go)       │
│  StartShare / StopShare / Follow /     │
│  Unfollow / GetShareStatus             │
└────────────┬───────────────────────────┘
             │
┌────────────▼───────────────────────────┐
│  ShareManager (share_manager.go)       │
│  libp2p host · publications map ·      │
│  follows map · event dispatch          │
└───┬────────────┬──────────────┬────────┘
    │            │              │
    ▼            ▼              ▼
identity      protocol        codec
(load/save    (handshake,     (encode/decode
keypair)      envelope,       share strings)
              ring buffer)
```

Each concern lives in its own file so a unit can be read and reasoned about independently:

- `share_manager.go` — lifecycle, registries, wiring.
- `share_service.go` — Wails frontend API, no P2P logic.
- `share_codec.go` — share-string encode/decode, key-to-share-id derivation.
- `share_protocol.go` — stream handler, handshake, envelope framing, GCM encryption, ring buffer, per-follower queue.
- `share_identity.go` — load or generate the persistent libp2p keypair.

Frontend:

- `frontend/js/share.js` — Share view renderer, create/follow modals, share-string parser, Wails event listeners.
- New `<section id="share-view">` and two modal blocks in `frontend/index.html`.

A new Wails-bound service `ShareService` follows the existing pattern (`ServeService`, `APIService`, `ClipboardService`). `App` calls `ShareManager` methods during startup / shutdown / clip creation; `ShareService` forwards frontend calls to `ShareManager`.

### 4.2 Integration with existing code

- `app.go` `OnStartup`: after DB init, `ShareManager.Init()` (identity, libp2p host) then `ShareManager.ResumeAll()` (replay `shares` and `follows` tables).
- `app.go` `OnShutdown`: `ShareManager.StopAll()` before DB close.
- Clip creation paths (`UploadFiles`, `AddTextClip`, `AddFromURL`, `watcher.go` import): a single post-commit call `ShareManager.OnClipCreated(clipID, tagIDs)`. The manager decides whether any publication matches and enqueues envelopes.
- Tag deletion path (`DeleteTag`): calls `ShareManager.StopShare(tagID)` before DB delete so streams close cleanly.

## 5. Cryptography and trust

### 5.1 Keys

Per-publication symmetric key: 32 random bytes from `crypto/rand`. Used as AES-256-GCM key for every envelope of that publication. Also derives `share_id = SHA-256(key)[:16]`, the public identifier the follower sends during handshake.

Persistent libp2p Ed25519 identity per install: generated on first run, stored at `<data-dir>/share_identity.key` with 0600 permissions. Same identity used across all publications so existing share strings survive app restarts.

### 5.2 Handshake (follower → publisher, at stream open)

```
share_id    : 16 bytes    // SHA-256(symkey)[:16]
proof_nonce : 16 bytes    // random
proof_hmac  : 32 bytes    // HMAC-SHA256(symkey, "mp-share-v1-follow" || proof_nonce)
since_seq   : uint64      // 0 if new follower; else last received seq
```

Publisher:
1. Look up `share_id` in its `publications` map. Unknown → `Reset`.
2. Recompute `HMAC-SHA256(publication.symkey, "mp-share-v1-follow" || proof_nonce)`; compare in constant time. Mismatch → `Reset`.
3. If `since_seq > 0`, replay ring-buffer entries with `seq > since_seq` (bounded by 1h / 1000-entry cap) then go live. Else go live immediately.

### 5.3 Envelope format (publisher → follower, repeated)

```
┌──────────────┬────────────────┬────────────────────────┐
│ u32 length   │ 12B nonce      │ ciphertext || GCM tag  │
└──────────────┴────────────────┴────────────────────────┘
```

- `length` covers `nonce + ciphertext + tag`. Frames the envelope unambiguously.
- GCM associated data = `share_id || seq (u64 big-endian)`. Binds each envelope to its publication and position; defeats replay and reorder across or within publications.
- Plaintext = CBOR:
  ```
  {
    seq: uint64,
    ts:  int64,                     // unix millis, informational
    kind: "clip",                   // reserved; future kinds don't bump version
    clip: {
      filename:     string,
      content_type: string,
      data:         bytes,          // arbitrary binary; screenshots, PDFs, etc.
      metadata:     { key: value }  // clip metadata k/v pairs
    }
  }
  ```

### 5.4 Trust model

**Publisher does not trust followers.** Specifically:

- **Followers receive plaintext content.** Once decrypted, the clip is theirs — re-distribution, screenshots, export are unavoidable. This is inherent to any share.
- **Followers cannot write to the publisher.** The protocol is one-way; the publisher's stream handler only writes.
- **Followers cannot probe the publisher's libp2p host.** The host registers exactly one protocol, `/mahpastes/share/1.0.0`. Any stream opened on a different protocol is `Reset`. DHT, peer-exchange, and identify-beyond-the-minimum are disabled on the publisher.
- **Followers cannot access other shares.** The handshake's `share_id` + HMAC proof means a follower learns nothing about publications other than the one whose key they hold. They cannot enumerate `share_id`s.
- **Followers can re-share the capability.** The share string is ambient authority — by design, so the "give out a link" UX works. To revoke, the publisher `Stop`s the share; the key dies, and every copy of that string becomes dead everywhere.
- **Followers can attempt to DOS the publisher.** Per-peer rate limits and a max-concurrent-streams cap apply. Aggressive reconnect loops are rejected with backoff.

**Accepted leak:** on a direct (non-relayed) connection, the follower's peer learns the publisher's public IP and vice versa. libp2p's DCUtR hole-punching upgrades from relay to direct opportunistically. This matches magic-wormhole / Iroh default behavior. A "force-relay" toggle is a future enhancement.

**Out of scope:**

- Forward secrecy. See §2.
- Post-compromise security. If the symmetric key leaks, every past envelope captured by an eavesdropper becomes readable. Key rotation requires `Stop` + new share.
- Publisher-to-follower authorization beyond possession of the key. Anyone with the string is authorized.

## 6. Wire protocol details

### 6.1 Protocol ID

`/mahpastes/share/1.0.0` — a single libp2p stream handler on the publisher. Bump the major component (`/mahpastes/share/2.0.0`) on a backwards-incompatible change; publisher can register multiple protocol IDs in parallel during a transition.

### 6.2 Liveness

Stream liveness uses libp2p's Yamux / QUIC keepalives; no app-level pings. Abnormal close on the follower side triggers exponential-backoff reconnect (1s → 30s cap, full jitter).

### 6.3 Publisher-side ring buffer

Per publication, in memory only:

- Keyed by `seq`, entries are pre-encrypted envelopes ready to retransmit.
- Eviction: entries older than 1h OR beyond the 1000-entry cap, whichever comes first.
- On process restart the ring is empty; `last_seq` is persisted to `shares` so new envelopes never reuse a seq.

### 6.4 Per-follower write queue

Per connected follower, bounded to 64 envelopes. If a follower cannot drain the queue (slow network, stalled peer), the publisher closes the connection. The follower reconnects and catches up via the ring buffer; prevents one slow peer from stalling the publisher.

## 7. Data model

### 7.1 Schema additions

```sql
CREATE TABLE shares (
    id         INTEGER PRIMARY KEY AUTOINCREMENT,
    tag_id     INTEGER NOT NULL,
    symkey     BLOB    NOT NULL,                  -- 32 bytes
    share_id   BLOB    NOT NULL UNIQUE,           -- SHA-256(symkey)[:16]
    last_seq   INTEGER NOT NULL DEFAULT 0,        -- monotonic, persisted
    created_at INTEGER NOT NULL,
    FOREIGN KEY (tag_id) REFERENCES tags(id) ON DELETE CASCADE
);
CREATE UNIQUE INDEX idx_shares_tag_id ON shares(tag_id);

CREATE TABLE follows (
    id             INTEGER PRIMARY KEY AUTOINCREMENT,
    remote_peer_id TEXT    NOT NULL,              -- multihash string
    remote_addrs   TEXT    NOT NULL,              -- JSON array of multiaddrs
    symkey         BLOB    NOT NULL,              -- 32 bytes
    local_tag_id   INTEGER NOT NULL,
    last_seq       INTEGER NOT NULL DEFAULT 0,    -- highest seq accepted locally
    last_seen_at   INTEGER,
    created_at     INTEGER NOT NULL,
    FOREIGN KEY (local_tag_id) REFERENCES tags(id) ON DELETE RESTRICT
);
CREATE INDEX idx_follows_peer ON follows(remote_peer_id);
```

### 7.2 Design rationale

- **One publication per tag** — enforced by `idx_shares_tag_id`. Matches the UX (one row, one string, one Stop). Rotating the key means `Stop` + new share; the old row is replaced.
- **`symkey` cleartext at rest** — same threat model as `clips.data`. Anyone who can read the SQLite file can read every clip already. Adding an at-rest KMS without solving the broader "laptop stolen" problem is theater.
- **`ON DELETE CASCADE` on `shares.tag_id`** — deleting a tag drops its share. The tag-delete path also calls `StopShare` explicitly so streams close gracefully before the cascade fires.
- **`ON DELETE RESTRICT` on `follows.local_tag_id`** — deleting a tag that is actively receiving a follow is refused at the DB level. The user must `Unfollow` first. Silent content loss would be worse than a blocked delete.
- **`last_seq` on both sides, persisted** — publisher so seqs never repeat across restarts; follower so reconnects can ask for the precise catch-up window.
- **Identity file outside SQLite** — `share_identity.key` is the one thing a user might want to back up separately. It is included in backup ZIPs; restore into a fresh install regenerates rather than overwrites, to avoid two installs sharing one peer identity.

### 7.3 Backup/restore

`backup.go` already snapshots the SQLite DB; the new tables ride along automatically once the migration lands. The backup ZIP additionally includes `share_identity.key`. On restore:

- If the target install has no existing identity file, the backup's identity is loaded. Existing share strings continue to work.
- If the target has an identity already, the backup's identity is **not** loaded (to prevent two concurrent installs sharing one peer-id). Existing `shares` rows are loaded but will require `Stop` + re-share to produce valid strings under the target's identity. This is surfaced as a notice on the restore screen.

## 8. Lifecycle and error handling

### 8.1 Startup

1. DB open and migrations run.
2. `ShareManager.Init()`:
   - Load or generate `share_identity.key`.
   - Build the libp2p host: QUIC + TCP on random ports, Noise encryption, Yamux muxer, AutoRelay enabled (public libp2p relays by default), DCUtR enabled, single registered protocol handler.
3. `ShareManager.ResumeAll()`:
   - For each `shares` row, register an active publication (makes its handler ready to accept incoming follower streams).
   - For each `follows` row, schedule a background reconnect loop.

### 8.2 Shutdown

`ShareManager.StopAll()`:
- Close every follower stream on active publications.
- Cancel every follow reconnect loop and close in-flight streams.
- Close the libp2p host.
- Ring buffers discard.

### 8.3 Clip creation

`ShareManager.OnClipCreated(clipID, tagIDs)` is called once per new clip, post-commit:
1. Filter `tagIDs` against the publications map — skip if no match.
2. For each match: read clip row, build plaintext CBOR, encrypt with publication's key, assign next `seq`, persist `shares.last_seq`.
3. Append to the ring buffer; fan out to each connected follower's write queue.

### 8.4 Error modes and resolution

| Condition | Behavior |
|---|---|
| Publisher offline while follower connected | Follower stream closes, status flips to `Offline · will resume`, exponential backoff (1s → 30s) reconnect. |
| Strict NAT on either side, direct fails | libp2p falls back to circuit-relay; card shows `Connected (relayed)`. |
| Malformed share string | Client-side parser rejects before any backend call; inline modal error. |
| Valid string but publisher unreachable on first follow | Backend attempts connection with short timeout; on failure, returns an error; `follows` row is **not** inserted; inline modal error. |
| Publisher restart while follower connected | Follower stream closes, reconnects, handshakes with `since_seq = last_seq`. Ring buffer empty after restart so recent-gap catch-up is possible only if publisher had been up long enough — otherwise silent gap (accepted per spec). |
| Tag deleted on publisher | `StopShare` called explicitly, streams close, `ON DELETE CASCADE` drops the row, card disappears. |
| Tag deleted on follower | Blocked by `ON DELETE RESTRICT`; user is prompted to `Unfollow` first. |
| Disk full on follower during insert | Envelope is dropped; `last_seq` is **not** advanced. Next reconnect requests that seq; if still in publisher's ring, recovered; otherwise accepted loss. |
| Slow follower cannot drain write queue | Publisher closes that follower's connection; follower reconnects and catches up from ring. |
| Follower attempts unknown protocol | Stream `Reset`. |
| Follower sends bad handshake HMAC | Stream `Reset`. No distinguishing signal — same response as unknown `share_id`. |

### 8.5 Rate limits

Publisher-side:
- Max concurrent streams per publication: 128.
- Max concurrent streams from one peer: 4.
- Handshake timeout: 5s.
- Reconnect penalty: peer that reconnects ≥ 10× in 60s is backed off to 60s for the next 5 minutes.

## 9. Testing

### 9.1 End-to-end (`e2e/tests/share/`)

Tests use two parallel `AppHelper` instances paired by passing the publisher's share string into the follower.

- `share-create.spec.ts` — create share on A, string appears, Stop, string no longer accepted by B.
- `share-follow.spec.ts` — A shares tag, B follows into chosen local tag, A adds a binary (PNG) clip, B sees the clip under its local tag within a bounded time.
- `share-offline-catchup.spec.ts` — B disconnects mid-session, A adds a clip, B reconnects within the 1h window → clip delivered.
- `share-offline-dropped.spec.ts` — simulate gap beyond the ring (fast-forward or direct manipulation of the ring's eviction clock) → clip not delivered, no error surfaced.
- `share-persistence.spec.ts` — create share, restart A, string still works against B; follow, restart B, follow resumes.
- `share-ui.spec.ts` — sidebar 2×2 grid renders; Share tab activates; create/follow modals open, validate, close; cards update on Wails events.
- `share-trust.spec.ts` — attempt to open a stream on a different protocol ID → `Reset`; attempt handshake with wrong HMAC → `Reset`; attempt handshake with unknown `share_id` → `Reset`.

### 9.2 Go unit tests

- `share_codec_test.go` — roundtrip encode/decode, reject missing prefix, reject wrong version, reject malformed CBOR.
- `share_protocol_test.go` — handshake accepts valid HMAC, rejects mismatched HMAC and unknown `share_id`; envelope GCM roundtrip; seq ordering preserved; ring evicts at 1h and at capacity cap.
- `share_identity_test.go` — keypair persists across `Init` calls; regenerates if file missing or corrupt.
- `share_manager_test.go` — `ResumeAll` replays both tables; `OnClipCreated` fans out to matching publications only.

### 9.3 Baseline

Per CLAUDE.md, the existing `e2e` suite must pass before and after the change. New tests must be added for every new behavior.

## 10. Rollout considerations

- **Dependency weight:** libp2p-go adds ~15MB of compiled size and pulls a deep tree. Acceptable; documented in the PR.
- **Public relay reliance:** default config uses libp2p's public relay bootstrap list. No action needed from users on typical networks. Future knob: user-supplied relay list.
- **Platform parity:** libp2p-go is pure Go. Preserves the CGo-free-Windows invariant. No OS-specific code paths added by this feature.
- **Telemetry:** none. Follow the existing pattern: local-only UI surfaces connection status; no usage data leaves the device.

## 11. Open questions — resolved during brainstorming

- ~~LAN-only vs internet-wide?~~ → internet-wide with NAT traversal.
- ~~What content propagates?~~ → new clips only, append-only, forward from follow.
- ~~Offline behavior?~~ → 1h catch-up buffer, accepted loss beyond.
- ~~Writable shares?~~ → no; 1:N read-only.
- ~~Tag-name auto-assign on follow?~~ → follower types a local tag name; publisher's tag name is not transmitted.
- ~~Sidebar layout for 4 tabs?~~ → keep `w-64`, switch to 2×2 grid.
- ~~Share string format?~~ → `mp-share:v1:<base64url(CBOR payload)>`, no cleartext tag hint.
- ~~Create-Share dialog?~~ → text string by default, QR on demand.
- ~~P2P library?~~ → libp2p-go.

## 12. Future work (not in this spec)

- Force-relay toggle (hide publisher's IP from followers).
- User-supplied relay servers.
- Multiple publications per tag (e.g. separate strings for separate audiences).
- Revocation per-follower without global `Stop`.
- Writable shares / co-publishing.
- Historical sync.
- Plugin event hooks (`share:published`, `share:received`, `share:followed`, `share:unfollowed`) once the core is proven.
- CLI support (`mp share start/stop`, `mp share follow/unfollow`) — maps onto the REST API once `ShareService` is stable.
