package main

import (
	"bytes"
	"crypto/rand"
	"encoding/binary"
	"io"
	"strings"
	"testing"
)

func TestEnvelopeEncryptDecryptRoundtrip(t *testing.T) {
	key := make([]byte, 32)
	rand.Read(key)
	shareID := DeriveShareID(key)

	plaintext := []byte("hello envelope")
	seq := uint64(42)

	frame, err := EncryptEnvelope(key, shareID, seq, plaintext)
	if err != nil {
		t.Fatalf("encrypt: %v", err)
	}
	// Frame layout: u32 length || nonce || ciphertext+tag
	if len(frame) < 4+12+16 {
		t.Fatalf("frame too short: %d", len(frame))
	}
	innerLen := binary.BigEndian.Uint32(frame[:4])
	if int(innerLen) != len(frame)-4 {
		t.Fatalf("length prefix mismatch: %d vs body %d", innerLen, len(frame)-4)
	}

	got, err := DecryptEnvelope(key, shareID, seq, frame)
	if err != nil {
		t.Fatalf("decrypt: %v", err)
	}
	if !bytes.Equal(got, plaintext) {
		t.Fatalf("plaintext mismatch: %q", got)
	}
}

func TestEnvelopeWrongSeqFails(t *testing.T) {
	key := make([]byte, 32)
	rand.Read(key)
	shareID := DeriveShareID(key)

	frame, err := EncryptEnvelope(key, shareID, 1, []byte("x"))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := DecryptEnvelope(key, shareID, 2, frame); err == nil {
		t.Fatal("expected decrypt fail on wrong seq AAD")
	}
}

func TestEnvelopeWrongShareIDFails(t *testing.T) {
	key := make([]byte, 32)
	rand.Read(key)
	shareID := DeriveShareID(key)
	bogus := make([]byte, 16)

	frame, err := EncryptEnvelope(key, shareID, 1, []byte("x"))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := DecryptEnvelope(key, bogus, 1, frame); err == nil {
		t.Fatal("expected decrypt fail on wrong share_id AAD")
	}
}

func TestReadFrameFromStream(t *testing.T) {
	key := make([]byte, 32)
	rand.Read(key)
	shareID := DeriveShareID(key)
	f1, _ := EncryptEnvelope(key, shareID, 1, []byte("msg1"))
	f2, _ := EncryptEnvelope(key, shareID, 2, []byte("msg2"))

	var buf bytes.Buffer
	buf.Write(f1)
	buf.Write(f2)

	out1, err := ReadFrame(&buf)
	if err != nil {
		t.Fatal(err)
	}
	got1, _ := DecryptEnvelope(key, shareID, 1, out1)
	if string(got1) != "msg1" {
		t.Fatalf("frame 1 plaintext %q", got1)
	}
	out2, err := ReadFrame(&buf)
	if err != nil {
		t.Fatal(err)
	}
	got2, _ := DecryptEnvelope(key, shareID, 2, out2)
	if string(got2) != "msg2" {
		t.Fatalf("frame 2 plaintext %q", got2)
	}
	if _, err := ReadFrame(&buf); err != io.EOF {
		t.Fatalf("expected EOF at end, got %v", err)
	}
}

func TestReadFrameRejectsOverlongEnvelope(t *testing.T) {
	// Pretend someone sends length larger than MaxEnvelopeLen.
	var hdr [4]byte
	binary.BigEndian.PutUint32(hdr[:], uint32(MaxEnvelopeLen+1))
	r := bytes.NewReader(hdr[:])
	_, err := ReadFrame(r)
	if err == nil || !strings.Contains(err.Error(), "too large") {
		t.Fatalf("expected 'too large' error, got %v", err)
	}
}

func TestHandshakeRoundtrip(t *testing.T) {
	key := make([]byte, 32)
	rand.Read(key)
	shareID := DeriveShareID(key)

	hs := BuildHandshake(key, shareID, 7)
	parsed, err := ParseHandshake(hs)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if !bytes.Equal(parsed.ShareID, shareID) {
		t.Fatal("share_id mismatch")
	}
	if parsed.SinceSeq != 7 {
		t.Fatalf("since_seq %d", parsed.SinceSeq)
	}
	if err := VerifyHandshake(key, parsed); err != nil {
		t.Fatalf("verify: %v", err)
	}
}

func TestHandshakeVerifyFailsOnTamper(t *testing.T) {
	key := make([]byte, 32)
	rand.Read(key)
	shareID := DeriveShareID(key)
	hs := BuildHandshake(key, shareID, 0)
	// HMAC occupies bytes 32..63 (16 share_id + 16 nonce + 32 hmac + 8 since_seq).
	// Flip a middle byte of the HMAC range to ensure we change the MAC, not since_seq.
	hs[50] ^= 0xFF
	parsed, err := ParseHandshake(hs)
	if err != nil {
		t.Fatal(err)
	}
	if err := VerifyHandshake(key, parsed); err == nil {
		t.Fatal("expected verify fail on tampered HMAC")
	}
}

func TestHandshakeVerifyFailsWithWrongKey(t *testing.T) {
	k1 := make([]byte, 32)
	k2 := make([]byte, 32)
	rand.Read(k1)
	rand.Read(k2)
	shareID := DeriveShareID(k1)
	hs := BuildHandshake(k1, shareID, 0)
	parsed, _ := ParseHandshake(hs)
	if err := VerifyHandshake(k2, parsed); err == nil {
		t.Fatal("expected verify fail with wrong key")
	}
}

func TestClipStartRoundtrip(t *testing.T) {
	meta := map[string]string{"author": "alice"}
	s := ClipStartPayload{
		Seq: 10, TS: 1_700_000_000_000,
		Kind: KindClipStart, ClipID: 123,
		Filename: "photo.png", ContentType: "image/png",
		Metadata: meta, TotalSize: 2048, ChunkCount: 3,
	}
	b, err := MarshalPayload(s)
	if err != nil {
		t.Fatal(err)
	}
	kind, raw, err := PeekPayloadKind(b)
	if err != nil {
		t.Fatal(err)
	}
	if kind != KindClipStart {
		t.Fatalf("kind %q", kind)
	}
	var got ClipStartPayload
	if err := UnmarshalPayload(raw, &got); err != nil {
		t.Fatal(err)
	}
	if got.Filename != "photo.png" || got.ChunkCount != 3 {
		t.Fatalf("got %+v", got)
	}
}

func TestClipChunkAndEndRoundtrip(t *testing.T) {
	c := ClipChunkPayload{Seq: 11, Kind: KindClipChunk, ClipID: 123, Index: 0, Data: []byte("hello")}
	b, _ := MarshalPayload(c)
	var gotC ClipChunkPayload
	if err := UnmarshalPayload(b, &gotC); err != nil {
		t.Fatal(err)
	}
	if string(gotC.Data) != "hello" {
		t.Fatalf("data %q", gotC.Data)
	}

	e := ClipEndPayload{Seq: 12, Kind: KindClipEnd, ClipID: 123, SHA256: make([]byte, 32)}
	b2, _ := MarshalPayload(e)
	var gotE ClipEndPayload
	if err := UnmarshalPayload(b2, &gotE); err != nil {
		t.Fatal(err)
	}
	if len(gotE.SHA256) != 32 {
		t.Fatal("sha256 len")
	}
}
