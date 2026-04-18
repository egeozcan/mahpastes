/**
 * share-address-change.spec.ts
 *
 * Verifies that a follower transparently reconnects when the publisher's
 * network address changes (e.g. the publisher moves to a different Wi-Fi
 * subnet between sessions) and the share string is re-discovered via mDNS.
 *
 * WHY SKIPPED:
 *   Simulating a real network address change in a local e2e environment
 *   requires either multiple network interfaces, virtual NICs, or iptables
 *   manipulation — none of which are reliably available in CI or on a
 *   developer laptop without elevated privileges.
 *
 *   The underlying mechanism (mDNS re-advertisement on startup + follower
 *   backoff reconnect) is already exercised by share-publisher-restart.spec.ts
 *   for the within-same-address case.  A multi-NIC test bed is needed to
 *   cover the address-change scenario end-to-end.
 *
 * TODO: Enable once the test infrastructure supports virtual network
 *   interfaces (e.g. via Docker bridge networks in CI, or a dedicated
 *   macOS System Extensions test helper).
 */

import { test } from '../../fixtures/test-fixtures.js';

test.describe('Share - Address change (publisher NIC switch)', () => {

  test.skip(
    true,
    'TODO: requires multi-NIC or virtual network support not available in current test infra',
  );

  test(
    'follower reconnects after publisher address changes',
    async () => {
      // TODO: Implement when test infra supports virtual network interfaces.
      // Steps:
      //   1. Publisher starts share on NIC A (address A1).
      //   2. Follower connects via mDNS, receives clip 1.
      //   3. Switch publisher to NIC B (address B1) — disable NIC A in OS network prefs.
      //   4. Restart publisher so mDNS re-advertises on NIC B.
      //   5. Publisher uploads clip 2.
      //   6. Follower must rediscover via mDNS on NIC B, reconnect, and receive clip 2.
      //   7. Assert follower clip count = 2.
    },
  );
});
