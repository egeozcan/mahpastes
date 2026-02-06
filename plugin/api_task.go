package plugin

import (
	"context"
	"sync"
	"sync/atomic"
	"time"

	lua "github.com/yuin/gopher-lua"
	"github.com/wailsapp/wails/v2/pkg/runtime"
)

const (
	// TaskCleanupDelay is how long to wait before cleaning up completed/failed tasks
	TaskCleanupDelay = 5 * time.Minute
)

// globalTaskID is a package-level counter for globally unique task IDs across all plugins
var globalTaskID int64

// TaskAPI provides task queue integration for plugins
type TaskAPI struct {
	ctx      context.Context
	tasks    map[int64]*pluginTask
	taskMu   sync.RWMutex
	pluginID int64
}

type pluginTask struct {
	ID       int64
	Name     string
	Total    int
	Current  int
	Status   string // "running", "completed", "failed"
	Error    string
}

// NewTaskAPI creates a new task API instance
func NewTaskAPI(ctx context.Context, pluginID int64) *TaskAPI {
	return &TaskAPI{
		ctx:      ctx,
		tasks:    make(map[int64]*pluginTask),
		pluginID: pluginID,
	}
}

// Register adds the task module to the Lua state
func (t *TaskAPI) Register(L *lua.LState) {
	taskMod := L.NewTable()

	taskMod.RawSetString("start", L.NewFunction(t.start))
	taskMod.RawSetString("progress", L.NewFunction(t.progress))
	taskMod.RawSetString("complete", L.NewFunction(t.complete))
	taskMod.RawSetString("fail", L.NewFunction(t.fail))

	L.SetGlobal("task", taskMod)
}

func (t *TaskAPI) start(L *lua.LState) int {
	name := L.CheckString(1)
	total := L.OptInt(2, 1)

	taskID := atomic.AddInt64(&globalTaskID, 1)

	task := &pluginTask{
		ID:      taskID,
		Name:    name,
		Total:   total,
		Current: 0,
		Status:  "running",
	}

	t.taskMu.Lock()
	t.tasks[taskID] = task
	t.taskMu.Unlock()

	// Emit task started event to frontend
	if t.ctx != nil {
		runtime.EventsEmit(t.ctx, "plugin:task:started", map[string]interface{}{
			"task_id":   taskID,
			"plugin_id": t.pluginID,
			"name":      name,
			"total":     total,
		})
	}

	L.Push(lua.LNumber(taskID))
	return 1
}

func (t *TaskAPI) progress(L *lua.LState) int {
	taskID := L.CheckInt64(1)
	current := L.CheckInt(2)

	t.taskMu.Lock()
	task, ok := t.tasks[taskID]
	if ok {
		task.Current = current
	}
	t.taskMu.Unlock()

	if !ok {
		L.Push(lua.LFalse)
		L.Push(lua.LString("task not found"))
		return 2
	}

	// Emit progress event to frontend
	if t.ctx != nil {
		runtime.EventsEmit(t.ctx, "plugin:task:progress", map[string]interface{}{
			"task_id":   taskID,
			"plugin_id": t.pluginID,
			"current":   current,
			"total":     task.Total,
			"name":      task.Name,
		})
	}

	L.Push(lua.LTrue)
	return 1
}

func (t *TaskAPI) complete(L *lua.LState) int {
	taskID := L.CheckInt64(1)

	t.taskMu.Lock()
	task, ok := t.tasks[taskID]
	if ok {
		task.Status = "completed"
		task.Current = task.Total
	}
	t.taskMu.Unlock()

	if !ok {
		L.Push(lua.LFalse)
		L.Push(lua.LString("task not found"))
		return 2
	}

	// Emit completion event to frontend
	if t.ctx != nil {
		runtime.EventsEmit(t.ctx, "plugin:task:completed", map[string]interface{}{
			"task_id":   taskID,
			"plugin_id": t.pluginID,
			"name":      task.Name,
		})
	}

	// Schedule cleanup after delay to prevent memory leaks
	t.scheduleCleanup(taskID)

	L.Push(lua.LTrue)
	return 1
}

func (t *TaskAPI) fail(L *lua.LState) int {
	taskID := L.CheckInt64(1)
	errMsg := L.OptString(2, "Unknown error")

	t.taskMu.Lock()
	task, ok := t.tasks[taskID]
	if ok {
		task.Status = "failed"
		task.Error = errMsg
	}
	t.taskMu.Unlock()

	if !ok {
		L.Push(lua.LFalse)
		L.Push(lua.LString("task not found"))
		return 2
	}

	// Emit failure event to frontend
	if t.ctx != nil {
		runtime.EventsEmit(t.ctx, "plugin:task:failed", map[string]interface{}{
			"task_id":   taskID,
			"plugin_id": t.pluginID,
			"name":      task.Name,
			"error":     errMsg,
		})
	}

	// Schedule cleanup after delay to prevent memory leaks
	t.scheduleCleanup(taskID)

	L.Push(lua.LTrue)
	return 1
}

// scheduleCleanup removes the task from memory after a delay
func (t *TaskAPI) scheduleCleanup(taskID int64) {
	go func() {
		time.Sleep(TaskCleanupDelay)
		t.taskMu.Lock()
		delete(t.tasks, taskID)
		t.taskMu.Unlock()
	}()
}

// GetTask returns a task by ID (for testing)
func (t *TaskAPI) GetTask(taskID int64) (*pluginTask, bool) {
	t.taskMu.RLock()
	defer t.taskMu.RUnlock()
	task, ok := t.tasks[taskID]
	return task, ok
}
