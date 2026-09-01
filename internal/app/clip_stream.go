package app

import (
	"database/sql"
	"errors"
	"fmt"
	"mime"
	"net/http"
	"strconv"
	"strings"
)

const clipStreamChunkSize int64 = 1024 * 1024

type clipStreamMetadata struct {
	contentType string
	filename    sql.NullString
	size        int64
}

func loadClipStreamMetadata(db *sql.DB, clipID int64) (clipStreamMetadata, error) {
	return scanClipStreamMetadata(db.QueryRow(
		"SELECT content_type, filename, LENGTH(data) FROM clips WHERE id = ?", clipID))
}

func loadClipStreamMetadataTx(tx *sql.Tx, clipID int64) (clipStreamMetadata, error) {
	return scanClipStreamMetadata(tx.QueryRow(
		"SELECT content_type, filename, LENGTH(data) FROM clips WHERE id = ?", clipID))
}

func scanClipStreamMetadata(row *sql.Row) (clipStreamMetadata, error) {
	var metadata clipStreamMetadata
	err := row.Scan(&metadata.contentType, &metadata.filename, &metadata.size)
	return metadata, err
}

// serveStoredClip streams a database-backed clip with bounded memory, reading
// it in chunks rather than allocating the whole blob.
//
// allowRanges controls single-range support. Turn it off for any caller that
// has already spent something to serve this response — a share link claims a
// download slot before the body is written, and a range request would let one
// byte consume it.
func serveStoredClip(w http.ResponseWriter, r *http.Request, db *sql.DB, clipID int64, disposition string, allowRanges bool) bool {
	// One read transaction for the metadata and every chunk. Without it the
	// length is read from one revision and the chunks from another, so a clip
	// edited mid-download is delivered as a splice of both — or truncated while
	// still advertising the original Content-Length.
	tx, err := db.BeginTx(r.Context(), &sql.TxOptions{ReadOnly: true})
	if err != nil {
		return false
	}
	defer tx.Rollback()

	metadata, err := loadClipStreamMetadataTx(tx, clipID)
	if err != nil {
		return false
	}

	w.Header().Set("X-Content-Type-Options", "nosniff")
	w.Header().Set("Content-Security-Policy", "default-src 'none'; sandbox")
	if allowRanges {
		w.Header().Set("Accept-Ranges", "bytes")
	}
	if metadata.contentType != "" {
		w.Header().Set("Content-Type", metadata.contentType)
	}
	name := sanitizeDownloadName(metadata.filename.String, clipID)
	if disposition == "attachment" {
		w.Header().Set("Content-Disposition", attachmentDisposition(name))
	} else {
		w.Header().Set("Content-Disposition", mime.FormatMediaType("inline", map[string]string{"filename": name}))
	}

	rangeHeader := r.Header.Get("Range")
	if !allowRanges {
		rangeHeader = ""
	}
	start, end, partial, rangeErr := parseClipByteRange(rangeHeader, metadata.size)
	if rangeErr != nil {
		w.Header().Set("Content-Range", fmt.Sprintf("bytes */%d", metadata.size))
		http.Error(w, "requested range not satisfiable", http.StatusRequestedRangeNotSatisfiable)
		return true
	}

	length := end - start + 1
	w.Header().Set("Content-Length", strconv.FormatInt(length, 10))
	if partial {
		w.Header().Set("Content-Range", fmt.Sprintf("bytes %d-%d/%d", start, end, metadata.size))
		w.WriteHeader(http.StatusPartialContent)
	}
	if r.Method == http.MethodHead || length == 0 {
		return true
	}

	for offset := start; offset <= end; {
		chunkLength := min(clipStreamChunkSize, end-offset+1)
		var chunk []byte
		if err := tx.QueryRow(
			"SELECT SUBSTR(data, ?, ?) FROM clips WHERE id = ?", offset+1, chunkLength, clipID,
		).Scan(&chunk); err != nil || len(chunk) == 0 {
			return true
		}
		if _, err := w.Write(chunk); err != nil {
			return true
		}
		offset += int64(len(chunk))
	}
	return true
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
