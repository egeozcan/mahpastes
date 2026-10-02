package plugin

import (
	"errors"
	"testing"
)

// TestClosedSandboxRunsNothing: EmitEvent defers delivery to a sandbox parked
// in a host call, and that delivery can acquire the sandbox only after the
// plugin was unloaded. gopher-lua nils its call stack on Close, so running a
// handler then would panic the delivering goroutine — and with it the app.
func TestClosedSandboxRunsNothing(t *testing.T) {
	s := NewSandbox(&Manifest{Name: "closed"}, 1)
	if err := s.LoadSource(`
function on_tag_created(data) end
function on_ui_action(action_id, clip_ids, options, context) return {success = true} end
function on_search(source, query) return {} end`); err != nil {
		t.Fatalf("LoadSource: %v", err)
	}
	s.Close()

	// Event delivery skips it without counting a run (see deliverEvent).
	if err := s.CallHandlerWithData("on_tag_created", map[string]interface{}{"id": 1}); !errors.Is(err, errSandboxClosed) {
		t.Fatalf("CallHandlerWithData on a closed sandbox = %v, want errSandboxClosed", err)
	}
	if err := s.CallHandler("on_tag_created"); err != nil {
		t.Fatalf("CallHandler on a closed sandbox = %v, want a silent skip", err)
	}
	if _, err := s.CallUIAction("x", nil, nil, nil, MaxExecutionTime); !errors.Is(err, errSandboxClosed) {
		t.Fatalf("CallUIAction on a closed sandbox = %v, want errSandboxClosed", err)
	}
	if _, err := s.CallSearch("src", "q", MaxSearchTime); !errors.Is(err, errSandboxClosed) {
		t.Fatalf("CallSearch on a closed sandbox = %v, want errSandboxClosed", err)
	}
	s.Close() // a second Close must not touch the closed state either
}
