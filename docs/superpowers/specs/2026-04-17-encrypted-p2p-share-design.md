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
- Propagating edits, renames, metadata changes, or deletes of previously-pushed clips. Each follower receives a point-in-time snapshot of the clip as it existed at first send; the stream is append-only.
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
mp-share:v1:<base64url(CBOR{peer_id, key})>
```

- `peer_id` — 32 raw bytes, the libp2p Ed25519 public key of this install. Stable across app restarts.
- `key` — 32 random bytes, freshly generated per publication.

No addresses in the string. The string contains only stable fields so a copied link survives publisher restarts, network changes, and relay reservation churn. Addresses are resolved at follow-time via libp2p's DHT; see §5.5.

CBOR (rather than raw concat) gives us cheap extensibility: future versions can add optional fields without changing the prefix. Base64url so the whole string is URL-safe and QR-friendly. Publisher's tag name is explicitly **not** included; followers choose a local tag name themselves.

Typical length: ~110–130 characters — a single line.

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

- `length` covers `nonce + ciphertext + tag`. Frames the envelope unambiguously. Hard cap: `length ≤ 1 MiB + 64 B overhead`.
- GCM associated data = `share_id || seq (u64 big-endian)`. Binds each envelope to its publication and position; defeats replay and reorder across or within publications.

**Chunked clip transport.** Clips of arbitrary size (screenshots, PDFs, multi-MB binaries) are transmitted as one or more envelopes so that memory cost is bounded. Each envelope's plaintext is one of three CBOR-tagged messages:

```
// 1. Per-clip header — first envelope of a clip.
{
  seq:      uint64,
  ts:       int64,                 // unix millis, informational
  kind:     "clip_start",
  clip_id:  uint64,                // opaque, unique within this stream session
  filename: string,
  content_type: string,
  metadata: { key: value },
  total_size: uint64,              // total plaintext byte count
  chunk_count: uint32              // N, number of clip_chunk envelopes that follow
}

// 2. Body chunk — each envelope carries up to 1 MiB of raw bytes.
{
  seq:     uint64,
  kind:    "clip_chunk",
  clip_id: uint64,
  index:   uint32,                 // 0..chunk_count-1
  data:    bytes                   // at most 1 MiB
}

// 3. Terminator — signals the clip is complete.
{
  seq:     uint64,
  kind:    "clip_end",
  clip_id: uint64,
  sha256:  bytes                   // 32 bytes, SHA-256 of the reassembled plaintext
}
```

Every envelope has its own monotonic `seq` so the ring / catch-up protocol treats chunks and clips uniformly. A clip under 1 MiB compiles to exactly three envelopes (`clip_start` + one `clip_chunk` + `clip_end`); for very small clips the start+chunk can be merged into a single envelope of kind `clip_inline` (body under ~900 KB fits in one envelope including metadata) as an optimization — this is a v1.1 concern, spec defers it.

`kind` is a versioned discriminator: future message types (heartbeats, key-rotation hints, etc.) add new kinds without bumping the protocol major.

**Follower-side assembly.** On `clip_start` the follower opens a temp file under `<data-dir>/share-staging/<stream-session>-<clip_id>`, appends each `clip_chunk.data` as envelopes arrive, and on `clip_end` validates the SHA-256 and atomically inserts the clip into SQLite with the configured local tag. Temp-file approach means arbitrarily large clips land without resident-memory pressure. A stream session abandoned mid-clip leaves its staging file on disk; a janitor on startup deletes any `share-staging/*` older than 24h. On `clip_chunk` arriving out of order for the same `clip_id` the stream is considered corrupt (libp2p streams are in-order within a session, so this indicates a bug) and the follower resets and reconnects.

### 5.4 Trust model

**Publisher does not trust followers.** Specifically:

- **Followers receive plaintext content.** Once decrypted, the clip is theirs — re-distribution, screenshots, export are unavoidable. This is inherent to any share.
- **Followers cannot write to the publisher.** The protocol is one-way; the publisher's stream handler only writes.
- **Followers cannot probe the publisher's publication state via libp2p services.** The host exposes only the baseline libp2p stack — Noise, Yamux, QUIC, Identify, Kademlia DHT (auto-server), circuit-relay v2 stop, DCUtR — plus exactly one application-level protocol, `/mahpastes/share/1.0.0`. Any stream opened on a different *application* protocol is `Reset`. The baseline services reveal only what is inherent to being a reachable peer (peer-id, listening addrs, supported protocol list, agent version); they do not reveal the existence, count, or `share_id` of any publication. `AgentVersion` is set to a neutral `mahpastes` string rather than install-specific metadata; no usage counters, user-agent fingerprints, or app-state leaks beyond that.
- **Followers cannot access other shares.** The handshake's `share_id` + HMAC proof means a follower learns nothing about publications other than the one whose key they hold. They cannot enumerate `share_id`s — the publisher reveals nothing about which `share_id`s exist until one is proved via HMAC.
- **Followers can re-share the capability.** The share string is ambient authority — by design, so the "give out a link" UX works. To revoke, the publisher `Stop`s the share; the key dies, and every copy of that string becomes dead everywhere.
- **Followers can attempt to DOS the publisher.** Per-peer rate limits and a max-concurrent-streams cap apply. Aggressive reconnect loops are rejected with backoff.

**Accepted leak:** on a direct (non-relayed) connection, the follower's peer learns the publisher's public IP and vice versa. libp2p's DCUtR hole-punching upgrades from relay to direct opportunistically. This matches magic-wormhole / Iroh default behavior. A "force-relay" toggle is a future enhancement.

**Out of scope:**

- Forward secrecy. See §2.
- Post-compromise security. If the symmetric key leaks, every past envelope captured by an eavesdropper becomes readable. Key rotation requires `Stop` + new share.
- Publisher-to-follower authorization beyond possession of the key. Anyone with the string is authorized.

### 5.5 Peer discovery

The share string deliberately omits addresses so that copied links survive restarts, IP changes, and relay reservation churn. Address resolution happens at follow-time:

1. **DHT bootstrap.** On startup, both roles connect to the public libp2p bootstrap peers (the standard `libp2p.ChainOptions` default list, ~6 nodes) and join the Kademlia DHT. Publisher runs in `dht.ModeAutoServer`; follower runs in `dht.ModeClient`.
2. **Publisher reachability.** Publisher enables AutoRelay, which reserves circuit-relay v2 slots on public relays and advertises the resulting `/p2p-circuit` multiaddrs via libp2p Identify. When a DHT peer queries the publisher's peer record, it gets the current relay and direct addresses.
3. **Follower resolution.** On follow (initial and every reconnect), the follower calls `host.Peerstore()` first (fast path for cached addrs); on miss or all-stale, calls `dht.FindPeer(ctx, peerID)` with a 10s timeout. The returned `AddrInfo` is stored in peerstore with a short TTL and dialed.
4. **Upgrade.** If the initial dial lands through a relay, libp2p's DCUtR protocol attempts to upgrade to a direct connection in the background. The UI reflects this as `Connected (relayed)` → `Connected` when upgrade succeeds.

**Listener ports** are still random per start (no firewall assumption), which is fine because the DHT record is what followers consult. **Relay reservations** are ephemeral, but the publisher's DHT record is refreshed as addrs change.

**If the DHT or all bootstrap peers are unreachable** (hostile network): follow fails with a clear error. v1 does not ship a fallback. A future knob can let users configure custom bootstrap peers or a private relay.

## 6. Wire protocol details

### 6.1 Protocol ID

`/mahpastes/share/1.0.0` — a single libp2p stream handler on the publisher. Bump the major component (`/mahpastes/share/2.0.0`) on a backwards-incompatible change; publisher can register multiple protocol IDs in parallel during a transition.

### 6.2 Liveness

Stream liveness uses libp2p's Yamux / QUIC keepalives; no app-level pings. Abnormal close on the follower side triggers exponential-backoff reconnect (1s → 30s cap, full jitter).

### 6.3 Publisher-side catch-up ring

The ring persists envelope **ciphertext** — not metadata references — to a new SQLite table `share_ring`. Two concrete reasons storing raw ciphertext is necessary, not optional:

1. **Clips are mutable.** The existing app flows permit content replacement ([app.go:837](app.go:837)), rename ([app.go:917](app.go:917)), and metadata edits ([app.go:2751](app.go:2751)). A ring that stored only a reference and re-read + re-encrypted at replay time would either re-send stale state as if it were live, or — worse — reuse the original GCM nonce against a mutated plaintext, which breaks AES-GCM's confidentiality guarantee.
2. **Replay from mid-clip must be complete.** Large clips span multiple envelopes. A follower that disconnects mid-clip resumes with `since_seq` pointing inside the chunk sequence. Regenerating the final `clip_end` envelope requires the original SHA-256 of the plaintext-at-first-send; a metadata-only ring cannot reconstruct this if the source clip has been edited.

**Table rows** (full schema in §7.1): `{id, publication_id, seq, kind, envelope_bytes, ts}`. `envelope_bytes` is the complete on-wire envelope — `nonce || ciphertext || GCM tag` — ready to be written to a follower's stream. `kind` is informational, used for stats and debugging; it does not affect replay logic.

**Snapshot semantics.** Once envelopes for a clip are committed to the ring, subsequent mutations of the source clip (edit, rename, metadata update, delete) do **not** trigger new envelopes and do **not** affect retransmit. Each follower receives the exact plaintext the publisher observed at first-send time. This satisfies the §2 non-goal that edits do not propagate, and is what makes the ring immune to the nonce-reuse hazard.

**Eviction.** Publisher deletes old rows on each write:

```sql
DELETE FROM share_ring
 WHERE publication_id = ?
   AND (ts < ? - 3600                              -- > 1h old
        OR (SELECT SUM(LENGTH(envelope_bytes))
              FROM share_ring
             WHERE publication_id = ?) > 268435456  -- > 256 MiB per publication
            AND seq = (SELECT MIN(seq) FROM share_ring WHERE publication_id = ?));
```

Age eviction (1h TTL) is the primary guarantee and matches the offline-catch-up spec in §2. The byte-cap (256 MiB per publication) is a runaway-disk safeguard; over it, oldest rows evict first.

**Retransmit path.** On handshake with `since_seq > 0`:

```sql
SELECT envelope_bytes FROM share_ring
 WHERE publication_id = ?
   AND seq > ?
 ORDER BY seq;
```

Stream each row's bytes to the follower as-is. No decryption, no re-encryption, no reads against the live `clips` table. If some requested seq has already been evicted (1h window passed), the result set starts at a higher seq than `since_seq + 1`; the follower observes the seq jump and accepts the gap. If no rows match at all, the publisher sends nothing on the retransmit path and goes straight to live; the follower's local `last_seq` advances only as new envelopes arrive.

**Bonus: restart-safe catch-up.** Because the ring is on disk, it survives publisher crashes/restarts within the 1h window. A follower reconnecting after a brief publisher bounce can still receive the envelopes it missed, provided the restart happened within the TTL. This is a strict improvement over the previous in-memory spec and costs nothing.

**Storage cost.** At the 256 MiB cap, a publisher running many high-throughput shares could store several hundred MiB of transient ciphertext. Because the DB is on the same disk as `clips.data` blobs already, this is proportional to the data being shared anyway — not a surprising multiplier.

### 6.4 Per-follower send scheduler

Per connected follower, the publisher runs a send scheduler with two bounds:

- **Byte cap:** 32 MiB of envelopes pending write to this peer's stream. If adding the next envelope would exceed, the stream is closed; the follower reconnects and resumes via the ring.
- **Envelope cap:** 256 envelopes queued. Primarily a defense against pathological chunk-spam from a buggy publisher; the byte cap is the load-bearing limit.

Because individual envelopes are capped at ~1 MiB, one slow follower can hold at most ~32 MiB of memory hostage before the publisher sheds it. Live envelopes are read once from `share_ring` (on-disk), paged into RAM only while sitting in send queues, then dropped as each is written to the stream. Retransmit re-reads from `share_ring` directly. End-to-end, a publisher serving K followers has peak RAM ≈ `K × 32 MiB` plus one in-flight chunk per active clip emission; the catch-up ring itself lives on disk.

## 7. Data model

### 7.1 Schema additions

```sql
CREATE TABLE shares (
    id         INTEGER PRIMARY KEY AUTOINCREMENT,
    tag_id     INTEGER NOT NULL,
    symkey     BLOB    NOT NULL,                  -- 32 bytes
    share_id   BLOB    NOT NULL UNIQUE,           -- SHA-256(symkey)[:16]
    last_seq   INTEGER NOT NULL DEFAULT 0,        -- monotonic, persisted
    status     TEXT    NOT NULL DEFAULT 'active', -- 'active' | 'invalid' (see §7.3)
    created_at INTEGER NOT NULL,
    FOREIGN KEY (tag_id) REFERENCES tags(id) ON DELETE CASCADE
);
CREATE UNIQUE INDEX idx_shares_tag_id ON shares(tag_id);

CREATE TABLE follows (
    id             INTEGER PRIMARY KEY AUTOINCREMENT,
    remote_peer_id TEXT    NOT NULL,              -- multihash string
    symkey         BLOB    NOT NULL,              -- 32 bytes
    local_tag_id   INTEGER NOT NULL,
    last_seq       INTEGER NOT NULL DEFAULT 0,    -- highest seq accepted locally
    last_seen_at   INTEGER,
    created_at     INTEGER NOT NULL,
    FOREIGN KEY (local_tag_id) REFERENCES tags(id) ON DELETE RESTRICT
);
CREATE INDEX idx_follows_peer ON follows(remote_peer_id);

CREATE TABLE share_ring (
    id             INTEGER PRIMARY KEY AUTOINCREMENT,
    publication_id INTEGER NOT NULL,              -- FK → shares.id
    seq            INTEGER NOT NULL,              -- envelope seq within this publication
    kind           TEXT    NOT NULL,              -- 'clip_start' | 'clip_chunk' | 'clip_end' | 'gap'
    envelope_bytes BLOB    NOT NULL,              -- full on-wire envelope: nonce || ciphertext || tag
    ts             INTEGER NOT NULL,              -- unix seconds, for 1h TTL eviction
    FOREIGN KEY (publication_id) REFERENCES shares(id) ON DELETE CASCADE
);
CREATE UNIQUE INDEX idx_share_ring_pub_seq ON share_ring(publication_id, seq);
CREATE INDEX idx_share_ring_ts ON share_ring(ts);
```

### 7.2 Design rationale

- **One publication per tag** — enforced by `idx_shares_tag_id`. Matches the UX (one row, one string, one Stop). Rotating the key means `Stop` + new share; the old row is replaced.
- **`symkey` cleartext at rest** — same threat model as `clips.data`. Anyone who can read the SQLite file can read every clip already. Adding an at-rest KMS without solving the broader "laptop stolen" problem is theater.
- **`ON DELETE CASCADE` on `shares.tag_id`** — deleting a tag drops its share. The tag-delete path also calls `StopShare` explicitly so streams close gracefully before the cascade fires.
- **`ON DELETE CASCADE` on `share_ring.publication_id`** — stopping or deleting a publication drops its ciphertext ring in the same transaction, freeing disk immediately.
- **`ON DELETE RESTRICT` on `follows.local_tag_id`** — deleting a tag that is actively receiving a follow is refused at the DB level. The user must `Unfollow` first. Silent content loss would be worse than a blocked delete.
- **`last_seq` on both sides, persisted** — publisher so seqs never repeat across restarts; follower so reconnects can ask for the precise catch-up window.
- **Identity file outside SQLite** — `share_identity.key` is the one thing a user might want to back up separately. It is included in backup ZIPs; restore into a fresh install regenerates rather than overwrites, to avoid two installs sharing one peer identity.

### 7.3 Backup/restore

`backup.go` already snapshots the SQLite DB; the new tables ride along automatically once the migration lands. The backup ZIP additionally includes `share_identity.key`. Restore is a migration-level event, so identity handling must be an explicit user choice rather than a silent policy — otherwise a user restoring onto a machine where mahpastes already ran once gets silently broken shares.

**Restore dialog (UI flow):**

After the user selects a backup ZIP, and before any write, the restore screen detects whether the target install already has `share_identity.key`. If yes, it prompts with three options:

1. **Take over — use the backup's identity.** Existing share strings continue to work on this install. Any publications previously started on *this* install become orphaned (their peer-id goes stale immediately); a warning lists them by tag name so the user is not surprised. `share_identity.key` is overwritten with the backup's copy. **Recommended when migrating to a new machine.**
2. **Keep this install's identity.** The backup's identity file is discarded. Restored `shares` rows are still inserted (for bookkeeping) but flagged `invalid`; cards display a "Re-share needed" state with a one-click `Stop + Share` action. **Recommended when merging a backup into an active second install.**
3. **Cancel restore.** No changes.

If the target install has no existing identity file (first run since install, or key never generated because no share was ever used), the backup's identity is loaded without prompting — there is nothing to lose. The restore screen surfaces this as an informational notice: "Identity restored from backup."

**Schema:** `shares.status TEXT NOT NULL DEFAULT 'active'` is added to support the `invalid` state. Only `'active'` publications are registered with the `ShareManager`; `'invalid'` ones appear in the UI list with a distinct visual and no operational streams.

## 8. Lifecycle and error handling

### 8.1 Startup

1. DB open and migrations run.
2. `ShareManager.Init()`:
   - Load or generate `share_identity.key`.
   - Build the libp2p host: QUIC + TCP on random ports, Noise encryption, Yamux muxer, AutoRelay enabled (public libp2p relays by default), DCUtR enabled, single registered protocol handler.
   - Bootstrap the Kademlia DHT against the public libp2p bootstrap peers; publisher mode is `AutoServer`, follower-only instances run `Client`. Wait up to 10s for the bootstrap to stabilize before `ResumeAll`, but don't block indefinitely — lazy bootstrap is fine for UX.
3. `ShareManager.ResumeAll()`:
   - For each `shares` row with `status = 'active'`, register a publication (stream handler ready for incoming followers).
   - For each `shares` row with `status = 'invalid'`, register it in the UI list only — no handler registered, no streams served.
   - For each `follows` row, schedule a background reconnect loop (dials via DHT `FindPeer(peerID)` → `AddrInfo`).
4. `shareStagingJanitor()` starts: walks `<data-dir>/share-staging/` and removes any file older than 24h. Runs once at startup, then every 6h.

### 8.2 Shutdown

`ShareManager.StopAll()`:
- Close every follower stream on active publications.
- Cancel every follow reconnect loop and close in-flight streams.
- Close the libp2p host.
- Ring buffers discard.

### 8.3 Clip creation

`ShareManager.OnClipCreated(clipID, tagIDs)` is called once per new clip, post-commit:

1. Filter `tagIDs` against active publications (`status = 'active'`) — skip if no match.
2. For each matching publication, open a single DB transaction covering the full `clip_start..clip_end` burst:
   - Read the clip's metadata and blob size from SQLite. Stream the blob in 1 MiB slices (`SUBSTR(clips.data, offset+1, 1048576)`) — never load the whole blob into memory.
   - Compute the plaintext SHA-256 incrementally while streaming, for use in the final `clip_end`.
   - For each envelope (`clip_start`, N × `clip_chunk`, `clip_end`): allocate the next `seq`, generate a fresh random 12-byte nonce, build AAD as `share_id || seq`, AES-256-GCM encrypt, and `INSERT INTO share_ring(publication_id, seq, kind, envelope_bytes, ts)`.
   - Update `shares.last_seq` once at the end of the burst.
   - Run ring eviction (age + byte-cap) for this publication.
3. Commit the transaction. After commit, enqueue the envelopes to every connected follower's send scheduler (live fan-out). New envelopes always flow through `share_ring` first, so live and catch-up paths are serving from the same authoritative store.

DB write amplification: one transaction per clip, envelope-count INSERTs per clip, one UPDATE for `last_seq`, and a capped eviction DELETE. Since clip creation is already a DB write, the incremental cost is additive DML on small rows.

### 8.4 Error modes and resolution

| Condition | Behavior |
|---|---|
| Publisher offline while follower connected | Follower stream closes, status flips to `Offline · will resume`, exponential backoff (1s → 30s) reconnect. |
| Strict NAT on either side, direct fails | libp2p falls back to circuit-relay; card shows `Connected (relayed)`. |
| Malformed share string | Client-side parser rejects before any backend call; inline modal error. |
| Valid string but publisher unreachable on first follow | Backend attempts connection with short timeout; on failure, returns an error; `follows` row is **not** inserted; inline modal error. |
| Publisher restart while follower connected | Follower stream closes, reconnects. Follower queries DHT for the publisher's current addresses, redials, handshakes with `since_seq = last_seq`. `share_ring` is on disk so catch-up within the 1h window works after restart; beyond it, silent gap (accepted per spec). |
| Publisher address change (new network, relay reservation expired) | DHT record updates automatically as addrs change. Follower's next reconnect does a fresh `FindPeer` and dials the new addrs. |
| DHT unreachable / hostile network | Follow attempt fails with error `DHT lookup failed — check network connectivity`. Follow row is not inserted (for first follow) or follow remains in `Offline · will resume` with backoff (for existing follows). |
| Strict NAT on either side, direct fails | libp2p falls back to circuit-relay; card shows `Connected (relayed)`. DCUtR attempts upgrade in the background. |
| Malformed share string | Client-side parser rejects before any backend call; inline modal error. |
| Valid string but publisher unreachable on first follow | Backend attempts DHT `FindPeer` + dial with short timeout; on failure, returns an error; `follows` row is **not** inserted; inline modal error. |
| Tag deleted on publisher | `StopShare` called explicitly, streams close, `ON DELETE CASCADE` drops the row, card disappears. |
| Source clip edited, renamed, or metadata-changed after first send | No propagation (per §2 non-goal). Retransmit serves the ciphertext captured at first send; followers receive a point-in-time snapshot. |
| Source clip deleted between send and retransmit | Retransmit is unaffected — ciphertext is self-contained in `share_ring`. |
| Tag deleted on follower | Blocked by `ON DELETE RESTRICT`; user is prompted to `Unfollow` first. |
| Disk full on follower during `clip_chunk` staging write | Staging write returns error; follower drops the in-flight clip without advancing `last_seq` past `clip_start`, resets stream. Next reconnect retries from `clip_start.seq`; if still in ring, recovered; otherwise accepted loss. |
| Staging temp file left behind (crash mid-clip) | Janitor at startup + every 6h removes `share-staging/*` older than 24h. |
| Malformed chunk order or checksum mismatch on `clip_end` | Follower aborts the clip, deletes the staging file, resets the stream, requests retransmit from `clip_start.seq` on reconnect. |
| Slow follower exceeds 32 MiB send-queue byte cap | Publisher closes that follower's connection; follower reconnects and catches up from ring. |
| Follower attempts unknown protocol | Stream `Reset`. |
| Follower sends bad handshake HMAC or unknown `share_id` | Stream `Reset`. Indistinguishable responses — no enumeration signal. |
| Restored backup with invalidated shares | `shares.status = 'invalid'` rows render with "Re-share needed" CTA; no handler registered until user re-shares. |

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
- `share-large-clip.spec.ts` — A pushes a multi-MB binary (e.g. 8 MiB image), B reassembles it end-to-end with matching SHA-256; staging file cleaned up on success.
- `share-offline-catchup.spec.ts` — B disconnects mid-session, A adds a clip, B reconnects within the 1h window → clip delivered.
- `share-offline-dropped.spec.ts` — simulate gap beyond the ring (fast-forward or direct manipulation of the ring's eviction clock) → clip not delivered, no error surfaced.
- `share-snapshot.spec.ts` — A pushes clip, B goes offline, A deletes the clip (or renames it, or edits its content), B reconnects within the 1h window — B receives the original point-in-time content unchanged.
- `share-publisher-restart.spec.ts` — A pushes clip, B goes offline, A restarts, B reconnects within 1h — clip delivered from persisted `share_ring`.
- `share-address-change.spec.ts` — A shares, B follows, A restarts on a different port / address → B's reconnect loop re-resolves via DHT and resumes.
- `share-persistence.spec.ts` — create share, restart A, string still works against B; follow, restart B, follow resumes.
- `share-restore.spec.ts` — restore backup over an install with existing identity: prompt shown; `Take over` recovers shares; `Keep` marks them `invalid` with Re-share CTA.
- `share-ui.spec.ts` — sidebar 2×2 grid renders; Share tab activates; create/follow modals open, validate, close; cards update on Wails events.
- `share-trust.spec.ts` — attempt to open a stream on a different protocol ID → `Reset`; attempt handshake with wrong HMAC → `Reset`; attempt handshake with unknown `share_id` → `Reset`.

### 9.2 Go unit tests

- `share_codec_test.go` — roundtrip encode/decode, reject missing prefix, reject wrong version, reject malformed CBOR. Asserts encoded length ≤ 140 chars.
- `share_protocol_test.go` — handshake accepts valid HMAC, rejects mismatched HMAC and unknown `share_id`; envelope GCM roundtrip; chunked clip assembly (start / N chunks / end) with SHA-256 verification; seq ordering preserved; `share_ring` evicts at 1h TTL and at 256 MiB per-publication byte cap; retransmit streams row ciphertext without decrypting; ring survives a simulated restart (close + reopen DB, catch-up still works); mutating the source clip's content/filename/metadata after send leaves replayed envelopes unchanged.
- `share_identity_test.go` — keypair persists across `Init` calls; regenerates if file missing; corrupt file returns a typed error rather than panicking.
- `share_manager_test.go` — `ResumeAll` replays `active` rows only; `invalid` rows are surfaced without handler registration; `OnClipCreated` fans out to matching publications only; chunk emission for a 3 MiB clip produces 5 envelopes (`clip_start` + 3 × `clip_chunk` + `clip_end`); per-follower scheduler sheds connection when byte cap exceeded.

### 9.3 Baseline

Per CLAUDE.md, the existing `e2e` suite must pass before and after the change. New tests must be added for every new behavior.

## 10. Rollout considerations

- **Dependency weight:** libp2p-go adds ~15MB of compiled size and pulls a deep tree (including a CBOR library, a QUIC stack, and the Kademlia DHT). Acceptable; documented in the PR.
- **Public infrastructure reliance:** default config uses libp2p's public bootstrap peers, the public IPFS Kademlia DHT, and public circuit-relay reservations. No action needed from users on typical networks. Feature does not function on networks where all DHT traffic is blocked. Future knob: user-supplied bootstrap list + private relay.
- **Memory ceiling:** worst-case publisher RAM with K connected followers is `K × 32 MiB` for per-follower send queues + one in-flight chunk (~1 MiB) per active emission. The catch-up ring lives on disk, not in RAM.
- **Disk ceiling:** up to 256 MiB of transient ciphertext per active publication in `share_ring`, evicted at 1h TTL. Practically: active publications use their allocation only during 1h of heavy pushing and fall back toward zero when idle.
- **Platform parity:** libp2p-go is pure Go. Preserves the CGo-free-Windows invariant. No OS-specific code paths added by this feature.
- **Data-dir additions:** `share_identity.key` (one file, ~100 bytes) and `share-staging/` directory (transient). Both live under the same data dir as the SQLite DB.
- **Telemetry:** none. Follow the existing pattern: local-only UI surfaces connection status; no usage data leaves the device.

## 11. Open questions — resolved during brainstorming and review

- ~~LAN-only vs internet-wide?~~ → internet-wide with NAT traversal.
- ~~What content propagates?~~ → new clips only, append-only, forward from follow.
- ~~Offline behavior?~~ → 1h catch-up buffer, accepted loss beyond.
- ~~Writable shares?~~ → no; 1:N read-only.
- ~~Tag-name auto-assign on follow?~~ → follower types a local tag name; publisher's tag name is not transmitted.
- ~~Sidebar layout for 4 tabs?~~ → keep `w-64`, switch to 2×2 grid.
- ~~Share string format?~~ → `mp-share:v1:<base64url(CBOR payload)>`, no addresses, no tag hint. Discovery via DHT.
- ~~Create-Share dialog?~~ → text string by default, QR on demand.
- ~~P2P library?~~ → libp2p-go.
- ~~Restart-stable shares when addresses are ephemeral?~~ → DHT-based discovery; share string contains only peer_id + key.
- ~~Memory budget under large clips?~~ → chunked envelopes (1 MiB max) + on-disk ciphertext ring in SQLite + byte-capped per-follower RAM queues.
- ~~Crypto safety against existing clip mutability (app.go:837/917/2751)?~~ → ring persists ciphertext, not references; replay does not re-read or re-encrypt the live clip.
- ~~Trust-model vs discovery contradiction?~~ → libp2p baseline services (Identify/DHT/relay/DCUtR) are enabled and characterized accurately; application-protocol whitelist enforces publication isolation; `AgentVersion` set to neutral.
- ~~clip_end reconstruction on mid-clip replay?~~ → the ring stores full envelopes, including `clip_end`, so any `since_seq` resumes correctly.
- ~~Backup/restore identity handling?~~ → explicit user choice (Take over / Keep / Cancel) when target install already has an identity.

## 12. Future work (not in this spec)

- Force-relay toggle (hide publisher's IP from followers).
- User-supplied relay servers.
- Multiple publications per tag (e.g. separate strings for separate audiences).
- Revocation per-follower without global `Stop`.
- Writable shares / co-publishing.
- Historical sync.
- Plugin event hooks (`share:published`, `share:received`, `share:followed`, `share:unfollowed`) once the core is proven.
- CLI support (`mp share start/stop`, `mp share follow/unfollow`) — maps onto the REST API once `ShareService` is stable.
