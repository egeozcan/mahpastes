package app

import (
	"crypto/rand"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"time"
)

// Share links are revocable, single-clip public URLs served at GET /s/{token}.
// The unguessable token IS the capability — the view route is intentionally
// unauthenticated. Management (mint/list/revoke) is admin-only over the REST
// API. This is deliberately distinct from the peer-to-peer "share" feature in
// share_*.go (ShareManager / mp-share: strings), which syncs whole tags between
// instances and has nothing to do with HTTP links.

const (
	// shareTokenBytes is the entropy of a share-link token: 32 bytes (256 bits)
	// of crypto/rand, hex-encoded into the URL path. The token is the sole
	// capability granting access to the linked clip, so it is sized like the
	// tag-serve key rather than the 16-byte API key.
	shareTokenBytes = 32

	// shareViewRatePerMin bounds anonymous /s/{token} hits per client IP.
	// Generous for legitimate use (a link is one clip, one navigation) while
	// throttling token enumeration on a network-exposed daemon.
	shareViewRatePerMin = 60
)

// ShareLinkInfo is the public description of a share link. It never includes the
// token itself — only an 8-char prefix for recognition in management UIs.
type ShareLinkInfo struct {
	ID            int64   `json:"id"`
	ClipID        int64   `json:"clip_id"`
	TokenPrefix   string  `json:"token_prefix"`
	Name          string  `json:"name"`
	IsRevoked     bool    `json:"is_revoked"`
	ExpiresAt     *string `json:"expires_at"`
	MaxDownloads  *int64  `json:"max_downloads"`
	DownloadCount int64   `json:"download_count"`
	CreatedAt     string  `json:"created_at"`
	LastUsedAt    *string `json:"last_used_at"`
}

// ShareLinkCreateResult is returned when a link is minted, with the plaintext
// token (and its URL path) shown exactly once. Only the token's SHA-256 hash is
// persisted, so a DB leak yields no working links.
type ShareLinkCreateResult struct {
	Token string        `json:"token"`
	Path  string        `json:"path"` // "/s/<token>" — prepend the public origin
	Info  ShareLinkInfo `json:"info"`
}

// CreateShareLink mints a revocable public link to a single clip. createdBy is
// the API key id of the minter for audit (0 = none, e.g. the desktop user). The
// plaintext token is returned once; only its SHA-256 hash is stored.
func (am *APIManager) CreateShareLink(clipID int64, name string, expiresInSeconds, maxDownloads, createdBy int64) (*ShareLinkCreateResult, error) {
	name = strings.TrimSpace(name)

	var exists int
	if err := am.app.db.QueryRow("SELECT COUNT(*) FROM clips WHERE id = ?", clipID).Scan(&exists); err != nil || exists == 0 {
		return nil, fmt.Errorf("clip %d not found", clipID)
	}

	tokenBytes := make([]byte, shareTokenBytes)
	if _, err := rand.Read(tokenBytes); err != nil {
		return nil, fmt.Errorf("failed to generate token: %w", err)
	}
	plaintext := hex.EncodeToString(tokenBytes)
	prefix := plaintext[:8] + "..."
	hash := sha256.Sum256([]byte(plaintext))
	tokenHash := hex.EncodeToString(hash[:])

	// Store expiry as UTC text in the same format SQLite's CURRENT_TIMESTAMP /
	// datetime() produce, so the comparison in handleShareView is valid in any
	// server timezone. Binding a raw time.Time makes the driver serialize a
	// local-offset RFC3339 string that does not compare correctly against
	// CURRENT_TIMESTAMP.
	var expiresAt *string
	if expiresInSeconds > 0 {
		s := time.Now().UTC().Add(time.Duration(expiresInSeconds) * time.Second).Format("2006-01-02 15:04:05")
		expiresAt = &s
	}
	var maxDownloadsPtr *int64
	if maxDownloads > 0 {
		maxDownloadsPtr = &maxDownloads
	}
	var createdByPtr *int64
	if createdBy > 0 {
		createdByPtr = &createdBy
	}

	result, err := am.app.db.Exec(
		`INSERT INTO share_links (clip_id, token_hash, token_prefix, name, created_by, expires_at, max_downloads)
		 VALUES (?, ?, ?, ?, ?, ?, ?)`,
		clipID, tokenHash, prefix, name, createdByPtr, expiresAt, maxDownloadsPtr,
	)
	if err != nil {
		return nil, fmt.Errorf("failed to create share link: %w", err)
	}
	id, _ := result.LastInsertId()

	info, err := am.getShareLinkInfo(id)
	if err != nil {
		return nil, err
	}
	return &ShareLinkCreateResult{
		Token: plaintext,
		Path:  "/s/" + plaintext,
		Info:  *info,
	}, nil
}

const shareLinkSelectCols = `id, clip_id, token_prefix, name, is_revoked, expires_at, max_downloads, download_count, created_at, last_used_at`

// rowScanner is satisfied by both *sql.Row and *sql.Rows.
type rowScanner interface {
	Scan(dest ...interface{}) error
}

func scanShareLink(s rowScanner) (*ShareLinkInfo, error) {
	var info ShareLinkInfo
	var name, expiresAt, lastUsedAt sql.NullString
	var maxDownloads sql.NullInt64
	var isRevoked int
	if err := s.Scan(&info.ID, &info.ClipID, &info.TokenPrefix, &name, &isRevoked,
		&expiresAt, &maxDownloads, &info.DownloadCount, &info.CreatedAt, &lastUsedAt); err != nil {
		return nil, err
	}
	info.Name = name.String
	info.IsRevoked = isRevoked == 1
	if expiresAt.Valid {
		info.ExpiresAt = &expiresAt.String
	}
	if lastUsedAt.Valid {
		info.LastUsedAt = &lastUsedAt.String
	}
	if maxDownloads.Valid {
		v := maxDownloads.Int64
		info.MaxDownloads = &v
	}
	return &info, nil
}

func (am *APIManager) getShareLinkInfo(id int64) (*ShareLinkInfo, error) {
	row := am.app.db.QueryRow("SELECT "+shareLinkSelectCols+" FROM share_links WHERE id = ?", id)
	return scanShareLink(row)
}

// ListShareLinks returns all share links (active and revoked), newest first.
func (am *APIManager) ListShareLinks() ([]ShareLinkInfo, error) {
	rows, err := am.app.db.Query("SELECT " + shareLinkSelectCols + " FROM share_links ORDER BY created_at DESC")
	if err != nil {
		return nil, fmt.Errorf("failed to list share links: %w", err)
	}
	defer rows.Close()

	links := []ShareLinkInfo{}
	for rows.Next() {
		info, err := scanShareLink(rows)
		if err != nil {
			return nil, fmt.Errorf("failed to scan share link: %w", err)
		}
		links = append(links, *info)
	}
	return links, rows.Err()
}

// RevokeShareLink soft-deletes a share link. Revocation is instant: the view
// path re-checks is_revoked in SQL on every request with no cache, so the next
// hit on the token 404s.
func (am *APIManager) RevokeShareLink(id int64) error {
	result, err := am.app.db.Exec("UPDATE share_links SET is_revoked = 1 WHERE id = ? AND is_revoked = 0", id)
	if err != nil {
		return fmt.Errorf("failed to revoke share link: %w", err)
	}
	rows, _ := result.RowsAffected()
	if rows == 0 {
		return fmt.Errorf("share link not found or already revoked")
	}
	return nil
}

// --- HTTP handlers ---

func (am *APIManager) handleCreateShareLink(w http.ResponseWriter, r *http.Request) {
	keyCtx := getKeyContext(r)
	var body struct {
		ClipID           int64  `json:"clip_id"`
		Name             string `json:"name"`
		ExpiresInSeconds int64  `json:"expires_in_seconds"`
		MaxDownloads     int64  `json:"max_downloads"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		am.jsonError(w, http.StatusBadRequest, "invalid JSON body")
		return
	}
	if body.ClipID <= 0 {
		am.jsonError(w, http.StatusBadRequest, "clip_id is required")
		return
	}
	// A tag-scoped admin key may only mint links for clips inside its subtree.
	if err := am.enforceTagScope(keyCtx, body.ClipID); err != nil {
		am.jsonError(w, http.StatusForbidden, err.Error())
		return
	}
	result, err := am.CreateShareLink(body.ClipID, body.Name, body.ExpiresInSeconds, body.MaxDownloads, keyCtx.KeyID)
	if err != nil {
		am.jsonError(w, http.StatusBadRequest, err.Error())
		return
	}
	// Set Content-Type before WriteHeader — afterwards it is silently dropped.
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusCreated)
	json.NewEncoder(w).Encode(result)
}

func (am *APIManager) handleListShareLinks(w http.ResponseWriter, r *http.Request) {
	keyCtx := getKeyContext(r)
	links, err := am.ListShareLinks()
	if err != nil {
		am.jsonError(w, http.StatusInternalServerError, err.Error())
		return
	}
	// A tag-scoped admin key only sees links whose clip is inside its subtree,
	// matching the scope already enforced when minting.
	if keyCtx.ScopedTagID != 0 {
		scoped := make([]ShareLinkInfo, 0, len(links))
		for _, l := range links {
			if am.enforceTagScope(keyCtx, l.ClipID) == nil {
				scoped = append(scoped, l)
			}
		}
		links = scoped
	}
	am.jsonOK(w, links)
}

func (am *APIManager) handleRevokeShareLink(w http.ResponseWriter, r *http.Request) {
	keyCtx := getKeyContext(r)
	id, err := parseIntParam(r.PathValue("id"))
	if err != nil {
		am.jsonError(w, http.StatusBadRequest, "invalid link id")
		return
	}
	// A tag-scoped key may only revoke links whose clip is inside its subtree.
	// Use a uniform 404 for "no such link" and "out of scope" so the endpoint is
	// not an existence oracle for out-of-scope links.
	if keyCtx.ScopedTagID != 0 {
		var clipID int64
		if err := am.app.db.QueryRow("SELECT clip_id FROM share_links WHERE id = ?", id).Scan(&clipID); err != nil {
			am.jsonError(w, http.StatusNotFound, "share link not found")
			return
		}
		if err := am.enforceTagScope(keyCtx, clipID); err != nil {
			am.jsonError(w, http.StatusNotFound, "share link not found")
			return
		}
	}
	if err := am.RevokeShareLink(id); err != nil {
		am.jsonError(w, http.StatusNotFound, err.Error())
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// shareNotFound writes a uniform 404 for every rejection reason — missing,
// revoked, expired, download-exhausted, or clip-gone — so the endpoint is not an
// oracle for which tokens exist.
func shareNotFound(w http.ResponseWriter) {
	http.Error(w, "Not found", http.StatusNotFound)
}

// handleShareView serves a clip's bytes to anyone holding a valid token. It is
// mounted WITHOUT authMiddleware: the 256-bit token is the capability. Every
// gate (revoked, expired, download cap, clip archived/expired) is re-evaluated
// in SQL on each request with no in-memory cache, which is what makes
// revocation and expiry take effect instantly.
func (am *APIManager) handleShareView(w http.ResponseWriter, r *http.Request) {
	if !am.shareLimiter.allow(am.clientIP(r), shareViewRatePerMin, time.Minute) {
		http.Error(w, "Too Many Requests", http.StatusTooManyRequests)
		return
	}

	token := r.PathValue("token")
	if token == "" {
		shareNotFound(w)
		return
	}
	hash := sha256.Sum256([]byte(token))
	tokenHash := hex.EncodeToString(hash[:])

	// One gated lookup. The clip-lifecycle clauses make a link stop serving once
	// the underlying clip is archived or expires, so a link never outlives its
	// clip's intended visibility. datetime() normalizes both stored timestamps
	// and 'now' to a comparable UTC form regardless of the server timezone.
	var linkID, clipID int64
	err := am.app.db.QueryRow(`
		SELECT sl.id, sl.clip_id
		FROM share_links sl
		JOIN clips c ON c.id = sl.clip_id
		WHERE sl.token_hash = ?
		  AND sl.is_revoked = 0
		  AND (sl.expires_at IS NULL OR datetime(sl.expires_at) > datetime('now'))
		  AND (sl.max_downloads IS NULL OR sl.download_count < sl.max_downloads)
		  AND c.is_archived = 0
		  AND (c.expires_at IS NULL OR datetime(c.expires_at) > datetime('now'))`,
		tokenHash,
	).Scan(&linkID, &clipID)
	if err != nil {
		shareNotFound(w)
		return
	}

	// A revoked link must never be served from a shared/browser cache.
	w.Header().Set("Cache-Control", "no-store")

	// Go's ServeMux dispatches HEAD to this GET route. A HEAD (e.g. a link-unfurl
	// bot when the URL is pasted into a chat) must not consume a download slot, or
	// a max_downloads=1 "one-time" link would be drained before the human clicks.
	// The gate above already 404s invalid/revoked/expired/exhausted links.
	if r.Method == http.MethodHead {
		w.WriteHeader(http.StatusOK)
		return
	}

	// Atomically claim a download slot. The WHERE clause re-checks the cap so two
	// concurrent requests at the limit cannot both succeed (rides WAL +
	// busy_timeout). 0 rows affected = the cap was hit between the SELECT and now.
	res, err := am.app.db.Exec(`
		UPDATE share_links
		SET download_count = download_count + 1, last_used_at = CURRENT_TIMESTAMP
		WHERE id = ? AND (max_downloads IS NULL OR download_count < max_downloads)`,
		linkID,
	)
	if err != nil {
		shareNotFound(w)
		return
	}
	if n, _ := res.RowsAffected(); n == 0 {
		shareNotFound(w)
		return
	}

	if !am.writeWholeClipBytes(w, r, clipID) {
		// Clip was deleted between the gate check and the read. Refund the slot so
		// a one-time link is not permanently bricked by a non-delivery.
		am.app.db.Exec("UPDATE share_links SET download_count = download_count - 1 WHERE id = ?", linkID)
		shareNotFound(w)
	}
}
