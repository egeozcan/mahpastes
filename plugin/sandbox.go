package plugin

import (
	"context"
	"errors"
	"fmt"
	"strconv"
	"sync"
	"time"

	lua "github.com/yuin/gopher-lua"
)

const (
	MaxExecutionTime = 30 * time.Second
	MaxUIActionTime  = 5 * time.Minute // for long-running UI actions (e.g. AI processing)
	MaxMemoryMB      = 50
	MaxSearchTime    = 15 * time.Second // for on_search: the hook makes an HTTP call, not an AI run
	MaxSearchResults = 50               // matches mahresources' fixed page size; keeps dropdown payloads bounded
)

// ErrPluginBusy is returned by CallSearch when the plugin's sandbox is already
// running another entry point. The picker shows it as a transient "busy" row
// instead of queueing behind, say, a five-minute async upload.
var ErrPluginBusy = errors.New("plugin is busy")

// Sandbox wraps a Lua state with resource limits
type Sandbox struct {
	L          *lua.LState
	manifest   *Manifest
	pluginID   int64
	mu         sync.Mutex
	cancel     context.CancelFunc
	httpBudget *HTTPBudget
}

// NewSandbox creates a new sandboxed Lua environment
func NewSandbox(manifest *Manifest, pluginID int64) *Sandbox {
	L := lua.NewState(lua.Options{
		SkipOpenLibs: true,
	})

	// Open only safe libraries
	lua.OpenBase(L)
	lua.OpenTable(L)
	lua.OpenString(L)
	lua.OpenMath(L)

	// Remove dangerous functions from base
	L.SetGlobal("dofile", lua.LNil)
	L.SetGlobal("loadfile", lua.LNil)
	L.SetGlobal("load", lua.LNil)
	L.SetGlobal("loadstring", lua.LNil)
	L.SetGlobal("rawequal", lua.LNil)
	L.SetGlobal("rawget", lua.LNil)
	L.SetGlobal("rawset", lua.LNil)
	L.SetGlobal("getmetatable", lua.LNil)
	L.SetGlobal("setmetatable", lua.LNil)
	L.SetGlobal("collectgarbage", lua.LNil)

	return &Sandbox{
		L:        L,
		manifest: manifest,
		pluginID: pluginID,
	}
}

// Close shuts down the sandbox
func (s *Sandbox) Close() {
	s.mu.Lock()
	defer s.mu.Unlock()

	if s.cancel != nil {
		s.cancel()
	}
	s.L.Close()
}

// LoadSource loads and executes the plugin source with timeout protection
func (s *Sandbox) LoadSource(source string) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	// Create context with timeout to prevent infinite loops during load
	ctx, cancel := context.WithTimeout(context.Background(), MaxExecutionTime)
	s.cancel = cancel
	defer func() {
		cancel()
		s.cancel = nil
	}()

	s.L.SetContext(ctx)

	err := s.L.DoString(source)
	if err != nil {
		if ctx.Err() == context.DeadlineExceeded {
			return fmt.Errorf("plugin load timed out after %v", MaxExecutionTime)
		}
		return err
	}
	return nil
}

// CallHandler calls a handler function with timeout
func (s *Sandbox) CallHandler(name string, args ...lua.LValue) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	fn := s.L.GetGlobal(name)
	if fn == lua.LNil {
		return nil // Handler not defined, skip silently
	}

	if _, ok := fn.(*lua.LFunction); !ok {
		return fmt.Errorf("%s is not a function", name)
	}

	// Create context with timeout
	ctx, cancel := context.WithTimeout(context.Background(), MaxExecutionTime)
	s.cancel = cancel
	defer func() {
		cancel()
		s.cancel = nil
	}()

	// Set up cancellation check
	s.L.SetContext(ctx)

	// Push function and arguments
	s.L.Push(fn)
	for _, arg := range args {
		s.L.Push(arg)
	}

	// Call with error handling
	err := s.L.PCall(len(args), 0, nil)
	if err != nil {
		if ctx.Err() == context.DeadlineExceeded {
			return fmt.Errorf("handler %s timed out after %v", name, MaxExecutionTime)
		}
		return fmt.Errorf("handler %s failed: %w", name, err)
	}

	return nil
}

// CallHandlerWithData calls a handler function with Go data that will be converted to Lua inside the mutex
func (s *Sandbox) CallHandlerWithData(name string, data interface{}) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	fn := s.L.GetGlobal(name)
	if fn == lua.LNil {
		return nil // Handler not defined, skip silently
	}

	if _, ok := fn.(*lua.LFunction); !ok {
		return fmt.Errorf("%s is not a function", name)
	}

	// Create context with timeout
	ctx, cancel := context.WithTimeout(context.Background(), MaxExecutionTime)
	s.cancel = cancel
	defer func() {
		cancel()
		s.cancel = nil
	}()

	// Set up cancellation check
	s.L.SetContext(ctx)

	// Push function
	s.L.Push(fn)

	// Convert and push data argument (done inside mutex for thread safety)
	argCount := 0
	if data != nil {
		s.L.Push(goToLua(s.L, data))
		argCount = 1
	}

	// Call with error handling
	err := s.L.PCall(argCount, 0, nil)
	if err != nil {
		if ctx.Err() == context.DeadlineExceeded {
			return fmt.Errorf("handler %s timed out after %v", name, MaxExecutionTime)
		}
		return fmt.Errorf("handler %s failed: %w", name, err)
	}

	return nil
}

// SetGlobalTable sets a table as a global
func (s *Sandbox) SetGlobalTable(name string, tbl *lua.LTable) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.L.SetGlobal(name, tbl)
}

// SetGlobalFunction sets a function as a global
func (s *Sandbox) SetGlobalFunction(name string, fn lua.LGFunction) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.L.SetGlobal(name, s.L.NewFunction(fn))
}

// GetState returns the underlying Lua state (for API registration)
func (s *Sandbox) GetState() *lua.LState {
	return s.L
}

// GetManifest returns the plugin manifest
func (s *Sandbox) GetManifest() *Manifest {
	return s.manifest
}

// GetPluginID returns the plugin database ID
func (s *Sandbox) GetPluginID() int64 {
	return s.pluginID
}

// SetHTTPBudget gives the sandbox the shared HTTP deadline holder its http
// module was built with. CallSearch sets it around the on_search call so the
// in-flight request respects the search deadline; every other entry point
// leaves it unset and plugin HTTP behavior is unchanged.
func (s *Sandbox) SetHTTPBudget(b *HTTPBudget) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.httpBudget = b
}

// scalarMapToLuaTable converts a Go map of scalar values into a Lua table.
// Non-scalar values are skipped. nil maps yield an empty table.
func scalarMapToLuaTable(L *lua.LState, m map[string]interface{}) *lua.LTable {
	t := L.NewTable()
	for k, v := range m {
		switch val := v.(type) {
		case string:
			t.RawSetString(k, lua.LString(val))
		case float64:
			t.RawSetString(k, lua.LNumber(val))
		case bool:
			t.RawSetString(k, lua.LBool(val))
		case int:
			t.RawSetString(k, lua.LNumber(val))
		case int64:
			t.RawSetString(k, lua.LNumber(val))
		}
	}
	return t
}

// CallSearch calls the on_search(source, query) handler and converts the
// returned rows to choices. Unlike CallUIAction it acquires the sandbox lock
// with TryLock and returns ErrPluginBusy when occupied: a blocking search
// would otherwise wait far outside its own timeout, because an async action
// takes the lock before creating its deadline and may hold it for
// MaxUIActionTime. The picker must never queue behind an upload.
func (s *Sandbox) CallSearch(source, query string, timeout time.Duration) ([]Choice, error) {
	if !s.mu.TryLock() {
		return nil, ErrPluginBusy
	}
	defer s.mu.Unlock()

	fn := s.L.GetGlobal("on_search")
	if fn == lua.LNil {
		return nil, fmt.Errorf("plugin does not implement on_search")
	}
	if _, ok := fn.(*lua.LFunction); !ok {
		return nil, fmt.Errorf("on_search is not a function")
	}

	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer func() {
		cancel()
		s.cancel = nil
	}()
	s.cancel = cancel

	s.L.SetContext(ctx)

	if s.httpBudget != nil {
		s.httpBudget.Set(timeout)
		defer s.httpBudget.Clear()
	}

	s.L.Push(fn)
	s.L.Push(lua.LString(source))
	s.L.Push(lua.LString(query))

	err := s.L.PCall(2, 1, nil)
	if err != nil {
		if ctx.Err() == context.DeadlineExceeded {
			return nil, fmt.Errorf("on_search timed out after %v", timeout)
		}
		return nil, fmt.Errorf("on_search failed: %w", err)
	}

	ret := s.L.Get(-1)
	s.L.Pop(1)

	tbl, ok := ret.(*lua.LTable)
	if !ok {
		return nil, fmt.Errorf("on_search must return a table of rows")
	}

	rows := tbl.Len()
	if rows > MaxSearchResults {
		rows = MaxSearchResults
	}
	choices := make([]Choice, 0, rows)
	for i := 1; i <= rows; i++ {
		row, ok := tbl.RawGetInt(i).(*lua.LTable)
		if !ok {
			continue // malformed row: skip rather than panic
		}
		value := luaValueToChoiceString(row.RawGetString("value"))
		if value == "" {
			continue
		}
		label := luaValueToChoiceString(row.RawGetString("label"))
		if label == "" {
			label = value
		}
		choices = append(choices, Choice{Value: value, Label: label})
	}
	return choices, nil
}

// luaValueToChoiceString coerces a Lua scalar to a display/submit string.
// Numbers are formatted without a trailing decimal (12 -> "12", not "12.0").
func luaValueToChoiceString(v lua.LValue) string {
	switch v := v.(type) {
	case *lua.LNilType:
		return ""
	case lua.LString:
		return string(v)
	case lua.LNumber:
		return strconv.FormatFloat(float64(v), 'f', -1, 64)
	case lua.LBool:
		if v {
			return "true"
		}
		return "false"
	default:
		return v.String()
	}
}

// CallUIAction calls the on_ui_action handler with proper context and returns the result.
// The timeout parameter controls how long the action is allowed to run.
// actionContext carries invocation context (e.g. the active folder's tag) and is passed
// to the handler as a fourth argument: on_ui_action(action_id, clip_ids, options, context).
func (s *Sandbox) CallUIAction(actionID string, clipIDs []int64, options map[string]interface{}, actionContext map[string]interface{}, timeout time.Duration) (map[string]interface{}, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	fn := s.L.GetGlobal("on_ui_action")
	if fn == lua.LNil {
		return nil, fmt.Errorf("plugin does not implement on_ui_action")
	}

	if _, ok := fn.(*lua.LFunction); !ok {
		return nil, fmt.Errorf("on_ui_action is not a function")
	}

	// Create context with timeout
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	s.cancel = cancel
	defer func() {
		cancel()
		s.cancel = nil
	}()

	// Set up cancellation check
	s.L.SetContext(ctx)

	// Convert clip_ids to Lua table
	clipIDsTable := s.L.NewTable()
	for _, id := range clipIDs {
		clipIDsTable.Append(lua.LNumber(id))
	}

	// Convert options and context to Lua tables
	optionsTable := scalarMapToLuaTable(s.L, options)
	contextTable := scalarMapToLuaTable(s.L, actionContext)

	// Push function and arguments
	s.L.Push(fn)
	s.L.Push(lua.LString(actionID))
	s.L.Push(clipIDsTable)
	s.L.Push(optionsTable)
	s.L.Push(contextTable)

	// Call with error handling
	err := s.L.PCall(4, 1, nil)
	if err != nil {
		if ctx.Err() == context.DeadlineExceeded {
			return nil, fmt.Errorf("on_ui_action timed out after %v", timeout)
		}
		return nil, fmt.Errorf("on_ui_action failed: %w", err)
	}

	// Get return value
	result := make(map[string]interface{})
	ret := s.L.Get(-1)
	s.L.Pop(1)

	if tbl, ok := ret.(*lua.LTable); ok {
		tbl.ForEach(func(k, v lua.LValue) {
			if key, ok := k.(lua.LString); ok {
				switch val := v.(type) {
				case lua.LNumber:
					result[string(key)] = int64(val)
				case lua.LString:
					result[string(key)] = string(val)
				case lua.LBool:
					result[string(key)] = bool(val)
				}
			}
		})

		// Check for nested modal table
		if modalVal := tbl.RawGetString("modal"); modalVal != nil {
			if modalTbl, ok := modalVal.(*lua.LTable); ok {
				modalData := make(map[string]interface{})
				modalTbl.ForEach(func(k, v lua.LValue) {
					if key, ok := k.(lua.LString); ok {
						switch val := v.(type) {
						case lua.LNumber:
							modalData[string(key)] = int64(val)
						case lua.LString:
							modalData[string(key)] = string(val)
						case lua.LBool:
							modalData[string(key)] = bool(val)
						}
					}
				})
				result["modal"] = modalData
			}
		}
	}

	return result, nil
}
