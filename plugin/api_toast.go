package plugin

import (
	"context"
	"html"
	"sync"
	"time"

	lua "github.com/yuin/gopher-lua"
	"github.com/wailsapp/wails/v2/pkg/runtime"
)

const (
	maxToastMessageLength = 200
	toastRateLimit        = 5 // per minute
)

// ToastAPI provides toast notification functionality for plugins
type ToastAPI struct {
	ctx      context.Context
	pluginID int64
	// Rate limiting
	mu        sync.Mutex
	callTimes []time.Time
}

// NewToastAPI creates a new toast API instance
func NewToastAPI(ctx context.Context, pluginID int64) *ToastAPI {
	return &ToastAPI{
		ctx:       ctx,
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

func (t *ToastAPI) show(L *lua.LState) int {
	message := L.CheckString(1)
	toastType := L.OptString(2, "info")

	// Validate type
	validTypes := map[string]bool{"info": true, "success": true, "error": true}
	if !validTypes[toastType] {
		toastType = "info"
	}

	// Truncate message if too long
	if len(message) > maxToastMessageLength {
		message = message[:maxToastMessageLength-3] + "..."
	}

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

	// Emit Wails event
	runtime.EventsEmit(t.ctx, "plugin:toast", map[string]string{
		"message": message,
		"type":    toastType,
	})

	L.Push(lua.LTrue)
	return 1
}
