package plugin

import (
	"html"
	"strconv"
	"strings"
	"sync"
	"time"
	"unicode/utf8"

	"go-clipboard/internal/bridgeiface"

	lua "github.com/yuin/gopher-lua"
)

const (
	maxToastMessageLength = 200
	toastRateLimit        = 5 // per minute
)

// ToastAPI provides toast notification functionality for plugins
type ToastAPI struct {
	bridge   bridgeiface.Bridge
	pluginID int64
	// Rate limiting
	mu        sync.Mutex
	callTimes []time.Time
}

// NewToastAPI creates a new toast API instance
func NewToastAPI(b bridgeiface.Bridge, pluginID int64) *ToastAPI {
	return &ToastAPI{
		bridge:    b,
		pluginID:  pluginID,
		callTimes: make([]time.Time, 0, toastRateLimit),
	}
}

// Register adds the toast module to the Lua state
func (t *ToastAPI) Register(L *lua.LState) {
	toastMod := L.NewTable()
	toastMod.RawSetString("show", L.NewFunction(t.show))
	L.SetGlobal("toast", toastMod)
}

// truncateToastMessage caps message at maxToastMessageLength bytes. Lua
// strings are raw bytes and a byte cut can split a character; invalid UTF-8
// reaches macOS as a notification with no body at all.
func truncateToastMessage(message string) string {
	message = strings.ToValidUTF8(message, "\uFFFD")
	if len(message) <= maxToastMessageLength {
		return message
	}
	cut := maxToastMessageLength - 3
	for cut > 0 && !utf8.RuneStart(message[cut]) {
		cut--
	}
	return message[:cut] + "..."
}

func (t *ToastAPI) show(L *lua.LState) int {
	message := L.CheckString(1)
	toastType := L.OptString(2, "info")

	// Validate type
	validTypes := map[string]bool{"info": true, "success": true, "error": true}
	if !validTypes[toastType] {
		toastType = "info"
	}

	message = truncateToastMessage(message)

	// HTML escape the message
	message = html.EscapeString(message)

	// Rate limiting
	t.mu.Lock()
	now := time.Now()
	// Remove calls older than 1 minute
	cutoff := now.Add(-time.Minute)
	validCalls := make([]time.Time, 0, len(t.callTimes))
	for _, ct := range t.callTimes {
		if ct.After(cutoff) {
			validCalls = append(validCalls, ct)
		}
	}
	t.callTimes = validCalls

	// Check if rate limited
	if len(t.callTimes) >= toastRateLimit {
		t.mu.Unlock()
		// Silently drop - don't error
		L.Push(lua.LFalse)
		return 1
	}

	t.callTimes = append(t.callTimes, now)
	t.mu.Unlock()

	// Emit Wails event. plugin_id lets a system notification for this toast
	// name the plugin; the in-app toast ignores it.
	t.bridge.Emit("plugin:toast", map[string]string{
		"message":   message,
		"type":      toastType,
		"plugin_id": strconv.FormatInt(t.pluginID, 10),
	})

	L.Push(lua.LTrue)
	return 1
}
