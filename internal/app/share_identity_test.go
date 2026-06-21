package app

import (
	"os"
	"path/filepath"
	"testing"
)

func TestIdentityGenerateAndReload(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, ShareIdentityFile)

	priv1, err := LoadOrCreateIdentity(path)
	if err != nil {
		t.Fatalf("first load: %v", err)
	}
	pid1 := PeerIDFromPrivKey(priv1)

	priv2, err := LoadOrCreateIdentity(path)
	if err != nil {
		t.Fatalf("second load: %v", err)
	}
	pid2 := PeerIDFromPrivKey(priv2)

	if pid1 != pid2 {
		t.Fatalf("peer IDs differ across reload: %s vs %s", pid1, pid2)
	}
}

func TestIdentityFilePermissions(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, ShareIdentityFile)
	_, err := LoadOrCreateIdentity(path)
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm() != 0o600 {
		t.Errorf("identity file perms = %v, want 0600", info.Mode().Perm())
	}
}

func TestIdentityCorruptFileReturnsError(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, ShareIdentityFile)
	if err := os.WriteFile(path, []byte("garbage"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := LoadOrCreateIdentity(path); err == nil {
		t.Fatal("expected error on corrupt identity file")
	}
}

func TestPublicKeyBytes32(t *testing.T) {
	dir := t.TempDir()
	priv, err := LoadOrCreateIdentity(filepath.Join(dir, ShareIdentityFile))
	if err != nil {
		t.Fatal(err)
	}
	pubBytes, err := PublicKeyBytes(priv)
	if err != nil {
		t.Fatal(err)
	}
	if len(pubBytes) != 32 {
		t.Fatalf("pub key bytes = %d, want 32 (raw Ed25519)", len(pubBytes))
	}
}
