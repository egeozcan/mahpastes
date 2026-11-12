// Package windowstate persists the main window's size, position and maximised
// state across launches. It is pure logic plus one entry in the desktop prefs
// file (internal/desktopprefs), which is read before wails.Run; the Wails
// runtime calls live in internal/wailsbridge.
package windowstate

import "go-clipboard/internal/desktopprefs"

// PrefsKey is the key the state is stored under in desktopprefs' file.
const PrefsKey = "window"

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
// X/Y are in whatever space Wails' WindowGetPosition reports: on macOS they
// are relative to the visible frame of the screen the window was on; on Linux
// they are root-window coordinates (which match the setter's monitor-relative
// space only on a monitor at the desktop origin); on Windows they are absolute
// virtual-desktop physical pixels (GetWindowRect), which the Windows restore
// path sets back directly so a window can return to a second monitor.
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

// Load reads the saved state. ok is false when it is missing, unreadable or
// holds an unusable size.
func Load(dataDir string) (s State, ok bool) {
	found, err := desktopprefs.Get(dataDir, PrefsKey, &s)
	if err != nil || !found {
		return State{}, false
	}
	if !validSize(s.Width, s.Height) {
		return State{}, false
	}
	return s, true
}

// Save stores the state atomically, keeping the other desktop prefs.
func Save(dataDir string, s State) error {
	return desktopprefs.Set(dataDir, PrefsKey, s)
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
