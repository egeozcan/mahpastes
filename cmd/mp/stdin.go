package main

import (
	"bufio"
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

func extensionForContentType(ct string) string {
	mt, _, err := mime.ParseMediaType(ct)
	if err != nil {
		mt = strings.TrimSpace(strings.SplitN(ct, ";", 2)[0])
	}
	switch mt {
	case "text/plain", "":
		return ".txt"
	case "text/html":
		return ".html"
	case "text/xml", "application/xml":
		return ".xml"
	case "application/json":
		return ".json"
	case "image/jpeg":
		return ".jpg"
	case "image/png":
		return ".png"
	case "image/gif":
		return ".gif"
	case "image/webp":
		return ".webp"
	case "application/pdf":
		return ".pdf"
	case "application/zip":
		return ".zip"
	case "application/octet-stream":
		return ".bin"
	}
	if exts, err := mime.ExtensionsByType(mt); err == nil && len(exts) > 0 {
		return exts[0]
	}
	return ".bin"
}

// stdinFilename returns the name to upload stdin under: the explicit one, or
// paste-<timestamp><ext> with the extension sniffed from the first 512 bytes
// (or derived from an explicit content type). The returned reader replays the
// peeked bytes.
func stdinFilename(explicit, contentType string, in io.Reader, now time.Time) (string, io.Reader, error) {
	br := bufio.NewReaderSize(in, 512)
	if explicit != "" {
		return explicit, br, nil
	}
	head, err := br.Peek(512)
	if err != nil && err != io.EOF && err != bufio.ErrBufferFull {
		return "", nil, fmt.Errorf("read stdin: %w", err)
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
