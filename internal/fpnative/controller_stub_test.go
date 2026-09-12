//go:build !darwin || !cgo || !fileprovider || bindings

package fpnative

import "testing"

func TestUnsupportedBuildHasNoNativeSideEffects(t *testing.T) {
	for _, op := range []string{"info", "add", "remove", "credential", "signal", "reveal"} {
		result, err := Call(op, "test")
		if result.Supported || result.Secret != "" || result.GroupPath != "" || err == nil {
			t.Fatalf("%s: %+v, %v", op, result, err)
		}
	}
}
