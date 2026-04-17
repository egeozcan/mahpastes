package main

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
		priv, uerr := crypto.UnmarshalPrivateKey(b)
		if uerr != nil {
			return nil, fmt.Errorf("identity file %s is corrupt: %w", path, uerr)
		}
		if priv.Type() != crypto.Ed25519 {
			return nil, fmt.Errorf("identity file %s is not Ed25519", path)
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
