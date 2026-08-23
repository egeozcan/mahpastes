package app

import (
	"crypto/rand"
	"fmt"
	"os"

	"github.com/libp2p/go-libp2p/core/crypto"
	"github.com/libp2p/go-libp2p/core/peer"
)

// LoadOrCreateIdentity returns the Ed25519 private key at path, generating and
// persisting one if the file does not yet exist. Permissions are 0600.
// Returns a typed error on corrupt existing files.
func LoadOrCreateIdentity(path string) (crypto.PrivKey, error) {
	b, err := os.ReadFile(path)
	if err == nil {
		priv, verr := ValidateIdentityBytes(b)
		if verr != nil {
			return nil, fmt.Errorf("identity file %s: %w", path, verr)
		}
		return priv, nil
	}
	if !os.IsNotExist(err) {
		return nil, fmt.Errorf("reading identity file: %w", err)
	}

	priv, _, err := crypto.GenerateEd25519Key(rand.Reader)
	if err != nil {
		return nil, fmt.Errorf("generating Ed25519 key: %w", err)
	}
	marshalled, err := crypto.MarshalPrivateKey(priv)
	if err != nil {
		return nil, fmt.Errorf("marshal private key: %w", err)
	}
	if err := os.WriteFile(path, marshalled, 0o600); err != nil {
		return nil, fmt.Errorf("writing identity file: %w", err)
	}
	return priv, nil
}

// ValidateIdentityBytes parses b as a marshalled libp2p private key and returns
// it only if it is one this app can actually run as. Every acceptance test a
// stored identity has to pass lives here, and both readers of an identity go
// through it: LoadOrCreateIdentity when it reads the file at startup, and
// RestoreBackup before it installs a backup's identity over the local one.
//
// Sharing that check is the point. The restore's install is destructive and
// irreversible — there is no second copy of share_identity.key anywhere, and
// the peer id it holds is the one every share string already in circulation
// names — so bytes that LoadOrCreateIdentity would reject must never reach it.
// Two separate checks would eventually drift, and the direction that drift
// takes is a restore installing something the next startup cannot read.
//
// Parsing is not enough on its own. A marshalled Ed25519 key holds the 32-byte
// seed and the 32-byte public key side by side, and libp2p's unmarshal does not
// check that the second is derived from the first: the 64-byte form checks
// nothing at all, and the 96-byte form only checks the appended copy of the
// public half against the embedded one — both copies can be equally wrong.
// Bytes like that parse, type-check, and yield a peer id, but every signature
// they produce fails verification, so the host authenticates to nobody. Signing
// a fixed nonce and verifying it with the key's own public half is what
// separates a key from something merely shaped like one, and it is
// format-independent, so it keeps holding if libp2p's accepted encodings
// change.
func ValidateIdentityBytes(b []byte) (crypto.PrivKey, error) {
	priv, err := crypto.UnmarshalPrivateKey(b)
	if err != nil {
		return nil, fmt.Errorf("not a valid libp2p private key: %w", err)
	}
	if priv.Type() != crypto.Ed25519 {
		return nil, fmt.Errorf("not Ed25519")
	}
	sig, err := priv.Sign(identityProofMessage)
	if err != nil {
		return nil, fmt.Errorf("key cannot sign: %w", err)
	}
	ok, err := priv.GetPublic().Verify(identityProofMessage, sig)
	if err != nil {
		return nil, fmt.Errorf("key signature could not be verified: %w", err)
	}
	if !ok {
		return nil, fmt.Errorf("key is unusable: its private and public halves do not correspond")
	}
	return priv, nil
}

// identityProofMessage is the fixed nonce ValidateIdentityBytes signs to prove
// a parsed key actually works. Nothing depends on its content — it is never
// stored, sent, or compared — only on the sign/verify round trip exercising the
// real scalar and the stored public half.
var identityProofMessage = []byte("mahpastes identity self-check v1")

// PeerIDFromPrivKey returns the libp2p peer.ID derived from a private key.
func PeerIDFromPrivKey(priv crypto.PrivKey) peer.ID {
	pid, err := peer.IDFromPrivateKey(priv)
	if err != nil {
		// With a valid Ed25519 priv key this cannot fail.
		panic(fmt.Errorf("derive peer.ID: %w", err))
	}
	return pid
}

// PublicKeyBytes returns the 32-byte raw Ed25519 public key for use in the
// share-string payload.
func PublicKeyBytes(priv crypto.PrivKey) ([]byte, error) {
	pub := priv.GetPublic()
	return pub.Raw()
}
