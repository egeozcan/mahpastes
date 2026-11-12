package buildinfo

import "testing"

func TestCurrentPrefersStampedVersion(t *testing.T) {
	old := Version
	defer func() { Version = old }()
	Version = "v9.8.7"
	if got := Current(); got != "v9.8.7" {
		t.Fatalf("Current() = %q, want v9.8.7", got)
	}
}

func TestCurrentNeverEmpty(t *testing.T) {
	old := Version
	defer func() { Version = old }()
	Version = "dev"
	if Current() == "" {
		t.Fatal("Current() is empty for an unstamped build")
	}
}
