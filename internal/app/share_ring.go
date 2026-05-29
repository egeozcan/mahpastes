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

// RingRetransmit returns rows with seq > sinceSeq that are within the 1h TTL
// relative to nowUnix, ordered by seq. Used on handshake catch-up.
func RingRetransmit(db *sql.DB, publicationID int64, sinceSeq uint64, nowUnix int64) ([]RingRow, error) {
	cutoff := nowUnix - RingTTLSeconds
	rows, err := db.Query(
		`SELECT seq, kind, envelope_bytes FROM share_ring
          WHERE publication_id = ?
            AND seq > ?
            AND ts >= ?
          ORDER BY seq`,
		publicationID, int64(sinceSeq), cutoff,
	)
	if err != nil {
		return nil, fmt.Errorf("ring retransmit query: %w", err)
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
