package plugin

import (
	"context"
	"fmt"
	"sync"
	"time"

	lua "github.com/yuin/gopher-lua"
)

const (
	MaxExecutionTime = 30 * time.Second
	MaxMemoryMB      = 50
)

// Sandbox wraps a Lua state with resource limits
type Sandbox struct {
	L        *lua.LState
	manifest *Manifest
	pluginID int64
	mu       sync.Mutex
	cancel   context.CancelFunc
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

// CallUIAction calls the on_ui_action handler with proper context and returns the result
func (s *Sandbox) CallUIAction(actionID string, clipIDs []int64, options map[string]interface{}) (map[string]interface{}, error) {
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
	ctx, cancel := context.WithTimeout(context.Background(), MaxExecutionTime)
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

	// Convert options to Lua table
	optionsTable := s.L.NewTable()
	for k, v := range options {
		switch val := v.(type) {
		case string:
			optionsTable.RawSetString(k, lua.LString(val))
		case float64:
			optionsTable.RawSetString(k, lua.LNumber(val))
		case bool:
			optionsTable.RawSetString(k, lua.LBool(val))
		case int:
			optionsTable.RawSetString(k, lua.LNumber(val))
		}
	}

	// Push function and arguments
	s.L.Push(fn)
	s.L.Push(lua.LString(actionID))
	s.L.Push(clipIDsTable)
	s.L.Push(optionsTable)

	// Call with error handling
	err := s.L.PCall(3, 1, nil)
	if err != nil {
		if ctx.Err() == context.DeadlineExceeded {
			return nil, fmt.Errorf("on_ui_action timed out after %v", MaxExecutionTime)
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
	}

	return result, nil
}
