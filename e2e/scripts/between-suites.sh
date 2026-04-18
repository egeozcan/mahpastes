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

echo "[between-suites] clearing restart lock..."
rm -rf /tmp/mahpastes-test-restart-lock 2>/dev/null || true

echo "[between-suites] waiting 15 s for OS to reclaim memory..."
sleep 15

echo "[between-suites] done."
