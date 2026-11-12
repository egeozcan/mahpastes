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

// Default and minimum window size, matching options.App in main.go.
const (
	DefaultWidth  = 1280
	DefaultHeight = 800
	MinWidth      = 800
	MinHeight     = 600
)

// MaxDimension bounds a stored width or height. Anything larger is treated as
// a corrupt file rather than handed to the window system.
const MaxDimension = 16384

// grab is how much of the window's top edge, in pixels, must stay on a screen
// for the user to be able to drag it back.
const grab = 100

// edgeSlack tolerates windows whose frame pokes slightly past the top of the
// usable area, such as snapped windows on Windows whose invisible resize
// border extends a few pixels beyond the work area.
const edgeSlack = 16

// State is the persisted geometry. X/Y are only meaningful when HasPos is set.
//
// Width/Height are logical (DPI-independent) pixels as Wails reports them.
// X/Y are in whatever space the platform's position getter uses: on macOS and
// Linux they are relative to the screen the window was on (Wails'
// WindowGetPosition), on Windows they are absolute virtual-desktop physical
// pixels (GetWindowRect), which lets a window come back on a second monitor.
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

// Rect is an area in screen coordinates.
type Rect struct{ X, Y, Width, Height int }

func validSize(w, h int) bool {
	return w >= MinWidth && h >= MinHeight && w <= MaxDimension && h <= MaxDimension
}

// Load reads the state file. ok is false when it is missing, unreadable or
// holds an unusable size.
func Load(dataDir string) (s State, ok bool) {
	b, err := os.ReadFile(filepath.Join(dataDir, FileName))
	if err != nil || json.Unmarshal(b, &s) != nil {
		return State{}, false
	}
	if !validSize(s.Width, s.Height) {
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
	if err := os.Rename(tmp, path); err != nil {
		_ = os.Remove(tmp)
		return err
	}
	return nil
}

// Merge combines a freshly read geometry with the previously saved state and
// returns what should be saved; ok is false when there is nothing worth
// saving. While the window is maximised, minimised or fullscreen its reported
// size and position are not the user's normal ones, so the previous normal
// geometry is kept (or, on a first run, the defaults with no position).
func Merge(prev State, hadPrev bool, cur State, normal bool) (State, bool) {
	if normal {
		cur.Maximised = false
		return cur, validSize(cur.Width, cur.Height)
	}
	base := prev
	if !hadPrev {
		base = State{Width: DefaultWidth, Height: DefaultHeight}
	}
	base.Maximised = cur.Maximised
	return base, true
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

// TitleVisible reports whether a window of width w whose top-left corner is at
// x,y keeps enough of its title area inside area to be grabbed.
func TitleVisible(x, y, w int, area Rect) bool {
	if area.Width <= 0 || area.Height <= 0 {
		return false
	}
	return x+w >= area.X+grab &&
		x <= area.X+area.Width-grab &&
		y >= area.Y-edgeSlack &&
		y <= area.Y+area.Height-grab
}

// PositionVisible reports whether a window of size w x h placed at x,y (relative
// to the screen it is on) keeps enough of its title area on screen to be
// grabbed. A saved position that fails this, for instance after a monitor was
// unplugged, is discarded so the window keeps Wails' default centring.
func PositionVisible(x, y, w, h int, screen Screen) bool {
	return TitleVisible(x, y, w, Rect{Width: screen.Width, Height: screen.Height})
}

// Restore is what to do with the freshly opened window.
type Restore struct {
	// Resize is set when the saved size does not fit the current screen and
	// the window must be shrunk to Width x Height.
	Resize        bool
	Width, Height int
	// Move is set when the saved position should be applied.
	Move bool
	X, Y int
}

// PlanRestore decides how to apply a saved state to a window that Wails opened
// at the saved size on a screen of the given logical size (nil when unknown).
// relativePos says the position can be checked against that screen because
// the platform's getter and setter are both relative to it (macOS, Linux);
// otherwise the caller checks and applies the position itself (Windows).
func PlanRestore(s State, screen *Screen, relativePos bool) Restore {
	var r Restore
	if s.Maximised {
		return r
	}
	r.Width, r.Height = ClampSize(s.Width, s.Height, screen)
	r.Resize = r.Width != s.Width || r.Height != s.Height
	if !s.HasPos || !relativePos || screen == nil {
		return r
	}
	if PositionVisible(s.X, s.Y, r.Width, r.Height, *screen) {
		r.Move, r.X, r.Y = true, s.X, s.Y
	}
	return r
}
