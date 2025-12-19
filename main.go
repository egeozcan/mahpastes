package main

import (
	"database/sql"
	_ "embed" // Import the embed package
	"encoding/json"
	"fmt"
	"io"
	"log"
	"mime"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"

	_ "github.com/mattn/go-sqlite3" // SQLite driver
	"github.com/rs/cors"            // For CORS handling
)

// db is the global database connection
var db *sql.DB

// tempDir is the directory where temporary files will be stored
var tempDir string

// tempFileMutex protects access to the tempDir
var tempFileMutex sync.Mutex

//go:embed index.html
var indexHTML []byte // Embed the index.html file into a byte slice

// main is the entry point of the application
func main() {
	log.SetFlags(log.LstdFlags | log.Lshortfile)

	// Initialize the database
	if err := initDB(); err != nil {
		log.Fatalf("Failed to initialize database: %v", err)
	}

	// Setup directory for temporary files
	if err := initTempDir(); err != nil {
		log.Fatalf("Failed to initialize temp directory: %v", err)
	}

	// Setup router
	mux := http.NewServeMux()
	mux.HandleFunc("/", handleIndex)
	mux.HandleFunc("/upload", handleUpload)
	mux.HandleFunc("/clips", handleGetClips)
	mux.HandleFunc("/clip/", handleClip)
	mux.HandleFunc("/archive/", handleToggleArchive)
	mux.HandleFunc("/tempfile/", handleTempFile)
	mux.HandleFunc("/tempfiles", handleDeleteAllTempFiles)
	mux.HandleFunc("/cancel-expiration/", handleCancelExpiration)

	// Setup CORS
	c := cors.New(cors.Options{
		AllowedOrigins: []string{"*"}, // Allow any origin
		AllowedMethods: []string{"GET", "POST", "DELETE", "PUT", "OPTIONS"},
		AllowedHeaders: []string{"*"},
	})
	handler := c.Handler(mux)

	port := "8989"
	log.Printf("Starting server on http://localhost:%s\n", port)
	if err := http.ListenAndServe(":"+port, handler); err != nil {
		log.Fatalf("Server failed to start: %v", err)
	}
}

// initDB initializes the SQLite database and creates the 'clips' table if it doesn't exist
func initDB() error {
	var err error
	db, err = sql.Open("sqlite3", "./clips.db")
	if err != nil {
		return fmt.Errorf("failed to open db: %w", err)
	}

	createTableSQL := `
    CREATE TABLE IF NOT EXISTS clips (
        id INTEGER PRIMARY KEY AUTOINCREMENT,
        content_type TEXT NOT NULL,
        data BLOB NOT NULL,
        filename TEXT,
        created_at DATETIME DEFAULT CURRENT_TIMESTAMP
    );`

	if _, err = db.Exec(createTableSQL); err != nil {
		return fmt.Errorf("failed to create table: %w", err)
	}

	// Migrate: Add is_archived column if it doesn't exist
	_, _ = db.Exec("ALTER TABLE clips ADD COLUMN is_archived INTEGER DEFAULT 0")
	// Migrate: Add expires_at column if it doesn't exist
	_, _ = db.Exec("ALTER TABLE clips ADD COLUMN expires_at DATETIME")

	// Start background cleanup job
	go startCleanupJob()

	return nil
}

// startCleanupJob deletes expired clips every minute
func startCleanupJob() {
	ticker := time.NewTicker(1 * time.Minute)
	for range ticker.C {
		result, err := db.Exec("DELETE FROM clips WHERE expires_at IS NOT NULL AND expires_at <= CURRENT_TIMESTAMP")
		if err != nil {
			log.Printf("Failed to delete expired clips: %v\n", err)
		} else {
			rows, _ := result.RowsAffected()
			if rows > 0 {
				log.Printf("Cleaned up %d expired clips\n", rows)
			}
		}
	}
}

// initTempDir creates the directory for storing temporary files
func initTempDir() error {
	tempFileMutex.Lock()
	defer tempFileMutex.Unlock()

	tempDir = filepath.Join(".", "clip_temp_files")
	if err := os.MkdirAll(tempDir, 0755); err != nil {
		return fmt.Errorf("failed to create temp dir '%s': %w", tempDir, err)
	}
	log.Printf("Temporary files will be stored in %s\n", tempDir)
	return nil
}

// handleIndex serves the main HTML page
func handleIndex(w http.ResponseWriter, r *http.Request) {
	if r.URL.Path != "/" {
		http.NotFound(w, r)
		return
	}
	w.Header().Set("Content-Type", "text/html")
	w.Write(indexHTML) // Write the embedded byte slice
}

// handleUpload handles file/text uploads
func handleUpload(w http.ResponseWriter, r *http.Request) {
	if r.Method != "POST" {
		http.Error(w, "Only POST method is allowed", http.StatusMethodNotAllowed)
		return
	}

	// 32 MB max memory for form parsing
	if err := r.ParseMultipartForm(32 << 20); err != nil {
		http.Error(w, "Failed to parse multipart form", http.StatusBadRequest)
		return
	}

	// Get files from the "data" field
	files := r.MultipartForm.File["data"]
	if len(files) == 0 {
		http.Error(w, "No files found in 'data'", http.StatusBadRequest)
		return
	}

	// Get expiration duration if provided
	expirationMinutes := 0
	if expStr := r.MultipartForm.Value["expiration"]; len(expStr) > 0 {
		expirationMinutes, _ = strconv.Atoi(expStr[0])
	}

	var expiresAt *time.Time
	if expirationMinutes > 0 {
		t := time.Now().Add(time.Duration(expirationMinutes) * time.Minute)
		expiresAt = &t
	}

	for _, handler := range files {
		file, err := handler.Open()
		if err != nil {
			log.Printf("Failed to open uploaded file: %v", err)
			continue
		}

		fileBytes, err := io.ReadAll(file)
		file.Close() // Close immediately after reading
		if err != nil {
			log.Printf("Failed to read bytes from file %s: %v", handler.Filename, err)
			continue
		}

		contentType := handler.Header.Get("Content-Type")
		filename := handler.Filename

		// Special handling for pasted text
		if contentType == "text/plain" || contentType == "" {
			textData := string(fileBytes)
			trimmedText := strings.TrimSpace(textData)

			if strings.HasPrefix(trimmedText, "<!DOCTYPE html") {
				contentType = "text/html"
			} else if isJSON(trimmedText) {
				contentType = "application/json"
			} else {
				contentType = "text/plain" // Ensure it's set
			}
		}

		// Insert into database (is_archived defaults to 0)
		_, err = db.Exec("INSERT INTO clips (content_type, data, filename, expires_at) VALUES (?, ?, ?, ?)",
			contentType, fileBytes, filename, expiresAt)
		if err != nil {
			log.Printf("Failed to insert into db: %v\n", err)
			// We don't return here so other files can still be processed
			continue
		}
	}

	w.WriteHeader(http.StatusCreated)
	fmt.Fprintln(w, "Upload(s) successful")
}

// isJSON checks if a string is valid JSON
func isJSON(s string) bool {
	var js json.RawMessage
	return json.Unmarshal([]byte(s), &js) == nil
}

// ClipPreview is the struct for JSON responses in the gallery
type ClipPreview struct {
	ID          int64      `json:"id"`
	ContentType string     `json:"content_type"`
	Filename    string     `json:"filename"`
	CreatedAt   time.Time  `json:"created_at"`
	ExpiresAt   *time.Time `json:"expires_at"`
	Preview     string     `json:"preview"`
	IsArchived  bool       `json:"is_archived"`
}

// handleGetClips retrieves a list of all clips for the gallery
func handleGetClips(w http.ResponseWriter, r *http.Request) {
	if r.Method != "GET" {
		http.Error(w, "Only GET method is allowed", http.StatusMethodNotAllowed)
		return
	}

	// Check for archived filter
	archived := 0
	if r.URL.Query().Get("archived") == "true" {
		archived = 1
	}

	// Get a preview (first 200 bytes) for text-based content
	query := `
    SELECT id, content_type, filename, created_at, expires_at, SUBSTR(data, 1, 500), is_archived
    FROM clips 
    WHERE is_archived = ? AND (expires_at IS NULL OR expires_at > CURRENT_TIMESTAMP)
    ORDER BY created_at DESC 
    LIMIT 50`

	rows, err := db.Query(query, archived)
	if err != nil {
		log.Printf("Failed to query clips: %v\n", err)
		http.Error(w, "Failed to query database", http.StatusInternalServerError)
		return
	}
	defer rows.Close()

	var clips []ClipPreview
	for rows.Next() {
		var clip ClipPreview
		var filename sql.NullString
		var expiresAt sql.NullTime
		var previewData []byte
		var isArchivedInt int
		if err := rows.Scan(&clip.ID, &clip.ContentType, &filename, &clip.CreatedAt, &expiresAt, &previewData, &isArchivedInt); err != nil {
			log.Printf("Failed to scan clip row: %v\n", err)
			continue
		}
		clip.Filename = filename.String
		clip.IsArchived = isArchivedInt == 1
		if expiresAt.Valid {
			clip.ExpiresAt = &expiresAt.Time
		}

		// Only set string preview for text-based types
		if strings.HasPrefix(clip.ContentType, "text/") || clip.ContentType == "application/json" {
			clip.Preview = string(previewData)
		} else {
			clip.Preview = "" // Client will know not to render text
		}
		clips = append(clips, clip)
	}

	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(clips); err != nil {
		log.Printf("Failed to encode clips to JSON: %v\n", err)
	}
}

// handleClip handles actions on a single clip (GET for serving, DELETE for removing)
func handleClip(w http.ResponseWriter, r *http.Request) {
	idStr := strings.TrimPrefix(r.URL.Path, "/clip/")
	id, err := strconv.ParseInt(idStr, 10, 64)
	if err != nil {
		http.Error(w, "Invalid clip ID", http.StatusBadRequest)
		return
	}

	switch r.Method {
	case "GET":
		// Serve the raw file
		var contentType string
		var data []byte
		row := db.QueryRow("SELECT content_type, data FROM clips WHERE id = ?", id)
		if err := row.Scan(&contentType, &data); err != nil {
			if err == sql.ErrNoRows {
				http.Error(w, "Clip not found", http.StatusNotFound)
				return
			}
			log.Printf("Failed to get clip data: %v\n", err)
			http.Error(w, "Failed to retrieve clip", http.StatusInternalServerError)
			return
		}

		w.Header().Set("Content-Type", contentType)
		if contentType == "text/html" {
			w.Header().Set("Content-Security-Policy", "default-src * data:; script-src * 'unsafe-inline' 'unsafe-eval'; style-src * 'unsafe-inline';")
		}
		w.Write(data)

	case "DELETE":
		// Delete the clip
		if _, err := db.Exec("DELETE FROM clips WHERE id = ?", id); err != nil {
			log.Printf("Failed to delete clip: %v\n", err)
			http.Error(w, "Failed to delete clip", http.StatusInternalServerError)
			return
		}
		w.WriteHeader(http.StatusOK)
		fmt.Fprintln(w, "Clip deleted")

	default:
		http.Error(w, "Invalid method", http.StatusMethodNotAllowed)
	}
}

// handleToggleArchive toggles the archived status of a clip
func handleToggleArchive(w http.ResponseWriter, r *http.Request) {
	if r.Method != "POST" {
		http.Error(w, "Only POST method is allowed", http.StatusMethodNotAllowed)
		return
	}

	idStr := strings.TrimPrefix(r.URL.Path, "/archive/")
	id, err := strconv.ParseInt(idStr, 10, 64)
	if err != nil {
		http.Error(w, "Invalid clip ID", http.StatusBadRequest)
		return
	}

	// Toggle the is_archived bit
	_, err = db.Exec("UPDATE clips SET is_archived = NOT is_archived WHERE id = ?", id)
	if err != nil {
		log.Printf("Failed to toggle archive status: %v\n", err)
		http.Error(w, "Failed to update clip", http.StatusInternalServerError)
		return
	}

	w.WriteHeader(http.StatusOK)
	fmt.Fprintln(w, "Archive status toggled")
}

// handleTempFile creates a temporary file from a clip and returns its path
func handleTempFile(w http.ResponseWriter, r *http.Request) {
	if r.Method != "POST" {
		http.Error(w, "Only POST method is allowed", http.StatusMethodNotAllowed)
		return
	}

	idStr := strings.TrimPrefix(r.URL.Path, "/tempfile/")
	id, err := strconv.ParseInt(idStr, 10, 64)
	if err != nil {
		http.Error(w, "Invalid clip ID", http.StatusBadRequest)
		return
	}

	var data []byte
	var filename sql.NullString
	var contentType string
	row := db.QueryRow("SELECT data, filename, content_type FROM clips WHERE id = ?", id)
	if err := row.Scan(&data, &filename, &contentType); err != nil {
		if err == sql.ErrNoRows {
			http.Error(w, "Clip not found", http.StatusNotFound)
			return
		}
		log.Printf("Failed to get clip data for temp file: %v\n", err)
		http.Error(w, "Failed to retrieve clip", http.StatusInternalServerError)
		return
	}

	// Create a safe filename
	safeName := fmt.Sprintf("%d", id)
	if filename.Valid && filename.String != "" {
		// Use the original filename as a base, but keep it simple
		safeName = fmt.Sprintf("%d_%s", id, filepath.Base(filename.String))
	} else {
		// Try to get an extension from the mime type
		exts, _ := mime.ExtensionsByType(contentType)
		if len(exts) > 0 {
			safeName = safeName + exts[0]
		}
	}

	// Ensure the temp directory exists
	if err := initTempDir(); err != nil {
		log.Printf("Failed to ensure temp dir exists: %v\n", err)
		http.Error(w, "Failed to create temp directory", http.StatusInternalServerError)
		return
	}

	tempFileMutex.Lock()
	tempFilePath := filepath.Join(tempDir, safeName)
	tempFileMutex.Unlock()

	if err := os.WriteFile(tempFilePath, data, 0644); err != nil {
		log.Printf("Failed to write temp file: %v\n", err)
		http.Error(w, "Failed to write temp file", http.StatusInternalServerError)
		return
	}

	absPath, err := filepath.Abs(tempFilePath)
	if err != nil {
		log.Printf("Failed to get absolute path: %v\n", err)
		http.Error(w, "Failed to resolve file path", http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]string{"path": absPath})
}

// handleDeleteAllTempFiles deletes all files from the temp directory
func handleDeleteAllTempFiles(w http.ResponseWriter, r *http.Request) {
	if r.Method != "DELETE" {
		http.Error(w, "Only DELETE method is allowed", http.StatusMethodNotAllowed)
		return
	}

	tempFileMutex.Lock()
	defer tempFileMutex.Unlock()

	// Remove the directory and all its contents
	if err := os.RemoveAll(tempDir); err != nil {
		log.Printf("Failed to remove temp dir: %v\n", err)
		http.Error(w, "Failed to delete temporary files", http.StatusInternalServerError)
		return
	}

	// Recreate the directory
	if err := os.MkdirAll(tempDir, 0755); err != nil {
		log.Printf("Failed to recreate temp dir: %v\n", err)
		// This is not ideal, but the app can probably continue
	}

	w.WriteHeader(http.StatusOK)
	fmt.Fprintln(w, "All temporary files deleted")
}

// handleCancelExpiration removes the expiration for a clip
func handleCancelExpiration(w http.ResponseWriter, r *http.Request) {
	if r.Method != "POST" {
		http.Error(w, "Only POST method is allowed", http.StatusMethodNotAllowed)
		return
	}

	idStr := strings.TrimPrefix(r.URL.Path, "/cancel-expiration/")
	id, err := strconv.ParseInt(idStr, 10, 64)
	if err != nil {
		http.Error(w, "Invalid clip ID", http.StatusBadRequest)
		return
	}

	_, err = db.Exec("UPDATE clips SET expires_at = NULL WHERE id = ?", id)
	if err != nil {
		log.Printf("Failed to cancel expiration: %v\n", err)
		http.Error(w, "Failed to update clip", http.StatusInternalServerError)
		return
	}

	w.WriteHeader(http.StatusOK)
	fmt.Fprintln(w, "Expiration cancelled")
}
