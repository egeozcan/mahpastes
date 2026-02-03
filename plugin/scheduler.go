package plugin

import (
	"fmt"
	"log"
	"sync"
	"time"
)

// ScheduledTask represents a running scheduled task
type ScheduledTask struct {
	name     string
	interval time.Duration
	sandbox  *Sandbox
	stopCh   chan struct{}
	running  bool
	stopped  bool // Prevents double-close of stopCh
	mu       sync.Mutex
}

// Scheduler manages scheduled tasks for plugins
type Scheduler struct {
	tasks map[string]*ScheduledTask // key: pluginID:taskName
	mu    sync.RWMutex
}

// NewScheduler creates a new scheduler
func NewScheduler() *Scheduler {
	return &Scheduler{
		tasks: make(map[string]*ScheduledTask),
	}
}

// AddTask adds a scheduled task
func (s *Scheduler) AddTask(pluginID int64, taskName string, interval int, sandbox *Sandbox) {
	s.mu.Lock()
	defer s.mu.Unlock()

	key := taskKey(pluginID, taskName)

	// Stop existing task if any
	if existing, ok := s.tasks[key]; ok {
		existing.Stop()
	}

	task := &ScheduledTask{
		name:     taskName,
		interval: time.Duration(interval) * time.Second,
		sandbox:  sandbox,
		stopCh:   make(chan struct{}),
	}

	s.tasks[key] = task
	go task.run()
}

// RemovePluginTasks removes all tasks for a plugin
func (s *Scheduler) RemovePluginTasks(pluginID int64) {
	s.mu.Lock()
	defer s.mu.Unlock()

	prefix := taskKeyPrefix(pluginID)
	for key, task := range s.tasks {
		if len(key) >= len(prefix) && key[:len(prefix)] == prefix {
			task.Stop()
			delete(s.tasks, key)
		}
	}
}

// StopAll stops all scheduled tasks
func (s *Scheduler) StopAll() {
	s.mu.Lock()
	defer s.mu.Unlock()

	for _, task := range s.tasks {
		task.Stop()
	}
	s.tasks = make(map[string]*ScheduledTask)
}

func taskKey(pluginID int64, taskName string) string {
	return fmt.Sprintf("%d:%s", pluginID, taskName)
}

func taskKeyPrefix(pluginID int64) string {
	return fmt.Sprintf("%d:", pluginID)
}

func (t *ScheduledTask) run() {
	t.mu.Lock()
	t.running = true
	t.mu.Unlock()

	ticker := time.NewTicker(t.interval)
	defer ticker.Stop()

	for {
		select {
		case <-t.stopCh:
			return
		case <-ticker.C:
			t.execute()
		}
	}
}

func (t *ScheduledTask) execute() {
	// Recover from panics to prevent goroutine termination
	defer func() {
		if r := recover(); r != nil {
			log.Printf("Scheduled task %s panicked: %v", t.name, r)
		}
	}()

	t.mu.Lock()
	if !t.running || t.sandbox == nil {
		t.mu.Unlock()
		return
	}
	// Capture sandbox reference while holding lock to prevent nil after unlock
	sandbox := t.sandbox
	t.mu.Unlock()

	// Call the handler function named after the task
	if err := sandbox.CallHandler(t.name); err != nil {
		log.Printf("Scheduled task %s failed: %v", t.name, err)
	}
}

// Stop stops the scheduled task
func (t *ScheduledTask) Stop() {
	t.mu.Lock()
	defer t.mu.Unlock()

	if t.running && !t.stopped {
		t.running = false
		t.stopped = true
		close(t.stopCh)
	}
}
