package plugin

import (
	"sync"
	"time"

	"go-clipboard/internal/bridgeiface"

	lua "github.com/yuin/gopher-lua"
)

// modalAckTimeout is how long to wait for the frontend to acknowledge
// receipt of a plugin:modal event before auto-releasing the lock.
// Only fires when no ack is received (e.g. startup race).
const modalAckTimeout = 5 * time.Second

const (
	maxModalTitleLength   = 200
	maxModalContentLength = 1 << 20
	maxModalCopyLength    = 1 << 20
	maxModalPasteLength   = 10 << 20
)

// modalGuard enforces the app-wide "one modal at a time" constraint.
// Shared across all ModalAPI instances so two different plugins cannot
// both pass the check simultaneously.
type modalGuard struct {
	mu      sync.Mutex
	showing bool
	acked   bool   // true once the frontend acknowledges the current modal
	gen     uint64 // incremented each time showing is set; used for timeout safety
}

// acquire tries to claim the modal slot. Returns (ok, generation).
func (g *modalGuard) acquire() (bool, uint64) {
	g.mu.Lock()
	defer g.mu.Unlock()
	if g.showing {
		return false, 0
	}
	g.showing = true
	g.acked = false
	g.gen++
	return true, g.gen
}

// startAckTimeout auto-releases the lock if the frontend never acknowledges
// the modal within modalAckTimeout (e.g. event emitted before JS listener
// is registered at startup).
func (g *modalGuard) startAckTimeout(gen uint64) {
	go func() {
		time.Sleep(modalAckTimeout)
		g.mu.Lock()
		if g.showing && g.gen == gen && !g.acked {
			g.showing = false
			g.gen++
		}
		g.mu.Unlock()
	}()
}

// markAcked records that the frontend received the current modal, cancelling
// the ack timeout's auto-release.
func (g *modalGuard) markAcked() {
	g.mu.Lock()
	g.acked = true
	g.mu.Unlock()
}

// release frees the modal slot so the next plugin modal can be shown.
func (g *modalGuard) release() {
	g.mu.Lock()
	g.showing = false
	g.acked = false
	g.gen++
	g.mu.Unlock()
}

func newModalGuard(b bridgeiface.Bridge) *modalGuard {
	g := &modalGuard{}
	// The desktop bridge can push these upstream events; the headless SSE bridge
	// cannot, so the server delivers the same signals via a REST endpoint that
	// calls markAcked/release directly (see Manager.NotifyModal*).
	b.On("plugin:modal:acked", func(data ...interface{}) { g.markAcked() })
	b.On("plugin:modal:closed", func(data ...interface{}) { g.release() })
	return g
}

// ModalAPI provides modal display functionality for plugins
type ModalAPI struct {
	bridge   bridgeiface.Bridge
	pluginID int64
	guard    *modalGuard
}

// NewModalAPI creates a new modal API instance.
// The guard is shared across all plugins to enforce single-modal globally.
func NewModalAPI(b bridgeiface.Bridge, pluginID int64, guard *modalGuard) *ModalAPI {
	return &ModalAPI{
		bridge:   b,
		pluginID: pluginID,
		guard:    guard,
	}
}

// Close releases resources held by this ModalAPI instance.
func (m *ModalAPI) Close() {
	// Nothing per-instance to clean up; the shared guard's event listener
	// is tied to the Manager lifetime, not individual plugins.
}

// Register adds the modal module to the Lua state
func (m *ModalAPI) Register(L *lua.LState) {
	modalMod := L.NewTable()
	modalMod.RawSetString("show", L.NewFunction(m.show))
	L.SetGlobal("modal", modalMod)
}

func (m *ModalAPI) show(L *lua.LState) int {
	opts := L.CheckTable(1)

	title := ""
	content := ""
	format := ""
	copyData := ""
	pasteData := ""
	pasteDataBase64 := false
	pasteName := ""
	pasteContentType := ""

	if v := opts.RawGetString("title"); v != lua.LNil {
		title = v.String()
	}
	if v := opts.RawGetString("content"); v != lua.LNil {
		content = v.String()
	}
	if v := opts.RawGetString("format"); v != lua.LNil {
		format = v.String()
	}
	if v := opts.RawGetString("copy_data"); v != lua.LNil {
		copyData = v.String()
	}
	if v := opts.RawGetString("paste_data"); v != lua.LNil {
		pasteData = v.String()
	}
	if v := opts.RawGetString("paste_data_base64"); v != lua.LNil {
		pasteDataBase64 = v == lua.LTrue
	}
	if v := opts.RawGetString("paste_name"); v != lua.LNil {
		pasteName = v.String()
	}
	if v := opts.RawGetString("paste_content_type"); v != lua.LNil {
		pasteContentType = v.String()
	}

	// Validate required fields
	if title == "" || content == "" || format == "" {
		L.Push(lua.LFalse)
		L.Push(lua.LString("title, content, and format are required"))
		return 2
	}

	validFormats := map[string]bool{"markdown": true, "image": true, "text": true}
	if !validFormats[format] {
		L.Push(lua.LFalse)
		L.Push(lua.LString("format must be 'markdown', 'image', or 'text'"))
		return 2
	}

	if len(title) > maxModalTitleLength {
		title = title[:maxModalTitleLength]
	}
	if len(content) > maxModalContentLength {
		L.Push(lua.LFalse)
		L.Push(lua.LString("content exceeds 1MB limit"))
		return 2
	}
	if len(copyData) > maxModalCopyLength {
		L.Push(lua.LFalse)
		L.Push(lua.LString("copy_data exceeds 1MB limit"))
		return 2
	}
	if len(pasteData) > maxModalPasteLength {
		L.Push(lua.LFalse)
		L.Push(lua.LString("paste_data exceeds 10MB limit"))
		return 2
	}

	// Rate limit: 1 modal at a time (app-wide)
	ok, gen := m.guard.acquire()
	if !ok {
		L.Push(lua.LFalse)
		L.Push(lua.LString("a modal is already showing"))
		return 2
	}
	m.guard.startAckTimeout(gen)

	md := &ModalData{
		PluginID:         m.pluginID,
		Title:            title,
		Content:          content,
		Format:           format,
		CopyData:         copyData,
		PasteData:        pasteData,
		PasteDataBase64:  pasteDataBase64,
		PasteName:        pasteName,
		PasteContentType: pasteContentType,
	}
	m.bridge.Emit("plugin:modal", md.ToEventMap())

	L.Push(lua.LTrue)
	return 1
}
