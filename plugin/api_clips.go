package plugin

import (
	"database/sql"
	"encoding/base64"
	"fmt"
	"io"
	"log"
	"net/http"
	"net/url"
	"path"
	"regexp"
	"strings"
	"time"

	lua "github.com/yuin/gopher-lua"
)

// maxContentTypeLength is the maximum length for a MIME content type string
const maxContentTypeLength = 256

// validMIMEType matches standard MIME type format (e.g. "application/json", "image/png")
var validMIMEType = regexp.MustCompile(`^[a-zA-Z0-9][a-zA-Z0-9!#$&\-^_.+]*\/[a-zA-Z0-9][a-zA-Z0-9!#$&\-^_.+]*$`)

const (
	// MaxClipDataSize is the maximum size of data a plugin can create (10MB)
	MaxClipDataSize = 10 * 1024 * 1024
	// URLFetchTimeout is the timeout for downloading content from URLs
	URLFetchTimeout = 60 * time.Second
)

// ClipsAPI provides clip CRUD operations to plugins
type ClipsAPI struct {
	db             *sql.DB
	allowedDomains map[string][]string // domain -> allowed methods (from manifest)
}

// NewClipsAPI creates a new clips API instance
func NewClipsAPI(db *sql.DB, allowedDomains map[string][]string) *ClipsAPI {
	return &ClipsAPI{db: db, allowedDomains: allowedDomains}
}

// Register adds the clips module to the Lua state
func (c *ClipsAPI) Register(L *lua.LState) {
	clipsMod := L.NewTable()

	clipsMod.RawSetString("list", L.NewFunction(c.list))
	clipsMod.RawSetString("get", L.NewFunction(c.get))
	clipsMod.RawSetString("get_data", L.NewFunction(c.getData))
	clipsMod.RawSetString("create", L.NewFunction(c.create))
	clipsMod.RawSetString("create_from_url", L.NewFunction(c.createFromURL))
	clipsMod.RawSetString("update", L.NewFunction(c.update))
	clipsMod.RawSetString("delete", L.NewFunction(c.deleteClip))
	clipsMod.RawSetString("delete_many", L.NewFunction(c.deleteMany))
	clipsMod.RawSetString("archive", L.NewFunction(c.archive))
	clipsMod.RawSetString("unarchive", L.NewFunction(c.unarchive))

	L.SetGlobal("clips", clipsMod)
}

func (c *ClipsAPI) list(L *lua.LState) int {
	// Optional filter table with content_type, limit, and offset
	var contentTypeFilter string
	limit := 100 // default limit
	offset := 0  // default offset

	if L.GetTop() >= 1 {
		if filter, ok := L.Get(1).(*lua.LTable); ok {
			if ct := filter.RawGetString("content_type"); ct != lua.LNil {
				contentTypeFilter = ct.String()
			}
			if lim := filter.RawGetString("limit"); lim != lua.LNil {
				if limNum, ok := lim.(lua.LNumber); ok {
					limit = int(limNum)
					if limit > 1000 {
						limit = 1000 // cap at 1000 to prevent abuse
					}
					if limit < 1 {
						limit = 1
					}
				}
			}
			if off := filter.RawGetString("offset"); off != lua.LNil {
				if offNum, ok := off.(lua.LNumber); ok {
					offset = int(offNum)
					if offset < 0 {
						offset = 0
					}
				}
			}
		}
	}

	query := `SELECT id, content_type, filename, created_at, is_archived
	          FROM clips WHERE (expires_at IS NULL OR expires_at > CURRENT_TIMESTAMP)`
	args := []interface{}{}

	if contentTypeFilter != "" {
		query += " AND content_type = ?"
		args = append(args, contentTypeFilter)
	}
	query += " ORDER BY created_at DESC LIMIT ? OFFSET ?"
	args = append(args, limit, offset)

	rows, err := c.db.Query(query, args...)
	if err != nil {
		L.Push(lua.LNil)
		L.Push(lua.LString(err.Error()))
		return 2
	}
	defer rows.Close()

	result := L.NewTable()
	for rows.Next() {
		var id int64
		var contentType string
		var filename sql.NullString
		var createdAt time.Time
		var isArchived int

		if err := rows.Scan(&id, &contentType, &filename, &createdAt, &isArchived); err != nil {
			log.Printf("clips.list: failed to scan row: %v", err)
			continue
		}

		clip := L.NewTable()
		clip.RawSetString("id", lua.LNumber(id))
		clip.RawSetString("content_type", lua.LString(contentType))
		clip.RawSetString("filename", lua.LString(filename.String))
		clip.RawSetString("created_at", lua.LNumber(createdAt.Unix()))
		clip.RawSetString("is_archived", lua.LBool(isArchived == 1))

		result.Append(clip)
	}

	L.Push(result)
	return 1
}

func (c *ClipsAPI) get(L *lua.LState) int {
	id := L.CheckInt64(1)

	var contentType string
	var data []byte
	var filename sql.NullString
	var createdAt time.Time
	var isArchived int

	err := c.db.QueryRow(`
		SELECT content_type, data, filename, created_at, is_archived
		FROM clips WHERE id = ?
	`, id).Scan(&contentType, &data, &filename, &createdAt, &isArchived)

	if err == sql.ErrNoRows {
		L.Push(lua.LNil)
		return 1
	}
	if err != nil {
		L.Push(lua.LNil)
		L.Push(lua.LString(err.Error()))
		return 2
	}

	clip := L.NewTable()
	clip.RawSetString("id", lua.LNumber(id))
	clip.RawSetString("content_type", lua.LString(contentType))
	clip.RawSetString("filename", lua.LString(filename.String))
	clip.RawSetString("created_at", lua.LNumber(createdAt.Unix()))
	clip.RawSetString("is_archived", lua.LBool(isArchived == 1))

	// For text content, return as-is; for binary, base64 encode
	if strings.HasPrefix(contentType, "text/") || contentType == "application/json" {
		clip.RawSetString("data", lua.LString(string(data)))
	} else {
		clip.RawSetString("data", lua.LString(base64.StdEncoding.EncodeToString(data)))
		clip.RawSetString("data_encoding", lua.LString("base64"))
	}

	L.Push(clip)
	return 1
}

// getData returns raw clip data (base64 for binary, plain for text)
// Returns: data, mime_type or nil, error
func (c *ClipsAPI) getData(L *lua.LState) int {
	id := L.CheckInt64(1)

	var contentType string
	var data []byte

	err := c.db.QueryRow(`
		SELECT content_type, data FROM clips WHERE id = ?
	`, id).Scan(&contentType, &data)

	if err == sql.ErrNoRows {
		L.Push(lua.LNil)
		L.Push(lua.LString("clip not found"))
		return 2
	}
	if err != nil {
		L.Push(lua.LNil)
		L.Push(lua.LString(err.Error()))
		return 2
	}

	// For text content, return as-is; for binary, base64 encode
	if strings.HasPrefix(contentType, "text/") || contentType == "application/json" {
		L.Push(lua.LString(string(data)))
	} else {
		L.Push(lua.LString(base64.StdEncoding.EncodeToString(data)))
	}
	L.Push(lua.LString(contentType))
	return 2
}

func (c *ClipsAPI) create(L *lua.LState) int {
	opts := L.CheckTable(1)

	dataVal := opts.RawGetString("data")
	if dataVal == lua.LNil {
		L.Push(lua.LNil)
		L.Push(lua.LString("data is required"))
		return 2
	}
	dataStr := dataVal.String()

	// Check size before processing
	if len(dataStr) > MaxClipDataSize {
		L.Push(lua.LNil)
		L.Push(lua.LString(fmt.Sprintf("data too large: %d bytes (max %d)", len(dataStr), MaxClipDataSize)))
		return 2
	}

	// Support both content_type and mime_type for flexibility
	contentType := "application/octet-stream"
	if ct := opts.RawGetString("content_type"); ct != lua.LNil {
		contentType = ct.String()
	} else if mt := opts.RawGetString("mime_type"); mt != lua.LNil {
		contentType = mt.String()
	}

	// Validate content type format
	contentType = validateContentType(contentType)

	// Support both filename and name for flexibility
	var filename string
	if fn := opts.RawGetString("filename"); fn != lua.LNil {
		filename = fn.String()
	} else if nm := opts.RawGetString("name"); nm != lua.LNil {
		filename = nm.String()
	}

	// Determine if data is base64 encoded
	// Check explicit encoding flag or auto-detect for binary content types
	isBase64 := false
	if enc := opts.RawGetString("data_encoding"); enc != lua.LNil && enc.String() == "base64" {
		isBase64 = true
	} else if !strings.HasPrefix(contentType, "text/") && contentType != "application/json" {
		// For binary content types, assume base64 if not explicitly text
		isBase64 = true
	}

	var data []byte
	if isBase64 {
		var err error
		data, err = base64.StdEncoding.DecodeString(dataStr)
		if err != nil {
			L.Push(lua.LNil)
			L.Push(lua.LString("invalid base64 data: " + err.Error()))
			return 2
		}
		// Check decoded size as well
		if len(data) > MaxClipDataSize {
			L.Push(lua.LNil)
			L.Push(lua.LString(fmt.Sprintf("decoded data too large: %d bytes (max %d)", len(data), MaxClipDataSize)))
			return 2
		}
	} else {
		data = []byte(dataStr)
	}

	result, err := c.db.Exec(
		"INSERT INTO clips (content_type, data, filename) VALUES (?, ?, ?)",
		contentType, data, filename,
	)
	if err != nil {
		L.Push(lua.LNil)
		L.Push(lua.LString(err.Error()))
		return 2
	}

	id, _ := result.LastInsertId()

	// Return a table with clip info (matching design spec)
	clip := L.NewTable()
	clip.RawSetString("id", lua.LNumber(id))
	L.Push(clip)
	return 1
}

// validateContentType checks that a content type string is a valid MIME type
func validateContentType(ct string) string {
	if len(ct) > maxContentTypeLength {
		return "application/octet-stream"
	}
	if !validMIMEType.MatchString(ct) {
		return "application/octet-stream"
	}
	return ct
}

// checkURLDomain validates the URL domain against the plugin's network permissions
func (c *ClipsAPI) checkURLDomain(urlStr string) error {
	parsed, err := url.Parse(urlStr)
	if err != nil {
		return fmt.Errorf("invalid URL: %w", err)
	}

	domain := parsed.Hostname()

	if len(c.allowedDomains) == 0 {
		return fmt.Errorf("no network permissions: plugin must declare network permissions to fetch URLs (domain: %s)", domain)
	}

	allowedMethods, ok := FindAllowedMethods(c.allowedDomains, domain)
	if !ok {
		return fmt.Errorf("domain not in allowlist: %s", domain)
	}

	// Check if GET is allowed for this domain
	getAllowed := false
	for _, m := range allowedMethods {
		if strings.EqualFold(m, "GET") {
			getAllowed = true
			break
		}
	}
	if !getAllowed {
		return fmt.Errorf("GET not allowed for domain %s (allowed: [%s])", domain, strings.Join(allowedMethods, ", "))
	}

	return nil
}

// createFromURL downloads content from a URL and creates a clip
func (c *ClipsAPI) createFromURL(L *lua.LState) int {
	rawURL := L.CheckString(1)

	// Validate URL scheme - only allow http and https
	if !strings.HasPrefix(rawURL, "http://") && !strings.HasPrefix(rawURL, "https://") {
		L.Push(lua.LNil)
		L.Push(lua.LString("only http:// and https:// URLs are allowed"))
		return 2
	}

	// Validate domain against plugin's network permissions
	if err := c.checkURLDomain(rawURL); err != nil {
		L.Push(lua.LNil)
		L.Push(lua.LString(err.Error()))
		return 2
	}

	var opts *lua.LTable
	if L.GetTop() >= 2 {
		opts = L.OptTable(2, nil)
	}

	// Create HTTP client with timeout and redirect limits
	allowedDomains := c.allowedDomains
	client := &http.Client{
		Timeout: URLFetchTimeout,
		CheckRedirect: func(req *http.Request, via []*http.Request) error {
			if len(via) >= 5 {
				return fmt.Errorf("too many redirects")
			}
			// Validate redirect domain against allowlist
			domain := req.URL.Hostname()
			if _, ok := FindAllowedMethods(allowedDomains, domain); !ok {
				return fmt.Errorf("redirect to unauthorized domain: %s", domain)
			}
			return nil
		},
	}

	resp, err := client.Get(rawURL)
	if err != nil {
		L.Push(lua.LNil)
		L.Push(lua.LString("failed to fetch URL: " + err.Error()))
		return 2
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		L.Push(lua.LNil)
		L.Push(lua.LString(fmt.Sprintf("HTTP error: %d %s", resp.StatusCode, resp.Status)))
		return 2
	}

	// Read body with size limit
	limitedReader := io.LimitReader(resp.Body, MaxClipDataSize+1)
	data, err := io.ReadAll(limitedReader)
	if err != nil {
		L.Push(lua.LNil)
		L.Push(lua.LString("failed to read response: " + err.Error()))
		return 2
	}

	if len(data) > MaxClipDataSize {
		L.Push(lua.LNil)
		L.Push(lua.LString(fmt.Sprintf("response too large: exceeds %d bytes", MaxClipDataSize)))
		return 2
	}

	// Determine content type
	contentType := resp.Header.Get("Content-Type")
	if contentType == "" {
		contentType = "application/octet-stream"
	}
	// Strip charset if present
	if idx := strings.Index(contentType, ";"); idx > 0 {
		contentType = strings.TrimSpace(contentType[:idx])
	}

	// Override with options if provided
	if opts != nil {
		if mt := opts.RawGetString("mime_type"); mt != lua.LNil {
			contentType = mt.String()
		} else if ct := opts.RawGetString("content_type"); ct != lua.LNil {
			contentType = ct.String()
		}
	}

	// Validate content type format
	contentType = validateContentType(contentType)

	// Determine filename
	filename := ""
	if opts != nil {
		if nm := opts.RawGetString("name"); nm != lua.LNil {
			filename = nm.String()
		} else if fn := opts.RawGetString("filename"); fn != lua.LNil {
			filename = fn.String()
		}
	}
	// Fallback to URL path
	if filename == "" {
		filename = path.Base(rawURL)
		if filename == "." || filename == "/" {
			filename = "downloaded"
		}
	}

	// Insert into database
	result, err := c.db.Exec(
		"INSERT INTO clips (content_type, data, filename) VALUES (?, ?, ?)",
		contentType, data, filename,
	)
	if err != nil {
		L.Push(lua.LNil)
		L.Push(lua.LString(err.Error()))
		return 2
	}

	id, _ := result.LastInsertId()

	// Return a table with clip info
	clip := L.NewTable()
	clip.RawSetString("id", lua.LNumber(id))
	L.Push(clip)
	return 1
}

func (c *ClipsAPI) update(L *lua.LState) int {
	id := L.CheckInt64(1)
	opts := L.CheckTable(2)

	// Only allow updating is_archived for now
	if archived := opts.RawGetString("is_archived"); archived != lua.LNil {
		archivedInt := 0
		if archived == lua.LTrue {
			archivedInt = 1
		}
		_, err := c.db.Exec("UPDATE clips SET is_archived = ? WHERE id = ?", archivedInt, id)
		if err != nil {
			L.Push(lua.LFalse)
			L.Push(lua.LString(err.Error()))
			return 2
		}
	}

	L.Push(lua.LTrue)
	return 1
}

func (c *ClipsAPI) deleteClip(L *lua.LState) int {
	id := L.CheckInt64(1)

	_, err := c.db.Exec("DELETE FROM clips WHERE id = ?", id)
	if err != nil {
		L.Push(lua.LFalse)
		L.Push(lua.LString(err.Error()))
		return 2
	}

	L.Push(lua.LTrue)
	return 1
}

func (c *ClipsAPI) deleteMany(L *lua.LState) int {
	ids := L.CheckTable(1)

	var idList []int64
	ids.ForEach(func(_, v lua.LValue) {
		if num, ok := v.(lua.LNumber); ok {
			idList = append(idList, int64(num))
		}
	})

	if len(idList) == 0 {
		L.Push(lua.LTrue)
		return 1
	}

	// Build query with placeholders
	placeholders := make([]string, len(idList))
	args := make([]interface{}, len(idList))
	for i, id := range idList {
		placeholders[i] = "?"
		args[i] = id
	}

	query := "DELETE FROM clips WHERE id IN (" + strings.Join(placeholders, ",") + ")"
	_, err := c.db.Exec(query, args...)
	if err != nil {
		L.Push(lua.LFalse)
		L.Push(lua.LString(err.Error()))
		return 2
	}

	L.Push(lua.LTrue)
	return 1
}

func (c *ClipsAPI) archive(L *lua.LState) int {
	id := L.CheckInt64(1)

	_, err := c.db.Exec("UPDATE clips SET is_archived = 1 WHERE id = ?", id)
	if err != nil {
		L.Push(lua.LFalse)
		L.Push(lua.LString(err.Error()))
		return 2
	}

	L.Push(lua.LTrue)
	return 1
}

func (c *ClipsAPI) unarchive(L *lua.LState) int {
	id := L.CheckInt64(1)

	_, err := c.db.Exec("UPDATE clips SET is_archived = 0 WHERE id = ?", id)
	if err != nil {
		L.Push(lua.LFalse)
		L.Push(lua.LString(err.Error()))
		return 2
	}

	L.Push(lua.LTrue)
	return 1
}
