package windowstate

import (
	"os"
	"path/filepath"
	"testing"

	"go-clipboard/internal/desktopprefs"
)

func TestSaveLoadRoundTrip(t *testing.T) {
	dir := t.TempDir()
	want := State{Width: 1000, Height: 700, X: 10, Y: 20, HasPos: true, Maximised: true}
	if err := Save(dir, want); err != nil {
		t.Fatal(err)
	}
	got, ok := Load(dir)
	if !ok || got != want {
		t.Fatalf("got %+v ok=%v", got, ok)
	}
	if leftovers, _ := filepath.Glob(filepath.Join(dir, "*.tmp")); len(leftovers) != 0 {
		t.Fatalf("temp files left behind: %v", leftovers)
	}
	// Saving again replaces the existing file.
	want.Width = 1100
	if err := Save(dir, want); err != nil {
		t.Fatal(err)
	}
	if got, _ := Load(dir); got.Width != 1100 {
		t.Fatalf("second save not applied: %+v", got)
	}
}

// The window state shares the desktop prefs file: saving one must keep the
// other.
func TestSaveKeepsOtherDesktopPrefs(t *testing.T) {
	dir := t.TempDir()
	if err := desktopprefs.SetKeepRunningWhenClosed(dir, true); err != nil {
		t.Fatal(err)
	}
	if err := Save(dir, State{Width: 1000, Height: 700}); err != nil {
		t.Fatal(err)
	}
	p, err := desktopprefs.Load(dir)
	if err != nil || p.KeepRunningWhenClosed == nil || !*p.KeepRunningWhenClosed {
		t.Fatalf("keep-running lost after a window save: %+v, %v", p, err)
	}
	if err := desktopprefs.SetKeepRunningWhenClosed(dir, false); err != nil {
		t.Fatal(err)
	}
	if got, ok := Load(dir); !ok || got.Width != 1000 {
		t.Fatalf("window state lost after a prefs save: %+v ok=%v", got, ok)
	}
}

func TestLoadRejectsMissingCorruptAndOutOfRange(t *testing.T) {
	dir := t.TempDir()
	if _, ok := Load(dir); ok {
		t.Fatal("missing file should not load")
	}
	if err := os.WriteFile(desktopprefs.Path(dir), []byte("{not json"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, ok := Load(dir); ok {
		t.Fatal("corrupt file should not load")
	}
	if err := os.WriteFile(desktopprefs.Path(dir), []byte(`{"window": "not an object"}`), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, ok := Load(dir); ok {
		t.Fatal("corrupt window entry should not load")
	}
	for _, s := range []State{
		{Width: 10, Height: 10},
		{Width: 1000, Height: 100},
		{Width: 100000, Height: 800},
		{Width: 1280, Height: MaxDimension + 1},
	} {
		if err := Save(dir, s); err != nil {
			t.Fatal(err)
		}
		if _, ok := Load(dir); ok {
			t.Fatalf("size %dx%d should be rejected", s.Width, s.Height)
		}
	}
}

func TestMerge(t *testing.T) {
	prev := State{Width: 900, Height: 650, X: 5, Y: 5, HasPos: true}

	// Maximised with a previous save: keep the normal geometry, record maximised.
	got, ok := Merge(prev, true, State{Width: 1920, Height: 1080, HasPos: true, Maximised: true}, false)
	if !ok || got.Width != 900 || got.X != 5 || !got.Maximised || !got.HasPos {
		t.Fatalf("maximised with prev: %+v ok=%v", got, ok)
	}

	// Normal window: take the current geometry, never maximised.
	got, ok = Merge(prev, true, State{Width: 1000, Height: 700, X: 40, Y: 50, HasPos: true}, true)
	if !ok || got.Width != 1000 || got.X != 40 || got.Maximised {
		t.Fatalf("normal: %+v ok=%v", got, ok)
	}

	// Maximised on a first run: the screen size must not become the normal
	// size; fall back to the defaults with no position.
	got, ok = Merge(State{}, false, State{Width: 2560, Height: 1440, X: 0, Y: 0, HasPos: true, Maximised: true}, false)
	if !ok || got.Width != DefaultWidth || got.Height != DefaultHeight || got.HasPos || !got.Maximised {
		t.Fatalf("maximised first run: %+v ok=%v", got, ok)
	}

	// Minimised or fullscreen (not normal, not maximised): keep prev, not maximised.
	got, ok = Merge(State{Width: 900, Height: 650, Maximised: true}, true, State{Width: 160, Height: 28, X: -32000, Y: -32000, HasPos: true}, false)
	if !ok || got.Width != 900 || got.Maximised || got.X == -32000 {
		t.Fatalf("minimised: %+v ok=%v", got, ok)
	}

	// A normal window reporting an unusable size is not saved.
	if _, ok := Merge(prev, true, State{Width: 100, Height: 100, HasPos: true}, true); ok {
		t.Fatal("tiny normal size should not be saved")
	}
}

func TestClampSize(t *testing.T) {
	w, h := ClampSize(3000, 2000, &Screen{1920, 1080})
	if w != 1920 || h != 1080 {
		t.Fatal(w, h)
	}
	w, h = ClampSize(100, 100, nil)
	if w != MinWidth || h != MinHeight {
		t.Fatal(w, h)
	}
	w, h = ClampSize(1000, 700, &Screen{})
	if w != 1000 || h != 700 {
		t.Fatal(w, h)
	}
}

func TestPositionVisible(t *testing.T) {
	s := Screen{1920, 1080}
	if !PositionVisible(100, 100, 1280, 800, s) {
		t.Fatal("expected visible")
	}
	if PositionVisible(3000, 100, 1280, 800, s) {
		t.Fatal("off to the right")
	}
	if PositionVisible(-2000, 100, 1280, 800, s) {
		t.Fatal("off to the left")
	}
	if PositionVisible(100, -50, 1280, 800, s) {
		t.Fatal("above screen")
	}
	if PositionVisible(100, 1050, 1280, 800, s) {
		t.Fatal("title bar below the bottom edge")
	}
	if !PositionVisible(-7, 0, 1280, 800, s) {
		t.Fatal("snapped window with an invisible border should stay visible")
	}
	if PositionVisible(100, 100, 1280, 800, Screen{}) {
		t.Fatal("unknown screen")
	}
}

func TestTitleVisibleOnOffsetArea(t *testing.T) {
	// A second monitor to the right of a 1920-wide primary.
	second := Rect{X: 1920, Y: 0, Width: 2560, Height: 1400}
	if !TitleVisible(2200, 100, 1280, second) {
		t.Fatal("window on the second monitor should be visible there")
	}
	if TitleVisible(100, 100, 1280, second) {
		t.Fatal("window on the primary is not on the second monitor")
	}
	// A monitor left of the primary has negative coordinates.
	left := Rect{X: -1920, Y: 0, Width: 1920, Height: 1040}
	if !TitleVisible(-1500, 40, 1280, left) {
		t.Fatal("window on a left-hand monitor should be visible")
	}
}

func TestPlanRestore(t *testing.T) {
	screen := &Screen{1920, 1080}

	r := PlanRestore(State{Width: 1280, Height: 800, X: 50, Y: 60, HasPos: true}, screen, true)
	if r.Resize || !r.Move || r.X != 50 || r.Y != 60 {
		t.Fatalf("fits: %+v", r)
	}

	// Saved on a bigger display: shrink to the screen and check visibility at
	// the shrunk size.
	r = PlanRestore(State{Width: 2560, Height: 1400, X: 0, Y: 0, HasPos: true}, screen, true)
	if !r.Resize || r.Width != 1920 || r.Height != 1080 || !r.Move {
		t.Fatalf("oversized: %+v", r)
	}

	// Off-screen position: keep Wails' centring.
	r = PlanRestore(State{Width: 1280, Height: 800, X: 4000, Y: 60, HasPos: true}, screen, true)
	if r.Move {
		t.Fatalf("off-screen should not move: %+v", r)
	}

	// Maximised: Wails maximises it; nothing else to do.
	r = PlanRestore(State{Width: 2560, Height: 1400, X: 50, Y: 60, HasPos: true, Maximised: true}, screen, true)
	if r.Resize || r.Move {
		t.Fatalf("maximised: %+v", r)
	}

	// No saved position, or one the caller places itself (Windows).
	if r = PlanRestore(State{Width: 1280, Height: 800}, screen, true); r.Move {
		t.Fatalf("no position: %+v", r)
	}
	if r = PlanRestore(State{Width: 1280, Height: 800, X: 50, Y: 60, HasPos: true}, screen, false); r.Move {
		t.Fatalf("absolute positions are placed by the caller: %+v", r)
	}

	// Unknown screen: no clamp beyond the minimum, no move.
	r = PlanRestore(State{Width: 2560, Height: 1400, X: 50, Y: 60, HasPos: true}, nil, true)
	if r.Resize || r.Move {
		t.Fatalf("unknown screen: %+v", r)
	}
}
