package main

import (
	"bufio"
	"errors"
	"fmt"
	"io"
	"mime"
	"net/http"
	"os"
	"strings"
	"time"

	"github.com/mattn/go-isatty"
)

// stdinIsTerminal reports whether stdin is an interactive terminal.
func stdinIsTerminal() bool {
	fd := os.Stdin.Fd()
	return isatty.IsTerminal(fd) || isatty.IsCygwinTerminal(fd)
}

// stdoutIsTerminal reports whether stdout is an interactive terminal.
func stdoutIsTerminal() bool {
	fd := os.Stdout.Fd()
	return isatty.IsTerminal(fd) || isatty.IsCygwinTerminal(fd)
}

// sniffExtension picks a file extension for data from its first bytes.
// The server prefers a filename's extension over its own sniffing, so a
// blanket ".txt" would mislabel binary input.
func sniffExtension(head []byte) string {
	if len(head) == 0 {
		return ".txt"
	}
	ct := http.DetectContentType(head)
	return extensionForContentType(ct)
}

// sniffedExtensions maps every type http.DetectContentType can report (plus
// the common explicit ones) to a fixed extension, so the same bytes get the
// same name on every OS: mime.ExtensionsByType reads /etc/mime.types or the
// Windows registry and returns its candidates in alphabetical order.
var sniffedExtensions = map[string]string{
	"text/plain":                    ".txt",
	"text/html":                     ".html",
	"text/xml":                      ".xml",
	"application/xml":               ".xml",
	"text/markdown":                 ".md",
	"text/csv":                      ".csv",
	"text/css":                      ".css",
	"text/javascript":               ".js",
	"application/javascript":        ".js",
	"application/json":              ".json",
	"image/jpeg":                    ".jpg",
	"image/png":                     ".png",
	"image/gif":                     ".gif",
	"image/webp":                    ".webp",
	"image/bmp":                     ".bmp",
	"image/x-icon":                  ".ico",
	"image/svg+xml":                 ".svg",
	"image/avif":                    ".avif",
	"application/pdf":               ".pdf",
	"application/postscript":        ".ps",
	"application/zip":               ".zip",
	"application/x-gzip":            ".gz",
	"application/gzip":              ".gz",
	"application/x-rar-compressed":  ".rar",
	"application/vnd.rar":           ".rar",
	"application/x-7z-compressed":   ".7z",
	"application/wasm":              ".wasm",
	"application/ogg":               ".ogg",
	"audio/mpeg":                    ".mp3",
	"audio/wave":                    ".wav",
	"audio/wav":                     ".wav",
	"audio/aiff":                    ".aiff",
	"audio/basic":                   ".au",
	"audio/midi":                    ".mid",
	"video/mp4":                     ".mp4",
	"video/webm":                    ".webm",
	"video/avi":                     ".avi",
	"font/ttf":                      ".ttf",
	"font/otf":                      ".otf",
	"font/collection":               ".ttc",
	"font/woff":                     ".woff",
	"font/woff2":                    ".woff2",
	"application/vnd.ms-fontobject": ".eot",
	"application/octet-stream":      ".bin",
}

func extensionForContentType(ct string) string {
	mt, _, err := mime.ParseMediaType(ct)
	if err != nil {
		mt = strings.ToLower(strings.TrimSpace(strings.SplitN(ct, ";", 2)[0]))
	}
	if mt == "" {
		return ".txt"
	}
	if ext, ok := sniffedExtensions[mt]; ok {
		return ext
	}
	// An explicit --content-type outside the table: ask the platform, which
	// is the best we have for a type the user chose themselves.
	if exts, err := mime.ExtensionsByType(mt); err == nil && len(exts) > 0 {
		return exts[0]
	}
	return ".bin"
}

// errEmptyStdin is returned when stdin carries no data: an empty clip is
// never what a pipeline meant (usually the producing command failed).
var errEmptyStdin = errors.New("no input: stdin is empty")

// stdinFilename returns the name to upload stdin under: the explicit one, or
// paste-<timestamp><ext> with the extension sniffed from the first 512 bytes
// (or derived from an explicit content type). The returned reader replays the
// peeked bytes. Empty input is an error.
func stdinFilename(explicit, contentType string, in io.Reader, now time.Time) (string, io.Reader, error) {
	br := bufio.NewReaderSize(in, 512)
	head, err := br.Peek(512)
	if err != nil && err != io.EOF && err != bufio.ErrBufferFull {
		return "", nil, fmt.Errorf("read stdin: %w", err)
	}
	if len(head) == 0 {
		return "", nil, errEmptyStdin
	}
	if explicit != "" {
		return explicit, br, nil
	}
	ext := sniffExtension(head)
	if contentType != "" {
		ext = extensionForContentType(contentType)
	}
	return "paste-" + now.Format("20060102-150405") + ext, br, nil
}

// looksBinary reports whether data should not be written to a terminal.
func looksBinary(head []byte) bool {
	for _, b := range head {
		if b == 0 {
			return true
		}
	}
	return !strings.HasPrefix(http.DetectContentType(head), "text/")
}
