package plugin

import "testing"

func TestCompareSemver(t *testing.T) {
	tests := []struct {
		a, b string
		want int
	}{
		{"1.0.0", "1.0.0", 0},
		{"1.0.0", "1.0.1", -1},
		{"1.0.1", "1.0.0", 1},
		{"1.1.0", "1.0.9", 1},
		{"2.0.0", "1.99.99", 1},
		{"0.1.0", "0.0.9", 1},
		{"1.0", "1.0.0", 0},
		{"1", "1.0.0", 0},
		{"bad", "1.0.0", 0},
		{"1.0.0", "bad", 0},
	}
	for _, tt := range tests {
		got := CompareSemver(tt.a, tt.b)
		if got != tt.want {
			t.Errorf("CompareSemver(%q, %q) = %d, want %d", tt.a, tt.b, got, tt.want)
		}
	}
}

func TestIsNewerVersion(t *testing.T) {
	if !IsNewerVersion("1.0.0", "1.0.1") {
		t.Error("1.0.1 should be newer than 1.0.0")
	}
	if IsNewerVersion("1.0.1", "1.0.0") {
		t.Error("1.0.0 should not be newer than 1.0.1")
	}
	if IsNewerVersion("1.0.0", "1.0.0") {
		t.Error("same version should not be newer")
	}
}
