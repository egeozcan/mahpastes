/**
 * share-address-change.spec.ts
 *
 * COVERAGE NOTE — not an e2e test by design.
 *
 * The "follower transparently reconnects when the publisher's network
 * address changes" scenario is covered by share-publisher-restart.spec.ts,
 * which restarts the publisher wails instance and the new libp2p host
 * binds to freshly-allocated random ports every time. From the follower's
 * perspective that is an address change: the publisher's multiaddrs at
 * (loopback:oldPort) no longer answer and the new (loopback:newPort)
 * multiaddrs must be rediscovered via mDNS before the reconnect can
 * succeed.
 *
 * What share-publisher-restart.spec.ts verifies that is identical to an
 * IP-level address change:
 *   - Publisher's libp2p host is completely torn down (Stop/Close).
 *   - A fresh host is started with new transport endpoints.
 *   - mDNS re-advertises the peer ID on the new endpoints.
 *   - The follower's runFollowLoop fast path (peerstore) misses, so it
 *     falls through to the slow path (dht.FindPeer + mDNS-sourced
 *     peerstore entry) to pick up the new multiaddrs.
 *   - Receipt of a clip after the restart confirms the follower
 *     re-established the encrypted stream using the rediscovered
 *     address.
 *
 * The strict "different IP, same machine" case is genuinely multi-NIC
 * and not reachable from a Playwright fixture running under a single
 * network stack. Because mDNS and the dht.FindPeer path are
 * address-family agnostic, the port-change test in
 * share-publisher-restart exercises exactly the same code path.
 *
 * This file deliberately declares no tests. Its job is to document where
 * the coverage lives so a future reader does not re-introduce a skip
 * stub pointing at imaginary "multi-NIC test infra."
 */

// Intentionally empty — see the file comment.
export {};
