package main

import (
	"bytes"
	"crypto/sha256"
	"encoding/base64"
	"errors"
	"fmt"
	"strings"

	"github.com/fxamacker/cbor/v2"
)

// sharePayload is the CBOR payload inside a share string.
// Field names use short integer keys to keep the encoded string compact.
type sharePayload struct {
	PeerID []byte `cbor:"1,keyasint"`
	Key    []byte `cbor:"2,keyasint"`
}

// EncodeShareString builds "mp-share:v1:<base64url(CBOR{peer_id, key})>".
func EncodeShareString(peerID, key []byte) (string, error) {
	if len(peerID) != 32 {
		return "", fmt.Errorf("peer_id must be 32 bytes (got %d)", len(peerID))
	}
	if len(key) != 32 {
		return "", fmt.Errorf("key must be 32 bytes (got %d)", len(key))
	}
	body, err := cbor.Marshal(sharePayload{PeerID: peerID, Key: key})
	if err != nil {
		return "", fmt.Errorf("cbor marshal: %w", err)
	}
	b64 := base64.RawURLEncoding.EncodeToString(body)
	return ShareStringPrefix + b64, nil
}

// DecodeShareString parses a share string and returns peer_id + key.
func DecodeShareString(s string) (peerID, key []byte, err error) {
	if !strings.HasPrefix(s, ShareStringPrefix) {
		return nil, nil, errors.New("invalid share string: missing prefix")
	}
	rest := strings.TrimPrefix(s, ShareStringPrefix)
	body, err := base64.RawURLEncoding.DecodeString(rest)
	if err != nil {
		return nil, nil, fmt.Errorf("invalid share string: base64: %w", err)
	}
	var p sharePayload
	if err := cbor.Unmarshal(body, &p); err != nil {
		return nil, nil, fmt.Errorf("invalid share string: cbor: %w", err)
	}
	if len(p.PeerID) != 32 || len(p.Key) != 32 {
		return nil, nil, errors.New("invalid share string: wrong field sizes")
	}
	return p.PeerID, p.Key, nil
}

// DeriveShareID returns the 16-byte public identifier for a publication,
// computed as SHA-256(symkey)[:16].
func DeriveShareID(symkey []byte) []byte {
	h := sha256.Sum256(symkey)
	out := make([]byte, 16)
	copy(out, h[:16])
	return out
}

// guard against the `bytes` import being unused if future refactors change shape
var _ = bytes.Equal
