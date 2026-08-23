package app

import (
	"database/sql"
	"fmt"
)

// RingRow is one row of share_ring as returned by queries.
type RingRow struct {
	Seq           uint64
	Kind          string
	EnvelopeBytes []byte
}

// RingRowMeta is one ring row minus its envelope blob — everything the
// catch-up planner reads, and nothing it does not.
type RingRowMeta struct {
	Seq     uint64
	Kind    string
	ByteLen int64
}

// RingInsert appends an envelope to the ring for publicationID.
func RingInsert(db *sql.DB, publicationID int64, seq uint64, kind string, envelopeBytes []byte, tsUnix int64) error {
	_, err := db.Exec(
		`INSERT INTO share_ring (publication_id, seq, kind, envelope_bytes, ts) VALUES (?, ?, ?, ?, ?)`,
		publicationID, int64(seq), kind, envelopeBytes, tsUnix,
	)
	if err != nil {
		return fmt.Errorf("ring insert: %w", err)
	}
	return nil
}

// RingRetransmitMeta returns the metadata of every row with seq > sinceSeq
// still within the 1h TTL relative to nowUnix, ordered by seq. This is the
// first half of handshake catch-up: plan the batch from metadata, then fetch
// only the chosen batch's blobs with RingFetchRange.
//
// Splitting it this way is what bounds catch-up memory. The ring holds up to
// RingBytesCapPerPub (256 MiB) per publication, so loading every surviving
// envelope — as a single SELECT of envelope_bytes did — cost up to 256 MiB per
// in-flight handshake. pub.fmu serialises handshakes within one publication
// but not across them, so N publications reconnecting at once could allocate
// N × 256 MiB from authorized traffic alone.
//
// The new peak is one metadata slice plus one batch of blobs per concurrent
// handshake: metadata is ~40 B per row (a full ring of chunked clips is a few
// hundred rows; one of tiny clips a few thousand), and the batch is bounded by
// CatchupHardBytesCap (~32 MiB) and CatchupHardEnvelopesCap. So N concurrent
// handshakes now peak at roughly N × 32 MiB rather than N × 256 MiB.
//
// LENGTH() on a bare BLOB column is answered from the record header, so this
// query does not read the blobs it measures.
// rowCap bounds how many metadata rows one call may materialize: the 256 MiB
// ring cap bounds envelope BYTES, not row count, so a ring of minimal clips
// can hold millions of rows and even 40-byte metadata structs would add up to
// hundreds of MiB per handshake. The query fetches rowCap+1 so the caller
// learns (via the truncated return) that more survivors exist beyond the
// window; the planner then marks the batch truncated and the follower pages
// through the backlog across drain-close/reconnect cycles, exactly as it
// already does for byte-truncated batches.
func RingRetransmitMeta(db *sql.DB, publicationID int64, sinceSeq uint64, nowUnix int64, rowCap int) ([]RingRowMeta, bool, error) {
	cutoff := nowUnix - RingTTLSeconds
	rows, err := db.Query(
		`SELECT seq, kind, LENGTH(envelope_bytes) FROM share_ring
          WHERE publication_id = ?
            AND seq > ?
            AND ts >= ?
          ORDER BY seq
          LIMIT ?`,
		publicationID, int64(sinceSeq), cutoff, rowCap+1,
	)
	if err != nil {
		return nil, false, fmt.Errorf("ring retransmit meta query: %w", err)
	}
	defer rows.Close()
	var out []RingRowMeta
	for rows.Next() {
		var r RingRowMeta
		var seqI int64
		if err := rows.Scan(&seqI, &r.Kind, &r.ByteLen); err != nil {
			return nil, false, err
		}
		r.Seq = uint64(seqI)
		out = append(out, r)
	}
	if err := rows.Err(); err != nil {
		return nil, false, err
	}
	truncated := len(out) > rowCap
	if truncated {
		out = out[:rowCap]
	}
	return out, truncated, nil
}

// RingFetchRange returns the full rows for the inclusive seq span
// startSeq..endSeq that are still within the 1h TTL relative to nowUnix,
// ordered by seq. The caller passes the same nowUnix it gave
// RingRetransmitMeta, so both halves of a catch-up see one TTL cutoff and a
// row cannot age out between them.
//
// The span is the planned batch, so what this loads is bounded by the
// catch-up caps rather than by the ring.
func RingFetchRange(db *sql.DB, publicationID int64, startSeq, endSeq uint64, nowUnix int64) ([]RingRow, error) {
	cutoff := nowUnix - RingTTLSeconds
	rows, err := db.Query(
		`SELECT seq, kind, envelope_bytes FROM share_ring
          WHERE publication_id = ?
            AND seq >= ?
            AND seq <= ?
            AND ts >= ?
          ORDER BY seq`,
		publicationID, int64(startSeq), int64(endSeq), cutoff,
	)
	if err != nil {
		return nil, fmt.Errorf("ring fetch range query: %w", err)
	}
	defer rows.Close()
	var out []RingRow
	for rows.Next() {
		var r RingRow
		var seqI int64
		if err := rows.Scan(&seqI, &r.Kind, &r.EnvelopeBytes); err != nil {
			return nil, err
		}
		r.Seq = uint64(seqI)
		out = append(out, r)
	}
	return out, rows.Err()
}

// RingEvict deletes (1) every row older than the 1h TTL, then (2) for any
// publication over the per-publication byte cap, drops oldest rows until under
// cap. Call after each emission and from the periodic sweeper.
func RingEvict(db *sql.DB, nowUnix int64, bytesCapPerPub int64) error {
	// (1) Age eviction — global.
	cutoff := nowUnix - RingTTLSeconds
	if _, err := db.Exec(`DELETE FROM share_ring WHERE ts < ?`, cutoff); err != nil {
		return fmt.Errorf("ring age evict: %w", err)
	}
	// (2) Per-publication byte cap.
	rows, err := db.Query(`SELECT publication_id, SUM(LENGTH(envelope_bytes)) FROM share_ring GROUP BY publication_id`)
	if err != nil {
		return fmt.Errorf("ring byte query: %w", err)
	}
	defer rows.Close()
	type overCap struct {
		pubID int64
		bytes int64
	}
	var over []overCap
	for rows.Next() {
		var pid, size int64
		if err := rows.Scan(&pid, &size); err != nil {
			return err
		}
		if size > bytesCapPerPub {
			over = append(over, overCap{pubID: pid, bytes: size})
		}
	}
	rows.Close()
	for _, o := range over {
		if err := trimOldestForPub(db, o.pubID, o.bytes-bytesCapPerPub); err != nil {
			return err
		}
	}
	return nil
}

// trimOldestForPub deletes rows starting from the oldest seq for this
// publication until at least `bytesToDrop` have been freed.
func trimOldestForPub(db *sql.DB, pubID, bytesToDrop int64) error {
	freed := int64(0)
	for freed < bytesToDrop {
		var id int64
		var size int64
		err := db.QueryRow(
			`SELECT id, LENGTH(envelope_bytes) FROM share_ring WHERE publication_id = ? ORDER BY seq ASC LIMIT 1`,
			pubID,
		).Scan(&id, &size)
		if err == sql.ErrNoRows {
			return nil
		}
		if err != nil {
			return err
		}
		if _, err := db.Exec(`DELETE FROM share_ring WHERE id = ?`, id); err != nil {
			return err
		}
		freed += size
	}
	return nil
}

// RingBytesForPub returns the total envelope byte footprint of a publication.
func RingBytesForPub(db *sql.DB, publicationID int64) (int64, error) {
	var n int64
	err := db.QueryRow(
		`SELECT COALESCE(SUM(LENGTH(envelope_bytes)),0) FROM share_ring WHERE publication_id = ?`,
		publicationID,
	).Scan(&n)
	return n, err
}
