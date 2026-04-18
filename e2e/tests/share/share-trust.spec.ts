/**
 * share-trust.spec.ts
 *
 * Verifies that a follower refuses to accept clips from a publisher whose
 * identity key does not match the one embedded in the share string (i.e.
 * a MITM or forged publisher).
 *
 * WHY SKIPPED:
 *   Testing cryptographic identity mismatch requires spawning a *third*
 *   wails instance that presents a different key-pair on the same mDNS
 *   service name as the legitimate publisher.  The current test infra does
 *   not support injecting a rogue libp2p peer into the running network.
 *
 *   Achieving this without a custom Go-side test binary (a "rogue publisher"
 *   that signs envelopes with a different key) is not feasible in the
 *   Playwright/Wails-dev e2e environment.
 *
 * TODO: Enable once a "rogue publisher" test helper binary is available
 *   (e.g. a small Go tool that accepts a share string, derives a *different*
 *   private key, and serves signed envelopes so the follower can verify
 *   rejection).
 */

import { test } from '../../fixtures/test-fixtures.js';

test.describe('Share - Trust / cryptographic identity enforcement', () => {

  test.skip(
    true,
    'TODO: requires rogue-publisher test binary to simulate key mismatch; not yet available',
  );

  test(
    'follower rejects clips from a publisher with a mismatched identity key',
    async () => {
      // TODO: Implement once rogue-publisher test harness exists.
      // Steps:
      //   1. Publisher A creates share string (embeds pubkey K_A).
      //   2. Follower B subscribes; receives clip 1 from A.  last_seq = 1.
      //   3. Rogue publisher R (key K_R ≠ K_A) advertises same share tag on mDNS.
      //   4. Follower sees R as an alternative peer and attempts to fetch.
      //   5. Assert: follower detects key mismatch, logs an error, and does NOT
      //      add any new clips from R.  follow.status stays 'connected' to A.
      //   6. Legitimate publisher A uploads clip 2; follower receives it normally.
    },
  );
});
