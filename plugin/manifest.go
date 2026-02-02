package plugin

import (
	"fmt"
	"strings"

	lua "github.com/yuin/gopher-lua"
)

// Manifest represents a parsed plugin manifest
type Manifest struct {
	Name        string
	Version     string
	Description string
	Author      string
	Network     map[string][]string // domain -> allowed methods
	Filesystem  FilesystemPerms
	Events      []string
	Schedules   []Schedule
}

// FilesystemPerms represents filesystem permission requests
type FilesystemPerms struct {
	Read  bool
	Write bool
}

// Schedule represents a scheduled task
type Schedule struct {
	Name     string
	Interval int // seconds
}

// ParseManifest extracts the Plugin table from Lua source
func ParseManifest(source string) (*Manifest, error) {
	L := lua.NewState()
	defer L.Close()

	// Execute the source to populate the Plugin global
	if err := L.DoString(source); err != nil {
		return nil, fmt.Errorf("failed to parse plugin: %w", err)
	}

	// Get the Plugin table
	pluginTable := L.GetGlobal("Plugin")
	if pluginTable == lua.LNil {
		return nil, fmt.Errorf("plugin must define a Plugin table")
	}

	tbl, ok := pluginTable.(*lua.LTable)
	if !ok {
		return nil, fmt.Errorf("Plugin must be a table")
	}

	manifest := &Manifest{
		Network: make(map[string][]string),
	}

	// Parse required fields
	if name := tbl.RawGetString("name"); name != lua.LNil {
		manifest.Name = name.String()
	} else {
		return nil, fmt.Errorf("plugin must have a name")
	}

	// Parse optional fields
	if version := tbl.RawGetString("version"); version != lua.LNil {
		manifest.Version = version.String()
	}
	if desc := tbl.RawGetString("description"); desc != lua.LNil {
		manifest.Description = desc.String()
	}
	if author := tbl.RawGetString("author"); author != lua.LNil {
		manifest.Author = author.String()
	}

	// Parse network permissions
	if network := tbl.RawGetString("network"); network != lua.LNil {
		if netTbl, ok := network.(*lua.LTable); ok {
			netTbl.ForEach(func(domain, methods lua.LValue) {
				domainStr := domain.String()
				var methodList []string
				if methodsTbl, ok := methods.(*lua.LTable); ok {
					methodsTbl.ForEach(func(_, method lua.LValue) {
						methodList = append(methodList, strings.ToUpper(method.String()))
					})
				}
				manifest.Network[domainStr] = methodList
			})
		}
	}

	// Parse filesystem permissions
	if fs := tbl.RawGetString("filesystem"); fs != lua.LNil {
		if fsTbl, ok := fs.(*lua.LTable); ok {
			if read := fsTbl.RawGetString("read"); read == lua.LTrue {
				manifest.Filesystem.Read = true
			}
			if write := fsTbl.RawGetString("write"); write == lua.LTrue {
				manifest.Filesystem.Write = true
			}
		}
	}

	// Parse events
	if events := tbl.RawGetString("events"); events != lua.LNil {
		if eventsTbl, ok := events.(*lua.LTable); ok {
			eventsTbl.ForEach(func(_, event lua.LValue) {
				manifest.Events = append(manifest.Events, event.String())
			})
		}
	}

	// Parse schedules
	if schedules := tbl.RawGetString("schedules"); schedules != lua.LNil {
		if schedulesTbl, ok := schedules.(*lua.LTable); ok {
			schedulesTbl.ForEach(func(_, sched lua.LValue) {
				if schedTbl, ok := sched.(*lua.LTable); ok {
					schedule := Schedule{}
					if name := schedTbl.RawGetString("name"); name != lua.LNil {
						schedule.Name = name.String()
					}
					if interval := schedTbl.RawGetString("interval"); interval != lua.LNil {
						if num, ok := interval.(lua.LNumber); ok {
							schedule.Interval = int(num)
						}
					}
					if schedule.Name != "" && schedule.Interval > 0 {
						manifest.Schedules = append(manifest.Schedules, schedule)
					}
				}
			})
		}
	}

	return manifest, nil
}

// ValidEvents returns the list of valid event names
func ValidEvents() []string {
	return []string{
		"app:startup",
		"app:shutdown",
		"clip:created",
		"clip:deleted",
		"clip:archived",
		"watch:file_detected",
		"watch:import_complete",
	}
}

// IsValidEvent checks if an event name is valid
func IsValidEvent(event string) bool {
	for _, valid := range ValidEvents() {
		if event == valid {
			return true
		}
	}
	return false
}
