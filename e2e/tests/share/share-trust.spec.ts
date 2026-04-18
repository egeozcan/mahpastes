/**
 * share-trust.spec.ts
 *
 * COVERAGE NOTE — not an e2e test by design.
 *
 * The "follower rejects clips from a publisher with a mismatched identity
 * key" scenario is covered by the Go unit test
 *   TestPublisherStreamRejectsWrongHMAC  in share_manager_test.go
 * which:
 *   1. Starts a real libp2p ShareManager as the publisher with symkey K1.
 *   2. Opens a follower stream with a DIFFERENT symkey K2 and a handshake
 *      whose HMAC therefore fails to verify against K1.
 *   3. Asserts the publisher resets the stream (no plaintext leaks).
 *
 * That Go test exercises the crypto trust boundary at the protocol level
 * without needing a third Playwright context, a rogue binary, or mDNS
 * hijacking. Replicating the same assertion in Playwright would require
 * spawning a custom Go process that signs envelopes with a foreign key
 * and serves them on the same mDNS service tag — strictly more complex
 * machinery for zero extra coverage.
 *
 * This file deliberately imports nothing and declares no tests. Its only
 * job is to document where the coverage lives for future grep-driven
 * archaeologists. If you're looking for e2e protection against identity
 * forgery, read the Go test above instead.
 */

// Intentionally empty — see the file comment.
export {};
