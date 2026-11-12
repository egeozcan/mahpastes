// Package windowstate persists the main window's size, position and maximised
// state across launches. It is pure logic plus a small JSON file in the data
// directory; the Wails runtime calls live in internal/wailsbridge.
package windowstate

import (
	"encoding/json"
	"os"
	"path/filepath"
)

// FileName is the state file inside the data directory.
const FileName = "window.json"

// Minimum window size, matching options.App MinWidth/MinHeight.
const (
	MinWidth  = 800
	MinHeight = 600
)

// State is the persisted geometry. X/Y are only meaningful when HasPos is set.
type State struct {
	Width     int  `json:"width"`
	Height    int  `json:"height"`
	X         int  `json:"x"`
	Y         int  `json:"y"`
	HasPos    bool `json:"hasPos"`
	Maximised bool `json:"maximised"`
}

// Screen is the logical size of one display.
type Screen struct{ Width, Height int }

// Load reads the state file. ok is false when it is missing, unreadable or
// holds an unusable size.
func Load(dataDir string) (s State, ok bool) {
	b, err := os.ReadFile(filepath.Join(dataDir, FileName))
	if err != nil || json.Unmarshal(b, &s) != nil {
		return State{}, false
	}
	if s.Width < MinWidth || s.Height < MinHeight {
		return State{}, false
	}
	return s, true
}

// Save writes the state atomically.
func Save(dataDir string, s State) error {
	b, err := json.Marshal(s)
	if err != nil {
		return err
	}
	path := filepath.Join(dataDir, FileName)
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, b, 0o600); err != nil {
		return err
	}
	return os.Rename(tmp, path)
}

// Merge combines a freshly read geometry with the previously saved state. While
// the window is maximised or fullscreen its reported size is the screen size,
// not the user's normal size, so the previous normal size and position are kept.
func Merge(prev State, hadPrev bool, cur State, normal bool) State {
	if normal || !hadPrev {
		cur.Maximised = !normal && cur.Maximised
		return cur
	}
	prev.Maximised = cur.Maximised
	return prev
}

// ClampSize bounds the size to the minimum and, when known, the screen.
func ClampSize(w, h int, screen *Screen) (int, int) {
	if screen != nil && screen.Width > 0 && screen.Height > 0 {
		if w > screen.Width {
			w = screen.Width
		}
		if h > screen.Height {
			h = screen.Height
		}
	}
	if w < MinWidth {
		w = MinWidth
	}
	if h < MinHeight {
		h = MinHeight
	}
	return w, h
}

// PositionVisible reports whether a window of size w x h placed at x,y (relative
// to the screen it is on) keeps enough of its title area on screen to be
// grabbed. A saved position that fails this, for instance after a monitor was
// unplugged, is discarded so the window keeps Wails' default centring.
func PositionVisible(x, y, w, h int, screen Screen) bool {
	const grab = 100
	if screen.Width <= 0 || screen.Height <= 0 {
		return false
	}
	return x+w >= grab && x <= screen.Width-grab && y >= 0 && y <= screen.Height-grab
}
