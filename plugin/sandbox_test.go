package plugin

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

// newSearchTestSandbox builds a sandbox with the http module registered the
// same way the manager does (shared budget holder) and the fixture loaded.
func newSearchTestSandbox(t *testing.T, src string, allowed map[string][]string) *Sandbox {
	t.Helper()
	manifest := &Manifest{Name: "test", Network: allowed}
	s := NewSandbox(manifest, 1)
	t.Cleanup(s.Close)

	api := NewHTTPAPI(NewNetworkPolicy(nil, 1, manifest))
	api.Register(s.GetState())
	s.SetHTTPBudget(api.Budget())

	if err := s.LoadSource(src); err != nil {
		t.Fatalf("LoadSource: %v", err)
	}
	return s
}

const searchFixtureSrc = `
function on_search(source, query)
    if source ~= "things" then return {} end
    if query == "boom" then return "not a table" end
    if query == "missing" then return nil end
    return {
        {value = "1", label = "Alpha"},
        {value = 12},
        {label = "no value"},
        "scalar entry",
        {value = "3", label = "Gamma"},
    }
end
`

func TestCallSearch_WellFormedRows(t *testing.T) {
	s := newSearchTestSandbox(t, searchFixtureSrc, map[string][]string{})

	choices, err := s.CallSearch("things", "al", MaxSearchTime)
	if err != nil {
		t.Fatalf("CallSearch: %v", err)
	}

	// Row 2 has no label (falls back to value "12"); row 3 has no value and
	// row 4 is a non-table entry — both are skipped, not a panic.
	if len(choices) != 3 {
		t.Fatalf("expected 3 choices, got %d: %+v", len(choices), choices)
	}
	if choices[0].Value != "1" || choices[0].Label != "Alpha" {
		t.Errorf("choice 0 = %+v", choices[0])
	}
	if choices[1].Value != "12" || choices[1].Label != "12" {
		t.Errorf("choice 1: numeric value must coerce, label falls back: %+v", choices[1])
	}
	if choices[2].Value != "3" || choices[2].Label != "Gamma" {
		t.Errorf("choice 2 = %+v", choices[2])
	}
}

func TestCallSearch_NonTableReturnErrors(t *testing.T) {
	s := newSearchTestSandbox(t, searchFixtureSrc, map[string][]string{})

	if _, err := s.CallSearch("things", "boom", MaxSearchTime); err == nil {
		t.Fatal("expected error for non-table return")
	} else if !strings.Contains(err.Error(), "must return a table") {
		t.Errorf("unexpected error: %v", err)
	}
}

func TestCallSearch_NilReturnErrors(t *testing.T) {
	s := newSearchTestSandbox(t, searchFixtureSrc, map[string][]string{})

	if _, err := s.CallSearch("things", "missing", MaxSearchTime); err == nil {
		t.Fatal("expected error for nil return")
	}
}

func TestCallSearch_MissingHandlerErrors(t *testing.T) {
	s := newSearchTestSandbox(t, `function something_else() end`, map[string][]string{})

	if _, err := s.CallSearch("things", "", MaxSearchTime); err == nil {
		t.Fatal("expected error when on_search is not defined")
	}
}

func TestCallSearch_CapsAtMaxSearchResults(t *testing.T) {
	src := `
function on_search(source, query)
    local rows = {}
    for i = 1, 80 do rows[i] = {value = tostring(i), label = "row" .. i} end
    return rows
end
`
	s := newSearchTestSandbox(t, src, map[string][]string{})

	choices, err := s.CallSearch("things", "", MaxSearchTime)
	if err != nil {
		t.Fatalf("CallSearch: %v", err)
	}
	if len(choices) != MaxSearchResults {
		t.Fatalf("expected %d choices, got %d", MaxSearchResults, len(choices))
	}
	if choices[len(choices)-1].Value != "50" {
		t.Errorf("expected cap at row 50, got last = %+v", choices[len(choices)-1])
	}
}

func TestCallSearch_BusySandboxReturnsErrPluginBusy(t *testing.T) {
	s := newSearchTestSandbox(t, searchFixtureSrc, map[string][]string{})

	// Simulate another entry point holding the sandbox lock.
	s.mu.Lock()
	defer s.mu.Unlock()

	done := make(chan struct{})
	var err error
	go func() {
		defer close(done)
		_, err = s.CallSearch("things", "", MaxSearchTime)
	}()

	select {
	case <-done:
		// CallSearch must return immediately with the typed error, not block
		// for MaxSearchTime.
		if err == nil || err.Error() != ErrPluginBusy.Error() {
			t.Fatalf("expected ErrPluginBusy, got %v", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("CallSearch blocked on a busy sandbox instead of returning ErrPluginBusy")
	}
}

// TestCallSearch_DeadlineCutsHTTPButHandlersKeepOldBudget proves two things:
// a search against a black-holed server gives up at the search timeout, while
// an event handler's HTTP call over the same module is left on the old
// five-minute budget (no budget active -> no request context).
func TestCallSearch_DeadlineCutsHTTPButHandlersKeepOldBudget(t *testing.T) {
	// A server that answers GET after a fixed delay.
	respondAfter := 1500 * time.Millisecond
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		time.Sleep(respondAfter)
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("finally"))
	}))
	defer srv.Close()
	// httptest listens on 127.0.0.1; the allowlist is matched on Hostname()
	// alone, so the port must not be part of the allowlist key.
	host := srv.URL[strings.LastIndex(srv.URL, "//")+2:]
	if idx := strings.LastIndex(host, ":"); idx >= 0 {
		host = host[:idx]
	}
	baseURL := srv.URL

	src := `
function on_search(source, query)
    local resp = http.get(query .. "/slow")
    return {{value = "done", label = resp.body}}
end

function on_clip_created(data)
    local resp = http.get(data.base .. "/slow")
    storage_hint = resp.body
    return
end
`
	allowed := map[string][]string{host: {"GET"}}
	s := newSearchTestSandbox(t, src, allowed)

	// Search gives up at its own timeout, not the 5-minute client timeout.
	start := time.Now()
	if _, err := s.CallSearch("things", baseURL, 300*time.Millisecond); err == nil {
		t.Fatal("expected the search to time out")
	}
	if elapsed := time.Since(start); elapsed > 2*time.Second {
		t.Fatalf("search took %v, expected ~300ms", elapsed)
	}

	// After the search the budget must be cleared again.
	if _, ok := s.httpBudget.Remaining(); ok {
		t.Fatal("HTTP budget still active after CallSearch returned")
	}

	// The event handler path is untouched: its request survives past the
	// search timeout and completes against the same slow server.
	handlerDone := make(chan error, 1)
	go func() {
		handlerDone <- s.CallHandlerWithData("on_clip_created", map[string]interface{}{"base": baseURL})
	}()

	select {
	case err := <-handlerDone:
		if err != nil {
			t.Fatalf("event handler HTTP call should complete unaffected, got: %v", err)
		}
	case <-time.After(15 * time.Second):
		t.Fatal("event handler did not finish; it may have been cut by the search budget")
	}
}
