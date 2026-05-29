package app

import (
	"testing"
)

func TestValidateSubtagPath(t *testing.T) {
	tests := []struct {
		name      string
		servedTag string
		relTag    string
		wantFull  string
		wantErr   bool
	}{
		{"empty tag returns served tag", "a/b", "", "a/b", false},
		{"single segment", "a/b", "c", "a/b/c", false},
		{"multi segment", "a/b", "c/d/e", "a/b/c/d/e", false},
		{"trims whitespace", "a/b", " c/d ", "a/b/c/d", false},
		{"rejects dotdot", "a/b", "../evil", "", true},
		{"rejects dotdot in middle", "a/b", "c/../evil", "", true},
		{"rejects single dot", "a/b", "./c", "", true},
		{"rejects empty segment", "a/b", "c//d", "", true},
		{"rejects _api segment", "a/b", "_api/foo", "", true},
		{"rejects _api as only segment", "a/b", "_api", "", true},
		{"allows _api substring", "a/b", "my_api_stuff", "a/b/my_api_stuff", false},
		{"top-level tag with subtag", "photos", "vacation/beach", "photos/vacation/beach", false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := validateSubtagPath(tt.servedTag, tt.relTag)
			if (err != nil) != tt.wantErr {
				t.Errorf("validateSubtagPath(%q, %q) error = %v, wantErr %v", tt.servedTag, tt.relTag, err, tt.wantErr)
				return
			}
			if !tt.wantErr && got != tt.wantFull {
				t.Errorf("validateSubtagPath(%q, %q) = %q, want %q", tt.servedTag, tt.relTag, got, tt.wantFull)
			}
		})
	}
}
