//go:build darwin

package main

import (
	"os"
	"testing"
)

// Every application the real lookup lists must still exist once it returns.
//
// Opt-in: it starts osascript (up to three times, each bounded only by a 10 s
// timeout) and asserts on this machine's Launch Services database, where an
// application deleted without being unregistered is still listed, and a
// sandboxed runner has no window server to ask. The filter it exercises is
// covered everywhere by TestLookupOpenWithAppsDropsProbeItself.
func TestLookupOpenWithAppsListsOnlyExistingApps(t *testing.T) {
	if os.Getenv("MAHPASTES_TEST_OSASCRIPT") == "" {
		t.Skip("set MAHPASTES_TEST_OSASCRIPT=1 to run against the real Launch Services")
	}
	if _, err := os.Stat("/usr/bin/osascript"); err != nil {
		t.Skip("osascript is not available")
	}
	fakeOpenWithLister(t, listOpenWithApps)
	for _, ext := range []string{".app", ".APP", ".txt"} {
		apps, err := lookupOpenWithApps(ext)
		if err != nil {
			t.Fatalf("%s: %v", ext, err)
		}
		for _, a := range apps {
			if _, err := os.Stat(a.Path); err != nil {
				t.Errorf("%s: %q points at a missing path: %v", ext, a.Name, err)
			}
		}
	}
}
