package app

import (
	"bytes"
	"crypto/rand"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/libp2p/go-libp2p/core/crypto"
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

// TestValidateIdentityBytesMatchesLoadOrCreate pins the two readers of an
// identity to one set of rules. RestoreBackup installs a backup's identity over
// the local one destructively and irreversibly, so anything it accepts has to
// be something LoadOrCreateIdentity will still accept on the next start — if
// those two ever disagree, the disagreement is an install the app cannot read
// and an old key that is already gone.
func TestValidateIdentityBytesMatchesLoadOrCreate(t *testing.T) {
	path := filepath.Join(t.TempDir(), ShareIdentityFile)
	created, err := LoadOrCreateIdentity(path)
	if err != nil {
		t.Fatalf("create identity: %v", err)
	}
	stored, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read identity file: %v", err)
	}

	validated, err := ValidateIdentityBytes(stored)
	if err != nil {
		t.Fatalf("ValidateIdentityBytes rejected the bytes LoadOrCreateIdentity just wrote: %v", err)
	}
	if got, want := PeerIDFromPrivKey(validated), PeerIDFromPrivKey(created); got != want {
		t.Errorf("peer id from validated key = %s, want %s", got, want)
	}

	// A well-formed libp2p key of the wrong type is the case an unmarshal-only
	// check would wave through.
	secp, _, err := crypto.GenerateKeyPairWithReader(crypto.Secp256k1, 256, rand.Reader)
	if err != nil {
		t.Fatalf("generate secp256k1 key: %v", err)
	}
	secpBytes, err := crypto.MarshalPrivateKey(secp)
	if err != nil {
		t.Fatalf("marshal secp256k1 key: %v", err)
	}

	for name, b := range map[string][]byte{
		"nil":           nil,
		"empty":         {},
		"garbage":       []byte("not-a-marshalled-libp2p-private-key"),
		"truncated key": stored[:len(stored)/2],
		"non-Ed25519":   secpBytes,
	} {
		if _, err := ValidateIdentityBytes(b); err == nil {
			t.Errorf("ValidateIdentityBytes(%s) returned no error", name)
		}
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

// ed25519KeyEnvelope wraps raw Ed25519 private-key bytes in the protobuf
// envelope crypto.MarshalPrivateKey produces, so a test can hand
// ValidateIdentityBytes a payload the marshal API refuses to build.
//
// The encoding is two fields of crypto.pb.PrivateKey: field 1 (Type) as a
// varint holding KeyType_Ed25519 = 1, then field 2 (Data) length-delimited.
// Raw key payloads are 64 or 96 bytes, so the length is always one varint byte.
// The caller checks the 64-byte case against crypto.MarshalPrivateKey, which is
// what keeps this hand-encoding honest.
func ed25519KeyEnvelope(t *testing.T, raw []byte) []byte {
	t.Helper()
	if len(raw) > 127 {
		t.Fatalf("raw key of %d bytes needs a multi-byte length varint", len(raw))
	}
	return append([]byte{0x08, 0x01, 0x12, byte(len(raw))}, raw...)
}

// mismatchedEd25519Halves returns raw private-key bytes whose seed and public
// half belong to different keys: 32 bytes of seed from one generated key and
// the 32-byte public key of another. Signatures made with it never verify.
func mismatchedEd25519Halves(t *testing.T) []byte {
	t.Helper()
	privA, _, err := crypto.GenerateEd25519Key(rand.Reader)
	if err != nil {
		t.Fatalf("generate key A: %v", err)
	}
	rawA, err := privA.Raw()
	if err != nil {
		t.Fatalf("raw key A: %v", err)
	}
	_, pubB, err := crypto.GenerateEd25519Key(rand.Reader)
	if err != nil {
		t.Fatalf("generate key B: %v", err)
	}
	rawPubB, err := pubB.Raw()
	if err != nil {
		t.Fatalf("raw public key B: %v", err)
	}
	mixed := make([]byte, 0, len(rawA))
	mixed = append(mixed, rawA[:32]...)
	return append(mixed, rawPubB...)
}

// TestValidateIdentityBytesRejectsMismatchedKeyHalves covers the check that
// parsing alone cannot make. A marshalled Ed25519 key carries the seed and the
// public key side by side, and libp2p's unmarshal does not verify that the
// second follows from the first: the 64-byte form checks nothing, and the
// 96-byte form only compares the appended copy of the public half against the
// embedded one — both copies can be equally wrong. Such bytes parse, type-check
// and yield a peer id, but every signature they make fails verification, so the
// libp2p host authenticates to nobody.
//
// That matters most at the restore: installBackupIdentity would replace the
// machine's only identity with an unusable one, and LoadOrCreateIdentity would
// keep accepting it on every start afterwards, with nothing saying why sharing
// no longer works.
func TestValidateIdentityBytesRejectsMismatchedKeyHalves(t *testing.T) {
	priv, _, err := crypto.GenerateEd25519Key(rand.Reader)
	if err != nil {
		t.Fatalf("generate key: %v", err)
	}
	raw, err := priv.Raw()
	if err != nil {
		t.Fatalf("raw key: %v", err)
	}
	if len(raw) != 64 {
		t.Fatalf("raw Ed25519 private key = %d bytes, want 64", len(raw))
	}
	mixed := mismatchedEd25519Halves(t)

	// The hand-built envelope has to be exactly what the library builds, or the
	// rejections below could be rejections of a malformed wrapper rather than
	// of the key inside it. crypto.MarshalPrivateKey emits Raw(), which for
	// Ed25519 is always the 64-byte form, so this is also the reason the
	// 96-byte cases have to be hand-built: no marshal API in this version of
	// go-libp2p can produce one.
	marshalled, err := crypto.MarshalPrivateKey(priv)
	if err != nil {
		t.Fatalf("marshal key: %v", err)
	}
	if !bytes.Equal(ed25519KeyEnvelope(t, raw), marshalled) {
		t.Fatalf("hand-built envelope != crypto.MarshalPrivateKey output; the encoding assumption is wrong")
	}

	accepted := map[string][]byte{
		"64-byte key":                    ed25519KeyEnvelope(t, raw),
		"96-byte key with redundant pub": ed25519KeyEnvelope(t, append(append([]byte{}, raw...), raw[32:]...)),
	}
	for name, b := range accepted {
		validated, err := ValidateIdentityBytes(b)
		if err != nil {
			t.Errorf("ValidateIdentityBytes(%s) = %v, want it accepted", name, err)
			continue
		}
		if got, want := PeerIDFromPrivKey(validated), PeerIDFromPrivKey(priv); got != want {
			t.Errorf("peer id from %s = %s, want %s", name, got, want)
		}
	}

	rejected := map[string][]byte{
		// Passes unmarshal today: the 64-byte arm checks nothing at all.
		"64-byte key with mismatched halves": ed25519KeyEnvelope(t, mixed),
		// Passes the redundancy check too — the appended copy matches the
		// embedded public half, and both are the wrong key's.
		"96-byte key with consistently wrong pub": ed25519KeyEnvelope(t, append(append([]byte{}, mixed...), mixed[32:]...)),
		// Caught one layer lower, by the redundancy check itself.
		"96-byte key with non-redundant pub": ed25519KeyEnvelope(t, append(append([]byte{}, raw...), mixed[32:]...)),
	}
	for name, b := range rejected {
		if _, err := ValidateIdentityBytes(b); err == nil {
			t.Errorf("ValidateIdentityBytes(%s) returned no error", name)
		}
	}

	// The rejection has to survive the round trip an adoption actually takes:
	// written to a file and read back by LoadOrCreateIdentity at the next
	// start. A key this broken must be reported as corrupt, not loaded.
	path := filepath.Join(t.TempDir(), ShareIdentityFile)
	if err := os.WriteFile(path, ed25519KeyEnvelope(t, mixed), 0o600); err != nil {
		t.Fatalf("write mismatched identity: %v", err)
	}
	if _, err := LoadOrCreateIdentity(path); err == nil {
		t.Error("LoadOrCreateIdentity accepted a key whose halves do not correspond")
	} else if !strings.Contains(err.Error(), path) {
		t.Errorf("error = %v, want it to name the identity file it rejected", err)
	}
}
