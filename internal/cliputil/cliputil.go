// Package cliputil wraps golang.design/x/clipboard writes so that a failed
// write is reported as an error instead of being silently dropped.
package cliputil

import (
	"errors"
	"fmt"

	"golang.design/x/clipboard"
)

// The library entry points, replaced in tests so the error mapping can be
// checked without touching the user's clipboard.
var (
	initClipboard  = clipboard.Init
	writeClipboard = clipboard.Write
)

// Write publishes data in the given format. clipboard.Write returns a nil
// channel when nothing was written (no backend, failed Init, empty format),
// which this turns into an error.
func Write(format clipboard.Format, data []byte) error {
	if err := initClipboard(); err != nil {
		return fmt.Errorf("failed to initialize clipboard: %w", err)
	}
	if writeClipboard(format, data) == nil {
		return errors.New("failed to write to clipboard: no clipboard backend accepted the data")
	}
	return nil
}

// WriteText publishes UTF-8 text.
func WriteText(text string) error {
	return Write(clipboard.FmtText, []byte(text))
}
