package app

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"encoding/binary"
	"errors"
	"fmt"
	"io"

	"github.com/fxamacker/cbor/v2"
)

// EncryptEnvelope builds the full on-wire frame:
//
//	u32 length || 12-byte nonce || ciphertext || GCM tag
//
// where AAD = share_id || seq (u64 BE).
func EncryptEnvelope(key, shareID []byte, seq uint64, plaintext []byte) ([]byte, error) {
	if len(key) != 32 {
		return nil, errors.New("key must be 32 bytes")
	}
	if len(shareID) != 16 {
		return nil, errors.New("share_id must be 16 bytes")
	}
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, err
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return nil, err
	}
	nonce := make([]byte, gcm.NonceSize()) // 12 bytes
	if _, err := rand.Read(nonce); err != nil {
		return nil, err
	}
	aad := make([]byte, 16+8)
	copy(aad, shareID)
	binary.BigEndian.PutUint64(aad[16:], seq)

	ct := gcm.Seal(nil, nonce, plaintext, aad)
	inner := make([]byte, 0, len(nonce)+len(ct))
	inner = append(inner, nonce...)
	inner = append(inner, ct...)

	if len(inner) > MaxEnvelopeLen {
		return nil, fmt.Errorf("envelope too large: %d > %d", len(inner), MaxEnvelopeLen)
	}
	frame := make([]byte, 4+len(inner))
	binary.BigEndian.PutUint32(frame[:4], uint32(len(inner)))
	copy(frame[4:], inner)
	return frame, nil
}

// DecryptEnvelope verifies and decrypts a full framed envelope (as produced by
// EncryptEnvelope). Returns plaintext.
func DecryptEnvelope(key, shareID []byte, seq uint64, frame []byte) ([]byte, error) {
	if len(frame) < 4+12+16 {
		return nil, errors.New("frame too short")
	}
	innerLen := binary.BigEndian.Uint32(frame[:4])
	if int(innerLen)+4 != len(frame) {
		return nil, errors.New("frame length mismatch")
	}
	nonce := frame[4 : 4+12]
	ct := frame[4+12:]

	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, err
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return nil, err
	}
	aad := make([]byte, 16+8)
	copy(aad, shareID)
	binary.BigEndian.PutUint64(aad[16:], seq)

	pt, err := gcm.Open(nil, nonce, ct, aad)
	if err != nil {
		return nil, fmt.Errorf("gcm open: %w", err)
	}
	return pt, nil
}

// ReadFrame reads one length-prefixed envelope from r and returns the full
// frame (length || nonce || ciphertext || tag) as a single []byte.
func ReadFrame(r io.Reader) ([]byte, error) {
	var hdr [4]byte
	if _, err := io.ReadFull(r, hdr[:]); err != nil {
		return nil, err
	}
	n := binary.BigEndian.Uint32(hdr[:])
	if int(n) > MaxEnvelopeLen {
		return nil, fmt.Errorf("frame body too large: %d > %d", n, MaxEnvelopeLen)
	}
	body := make([]byte, n)
	if _, err := io.ReadFull(r, body); err != nil {
		return nil, err
	}
	out := make([]byte, 4+int(n))
	copy(out, hdr[:])
	copy(out[4:], body)
	return out, nil
}

// Handshake is the one-shot follower→publisher message sent at stream open.
// Wire layout: share_id (16) || proof_nonce (16) || proof_hmac (32) || since_seq (u64 BE) = 72 bytes.
type Handshake struct {
	ShareID    []byte // 16
	ProofNonce []byte // 16
	ProofHMAC  []byte // 32
	SinceSeq   uint64
}

// HandshakeBytesLen is the on-wire size of a handshake blob.
const HandshakeBytesLen = 16 + 16 + 32 + 8

// BuildHandshake produces the 72-byte handshake blob for a follower.
func BuildHandshake(symkey, shareID []byte, sinceSeq uint64) []byte {
	nonce := make([]byte, 16)
	if _, err := rand.Read(nonce); err != nil {
		panic(fmt.Errorf("handshake nonce rand: %w", err))
	}
	h := hmac.New(sha256.New, symkey)
	h.Write([]byte(HandshakeHMACContext))
	h.Write(nonce)
	mac := h.Sum(nil)

	buf := make([]byte, 0, HandshakeBytesLen)
	buf = append(buf, shareID...)
	buf = append(buf, nonce...)
	buf = append(buf, mac...)
	var seqBuf [8]byte
	binary.BigEndian.PutUint64(seqBuf[:], sinceSeq)
	buf = append(buf, seqBuf[:]...)
	return buf
}

// ParseHandshake decodes the 72-byte blob into a Handshake struct. It does NOT
// verify the HMAC — call VerifyHandshake with the looked-up key.
func ParseHandshake(b []byte) (*Handshake, error) {
	if len(b) != HandshakeBytesLen {
		return nil, fmt.Errorf("handshake len %d != %d", len(b), HandshakeBytesLen)
	}
	return &Handshake{
		ShareID:    append([]byte(nil), b[0:16]...),
		ProofNonce: append([]byte(nil), b[16:32]...),
		ProofHMAC:  append([]byte(nil), b[32:64]...),
		SinceSeq:   binary.BigEndian.Uint64(b[64:72]),
	}, nil
}

// VerifyHandshake recomputes the HMAC with the publisher's stored symkey and
// compares in constant time.
func VerifyHandshake(symkey []byte, hs *Handshake) error {
	h := hmac.New(sha256.New, symkey)
	h.Write([]byte(HandshakeHMACContext))
	h.Write(hs.ProofNonce)
	expected := h.Sum(nil)
	if !hmac.Equal(expected, hs.ProofHMAC) {
		return errors.New("handshake HMAC mismatch")
	}
	return nil
}

// ClipStartPayload is the first envelope of a clip transmission.
type ClipStartPayload struct {
	Seq         uint64            `cbor:"seq"`
	TS          int64             `cbor:"ts"`
	Kind        string            `cbor:"kind"`
	ClipID      uint64            `cbor:"clip_id"`
	Filename    string            `cbor:"filename"`
	ContentType string            `cbor:"content_type"`
	Metadata    map[string]string `cbor:"metadata"`
	TotalSize   uint64            `cbor:"total_size"`
	ChunkCount  uint32            `cbor:"chunk_count"`
}

// ClipChunkPayload carries one 1-MiB-or-less slice of a clip's body.
type ClipChunkPayload struct {
	Seq    uint64 `cbor:"seq"`
	Kind   string `cbor:"kind"`
	ClipID uint64 `cbor:"clip_id"`
	Index  uint32 `cbor:"index"`
	Data   []byte `cbor:"data"`
}

// ClipEndPayload signals clip completion and carries the plaintext SHA-256
// for assembly verification.
type ClipEndPayload struct {
	Seq    uint64 `cbor:"seq"`
	Kind   string `cbor:"kind"`
	ClipID uint64 `cbor:"clip_id"`
	SHA256 []byte `cbor:"sha256"`
}

// GapPayload tells the follower to advance past a seq that can no longer be
// replayed (e.g., the source clip was deleted before retransmit).
type GapPayload struct {
	Seq  uint64 `cbor:"seq"`
	Kind string `cbor:"kind"`
}

// MarshalPayload CBOR-encodes any payload struct.
func MarshalPayload(v any) ([]byte, error) { return cbor.Marshal(v) }

// UnmarshalPayload CBOR-decodes into dst.
func UnmarshalPayload(b []byte, dst any) error { return cbor.Unmarshal(b, dst) }

// PeekPayloadKind decodes just the "kind" field from a CBOR-encoded payload,
// returning the kind string and the original bytes for a second pass.
func PeekPayloadKind(b []byte) (string, []byte, error) {
	var peek struct {
		Kind string `cbor:"kind"`
	}
	if err := cbor.Unmarshal(b, &peek); err != nil {
		return "", nil, err
	}
	return peek.Kind, b, nil
}
