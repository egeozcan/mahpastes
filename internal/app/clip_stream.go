package app

import (
	"bytes"
	"context"
	"database/sql"
	"errors"
	"fmt"
	"io"
	"log"
	"mime"
	"net/http"
	"strconv"
	"strings"
	"time"
)

var (
	// clipStreamWriteTimeout bounds how long one write of a clip body may
	// block. The API server runs with WriteTimeout 0 so the SSE stream can stay
	// open, which means nothing else ever times out a response: a client that
	// stops reading — a paused download, or someone doing it on purpose — would
	// keep its handler parked in Write for as long as it keeps the TCP
	// connection open. The deadline slides forward before every chunk, so a
	// slow but moving reader is fine (it needs to drain one chunk per window:
	// 64 KB here, ~2 KB/s at the default; 32 KB for tag-serve's
	// slidingDeadlineReader, ~1 KB/s) and only a stalled one is cut off.
	clipStreamWriteTimeout = 30 * time.Second

	// clipStreamInMemoryMax is the largest clip served straight from memory:
	// read whole in one statement, so the database is released before the
	// first byte goes out. Anything bigger is served from an on-disk snapshot
	// (clip_snapshot.go) instead of being buffered per request.
	clipStreamInMemoryMax int64 = 1 << 20
)

// clipStreamCopyChunk is the unit each write deadline covers.
const clipStreamCopyChunk = 64 * 1024

type clipStreamMetadata struct {
	contentType string
	filename    sql.NullString
	size        int64
}

func loadClipStreamMetadata(db *sql.DB, clipID int64) (clipStreamMetadata, error) {
	return scanClipStreamMetadata(db.QueryRow(
		"SELECT content_type, filename, LENGTH(data) FROM clips WHERE id = ?", clipID))
}

func scanClipStreamMetadata(row *sql.Row) (clipStreamMetadata, error) {
	var metadata clipStreamMetadata
	err := row.Scan(&metadata.contentType, &metadata.filename, &metadata.size)
	return metadata, err
}

// clipBody is one revision of a clip's bytes, detached from the database:
// either a small clip held in memory or an open snapshot file. Nothing that
// serves it holds a connection or a read transaction.
type clipBody struct {
	clipStreamMetadata
	data   io.ReaderAt // nil when only the headers are wanted (HEAD)
	closer func()
	// hash is the SHA-256 (hex) of exactly these bytes — for HEAD, the
	// stored content_hash — or "" when unknown. It identifies the revision,
	// which makes it a strong validator (ETag).
	hash string
}

func (b *clipBody) Close() {
	if b.closer != nil {
		b.closer()
	}
}

// loadClipBodyMetadata reads what a response needs before it has the bytes,
// plus the content hash that identifies the revision. octet_length rather than
// LENGTH because it counts bytes for a TEXT value too, and SQLite answers both
// from the record header without loading the blob.
func loadClipBodyMetadata(ctx context.Context, db *sql.DB, clipID int64) (clipStreamMetadata, string, error) {
	var meta clipStreamMetadata
	var hash string
	err := db.QueryRowContext(ctx,
		"SELECT content_type, filename, octet_length(data), COALESCE(content_hash, '') FROM clips WHERE id = ?",
		clipID,
	).Scan(&meta.contentType, &meta.filename, &meta.size, &hash)
	if errors.Is(err, sql.ErrNoRows) {
		return meta, "", ErrClipNotFound
	}
	return meta, hash, err
}

// openClipBody returns the clip's bytes in a form that can be written to a
// client without touching the database again. headOnly skips the bytes.
//
// Large clips must not be streamed out of SQLite. It cannot seek into a blob:
// every SUBSTR(data, ...) loads the whole value (~22 ms on a 100 MB clip at any
// offset), so a chunked download is quadratic in CPU and holds clip-size memory
// per chunk in flight, and keeping the chunks consistent meant holding a read
// transaction across every network write — which a client that stopped
// reading could then pin, with its WAL snapshot, forever. A snapshot costs one
// O(n) read per revision; see clip_snapshot.go. Without a TempClipStore (a
// misconfigured or minimal setup) each request gets a private copy instead:
// still no transaction held across writes, just no reuse.
func openClipBody(ctx context.Context, db *sql.DB, store *TempClipStore, clipID int64, headOnly bool) (*clipBody, error) {
	meta, hash, err := loadClipBodyMetadata(ctx, db, clipID)
	if err != nil {
		return nil, err
	}
	if headOnly {
		return &clipBody{clipStreamMetadata: meta, hash: hash}, nil
	}
	if meta.size <= clipStreamInMemoryMax {
		var data []byte
		body := &clipBody{}
		err := db.QueryRowContext(ctx,
			"SELECT content_type, filename, data FROM clips WHERE id = ?", clipID,
		).Scan(&body.contentType, &body.filename, &data)
		if errors.Is(err, sql.ErrNoRows) {
			return nil, ErrClipNotFound
		}
		if err != nil {
			return nil, err
		}
		body.size = int64(len(data))
		body.data = bytes.NewReader(data)
		body.hash = computeContentHash(data)
		return body, nil
	}
	if store != nil && store.db != nil && store.dir != "" {
		return store.openSnapshot(ctx, clipID, meta, hash)
	}
	return openPrivateClipSnapshot(ctx, db, clipID)
}

// serveStoredClip writes a database-backed clip with the stored-XSS headers
// (see writeClipBytes). It returns false, having written nothing, when the clip
// could not be read — normally because it no longer exists — so the caller can
// answer in its own terms (and a share link can refund its download slot).
//
// allowRanges controls single-range support. Turn it off for any caller that
// has already spent something to serve this response — a share link claims a
// download slot before the body is written, and a range request would let one
// byte consume it.
func serveStoredClip(w http.ResponseWriter, r *http.Request, db *sql.DB, store *TempClipStore, clipID int64, disposition string, allowRanges bool) bool {
	// Headers and body come from the same revision: the body carries the
	// metadata read alongside its bytes, so a clip edited mid-request is
	// delivered whole as one revision or the other, never spliced or truncated
	// under the original Content-Length.
	body, err := openClipBody(r.Context(), db, store, clipID, r.Method == http.MethodHead)
	if err != nil {
		if !errors.Is(err, ErrClipNotFound) && !errors.Is(err, context.Canceled) {
			log.Printf("clip stream: clip %d: %v", clipID, err)
		}
		return false
	}
	defer body.Close()

	w.Header().Set("X-Content-Type-Options", "nosniff")
	w.Header().Set("Content-Security-Policy", "default-src 'none'; sandbox")
	if allowRanges {
		w.Header().Set("Accept-Ranges", "bytes")
	}
	if body.contentType != "" {
		w.Header().Set("Content-Type", body.contentType)
	}
	name := sanitizeDownloadName(body.filename.String, clipID)
	if disposition == "attachment" {
		w.Header().Set("Content-Disposition", attachmentDisposition(name))
	} else {
		w.Header().Set("Content-Disposition", mime.FormatMediaType("inline", map[string]string{"filename": name}))
	}

	rangeHeader := r.Header.Get("Range")
	if !allowRanges {
		rangeHeader = ""
	}
	start, end, partial, rangeErr := parseClipByteRange(rangeHeader, body.size)
	if rangeErr != nil {
		w.Header().Set("Content-Range", fmt.Sprintf("bytes */%d", body.size))
		http.Error(w, "requested range not satisfiable", http.StatusRequestedRangeNotSatisfiable)
		return true
	}

	length := end - start + 1
	w.Header().Set("Content-Length", strconv.FormatInt(length, 10))
	if partial {
		w.Header().Set("Content-Range", fmt.Sprintf("bytes %d-%d/%d", start, end, body.size))
		w.WriteHeader(http.StatusPartialContent)
	}
	if r.Method == http.MethodHead || length == 0 {
		return true
	}

	// A failed write means the client went away or stopped reading; the short
	// body against the declared Content-Length makes net/http drop the
	// connection, which is all there is left to do.
	_ = copyClipBody(w, io.NewSectionReader(body.data, start, length))
	return true
}

// copyClipBody writes src to w, sliding a write deadline forward before each
// chunk so a client that stops reading is cut off after clipStreamWriteTimeout
// instead of holding the handler forever (see clipStreamWriteTimeout).
func copyClipBody(w http.ResponseWriter, src io.Reader) error {
	rc := http.NewResponseController(w)
	// Recorders and wrappers that cannot set deadlines report ErrNotSupported;
	// the copy itself still works.
	setDeadline := func(t time.Time) { _ = rc.SetWriteDeadline(t) }

	buf := make([]byte, clipStreamCopyChunk)
	for {
		n, readErr := src.Read(buf)
		if n > 0 {
			setDeadline(time.Now().Add(clipStreamWriteTimeout))
			if _, err := w.Write(buf[:n]); err != nil {
				return err
			}
		}
		if readErr == io.EOF {
			break
		}
		if readErr != nil {
			return readErr
		}
	}

	// Push the buffered tail out under a fresh deadline too, so nothing is
	// left for net/http's own final flush to block on. The deadline is left in
	// place: net/http clears the connection's write deadline once the response
	// is finished, before reusing it for the next keep-alive request.
	setDeadline(time.Now().Add(clipStreamWriteTimeout))
	if err := rc.Flush(); err != nil && !errors.Is(err, http.ErrNotSupported) {
		return err
	}
	return nil
}

func parseClipByteRange(header string, size int64) (start, end int64, partial bool, err error) {
	if header == "" {
		if size == 0 {
			return 0, -1, false, nil
		}
		return 0, size - 1, false, nil
	}
	if size <= 0 || !strings.HasPrefix(header, "bytes=") || strings.Contains(header, ",") {
		return 0, 0, false, errors.New("invalid byte range")
	}

	parts := strings.SplitN(strings.TrimPrefix(header, "bytes="), "-", 2)
	if len(parts) != 2 || (parts[0] == "" && parts[1] == "") {
		return 0, 0, false, errors.New("invalid byte range")
	}
	if parts[0] == "" {
		suffix, parseErr := strconv.ParseInt(parts[1], 10, 64)
		if parseErr != nil || suffix <= 0 {
			return 0, 0, false, errors.New("invalid suffix range")
		}
		if suffix > size {
			suffix = size
		}
		return size - suffix, size - 1, true, nil
	}

	start, err = strconv.ParseInt(parts[0], 10, 64)
	if err != nil || start < 0 || start >= size {
		return 0, 0, false, errors.New("invalid range start")
	}
	end = size - 1
	if parts[1] != "" {
		end, err = strconv.ParseInt(parts[1], 10, 64)
		if err != nil || end < start {
			return 0, 0, false, errors.New("invalid range end")
		}
		if end >= size {
			end = size - 1
		}
	}
	return start, end, true, nil
}
