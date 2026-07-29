#!/usr/bin/env bash
# between-suites.sh
#
# Run between the main Playwright suite and the share suite.
# Kills any lingering wails dev processes (belt-and-suspenders on top of
# global-teardown), clears the restart lock, and pauses briefly so that
# macOS has time to reclaim virtual-memory pages from the four primary
# wails instances that just exited.  Without this pause, the share suite's
# first secondary spawn can push the system into low-memory territory and
# trigger Jetsam SIGKILL of the Playwright worker process.
set -euo pipefail

echo "[between-suites] killing stray wails processes..."
pkill -f "wails dev" 2>/dev/null || true
pkill -f "devserver localhost" 2>/dev/null || true

# Wait for them to actually exit before the next suite starts building.
# A `wails dev` unlinks the shared app binary
# (build/bin/mahpastes.app/Contents/MacOS/mahpastes) from its exit handler, so a
# straggler that outlives this script deletes that binary at an arbitrary later
# moment — potentially inside the next suite's build, where it breaks the
# codesign step and wedges that instance permanently.
for _ in $(seq 1 40); do
  pgrep -f "devserver localhost" >/dev/null 2>&1 || break
  sleep 0.5
done
if pgrep -f "devserver localhost" >/dev/null 2>&1; then
  echo "[between-suites] SIGKILLing survivors: $(pgrep -f 'devserver localhost' | tr '\n' ' ')"
  pkill -9 -f "devserver localhost" 2>/dev/null || true
  sleep 1
fi

echo "[between-suites] clearing restart lock..."
# Must match os.tmpdir() in wails-manager.ts, which on macOS is $TMPDIR
# (/var/folders/.../T), NOT /tmp.
rm -rf "${TMPDIR:-/tmp}/mahpastes-test-restart-lock" 2>/dev/null || true

echo "[between-suites] waiting 15 s for OS to reclaim memory..."
sleep 15

echo "[between-suites] done."
