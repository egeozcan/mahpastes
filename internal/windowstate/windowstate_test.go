package windowstate

import "testing"

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
}

func TestLoadRejectsMissingAndTiny(t *testing.T) {
	dir := t.TempDir()
	if _, ok := Load(dir); ok {
		t.Fatal("missing file should not load")
	}
	_ = Save(dir, State{Width: 10, Height: 10})
	if _, ok := Load(dir); ok {
		t.Fatal("tiny size should be rejected")
	}
}

func TestMergeKeepsNormalGeometryWhenMaximised(t *testing.T) {
	prev := State{Width: 900, Height: 650, X: 5, Y: 5, HasPos: true}
	cur := State{Width: 1920, Height: 1080, Maximised: true}
	got := Merge(prev, true, cur, false)
	if got.Width != 900 || !got.Maximised || !got.HasPos {
		t.Fatalf("got %+v", got)
	}
	got = Merge(prev, true, State{Width: 1000, Height: 700}, true)
	if got.Width != 1000 || got.Maximised {
		t.Fatalf("got %+v", got)
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
	if PositionVisible(100, 100, 1280, 800, Screen{}) {
		t.Fatal("unknown screen")
	}
}
