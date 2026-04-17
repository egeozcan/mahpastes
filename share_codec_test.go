package main

import (
	"bytes"
	"strings"
	"testing"
)

func TestEncodeDecodeRoundtrip(t *testing.T) {
	peerID := bytes.Repeat([]byte{0xAB}, 32)
	key := bytes.Repeat([]byte{0xCD}, 32)

	s, err := EncodeShareString(peerID, key)
	if err != nil {
		t.Fatalf("encode failed: %v", err)
	}
	if !strings.HasPrefix(s, ShareStringPrefix) {
		t.Fatalf("prefix mismatch: %q", s)
	}
	if len(s) > 160 {
		t.Fatalf("encoded length %d exceeds budget (≤160)", len(s))
	}

	gotPeerID, gotKey, err := DecodeShareString(s)
	if err != nil {
		t.Fatalf("decode failed: %v", err)
	}
	if !bytes.Equal(gotPeerID, peerID) {
		t.Fatalf("peer_id mismatch")
	}
	if !bytes.Equal(gotKey, key) {
		t.Fatalf("key mismatch")
	}
}

func TestDecodeRejectsMissingPrefix(t *testing.T) {
	if _, _, err := DecodeShareString("abcdef"); err == nil {
		t.Fatal("expected error for missing prefix")
	}
}

func TestDecodeRejectsWrongVersion(t *testing.T) {
	if _, _, err := DecodeShareString("mp-share:v99:xxxxx"); err == nil {
		t.Fatal("expected error for wrong version")
	}
}

func TestDecodeRejectsMalformedBase64(t *testing.T) {
	if _, _, err := DecodeShareString("mp-share:v1:!!!not-base64!!!"); err == nil {
		t.Fatal("expected error for malformed base64")
	}
}

func TestDecodeRejectsMalformedCBOR(t *testing.T) {
	// Valid base64 but garbage CBOR body
	if _, _, err := DecodeShareString("mp-share:v1:AAAAAA"); err == nil {
		t.Fatal("expected error for malformed CBOR")
	}
}

func TestShareIDDerivation(t *testing.T) {
	key := bytes.Repeat([]byte{0x42}, 32)
	id1 := DeriveShareID(key)
	id2 := DeriveShareID(key)
	if len(id1) != 16 {
		t.Fatalf("share_id length %d != 16", len(id1))
	}
	if !bytes.Equal(id1, id2) {
		t.Fatalf("derivation not deterministic")
	}
	other := DeriveShareID(bytes.Repeat([]byte{0x43}, 32))
	if bytes.Equal(id1, other) {
		t.Fatalf("derivation should differ on different keys")
	}
}
