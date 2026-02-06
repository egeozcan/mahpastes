package plugin

import (
	"fmt"
	"regexp"
	"strconv"
	"strings"
)

// Pre-compiled regexes for manifest parsing (only static patterns)
var (
	rePluginTable     = regexp.MustCompile(`(?m)^Plugin\s*=\s*\{`)
	reQuotedStrings   = regexp.MustCompile(`["']([^"']+)["']`)
	reNetworkBlock    = regexp.MustCompile(`network\s*=\s*\{`)
	reBracketDomain   = regexp.MustCompile(`\["([^"]+)"\]\s*=\s*\{([^}]*)\}`)
	reSimpleDomain    = regexp.MustCompile(`(\w+)\s*=\s*\{([^}]*)\}`)
	reSchedulesBlock  = regexp.MustCompile(`schedules\s*=\s*\{`)
	reNameField       = regexp.MustCompile(`name\s*=\s*["']([^"']+)["']`)
	reIntervalField   = regexp.MustCompile(`interval\s*=\s*(\d+)`)
	reSettingsBlock   = regexp.MustCompile(`settings\s*=\s*\{`)
	reUIBlock         = regexp.MustCompile(`ui\s*=\s*\{`)
	reOptionsBlock    = regexp.MustCompile(`options\s*=\s*\{`)
	reRequiredField   = regexp.MustCompile(`required\s*=\s*(true|false)`)
	reChoicesBlock    = regexp.MustCompile(`choices\s*=\s*\{`)
	reDefaultBool     = regexp.MustCompile(`default\s*=\s*(true|false)`)
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
	Settings    []SettingField
	UI          *UIManifest
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

// SettingField represents a plugin setting declaration
type SettingField struct {
	Key         string   `json:"key"`
	Type        string   `json:"type"`
	Label       string   `json:"label"`
	Description string   `json:"description,omitempty"`
	Default     any      `json:"default,omitempty"`
	Options     []string `json:"options,omitempty"`
}

// UIManifest represents plugin UI declarations
type UIManifest struct {
	LightboxButtons []UIAction `json:"lightbox_buttons,omitempty"`
	CardActions     []UIAction `json:"card_actions,omitempty"`
}

// UIAction represents a plugin-defined action button
type UIAction struct {
	ID      string      `json:"id"`
	Label   string      `json:"label"`
	Icon    string      `json:"icon,omitempty"`
	Async   bool        `json:"async,omitempty"`
	Options []FormField `json:"options,omitempty"`
}

// FormField represents a form field in an options dialog
type FormField struct {
	ID       string   `json:"id"`
	Type     string   `json:"type"` // text, password, checkbox, select, range
	Label    string   `json:"label"`
	Required bool     `json:"required,omitempty"`
	Default  any      `json:"default,omitempty"`
	Choices  []Choice `json:"choices,omitempty"` // for select
	Min      float64  `json:"min,omitempty"`     // for range
	Max      float64  `json:"max,omitempty"`     // for range
	Step     float64  `json:"step,omitempty"`    // for range
}

// Choice represents a select option
type Choice struct {
	Value string `json:"value"`
	Label string `json:"label"`
}

// ParseManifest extracts the Plugin table from Lua source using text parsing.
// This does NOT execute any Lua code - it only parses the declarative Plugin table.
func ParseManifest(source string) (*Manifest, error) {
	// Find the Plugin table assignment
	pluginBlock, err := extractPluginTable(source)
	if err != nil {
		return nil, err
	}

	manifest := &Manifest{
		Network: make(map[string][]string),
	}

	// Parse required fields
	manifest.Name = extractStringField(pluginBlock, "name")
	if manifest.Name == "" {
		return nil, fmt.Errorf("plugin must have a name")
	}

	// Parse optional string fields
	manifest.Version = extractStringField(pluginBlock, "version")
	manifest.Description = extractStringField(pluginBlock, "description")
	manifest.Author = extractStringField(pluginBlock, "author")

	// Parse filesystem permissions
	manifest.Filesystem.Read = extractBoolField(pluginBlock, "filesystem", "read")
	manifest.Filesystem.Write = extractBoolField(pluginBlock, "filesystem", "write")

	// Parse events array
	manifest.Events = extractStringArray(pluginBlock, "events")

	// Parse network permissions
	manifest.Network = extractNetworkPerms(pluginBlock)

	// Parse schedules
	manifest.Schedules = extractSchedules(pluginBlock)

	// Parse settings
	manifest.Settings = extractSettings(pluginBlock)

	// Parse UI declarations
	manifest.UI = extractUI(pluginBlock)

	return manifest, nil
}

// extractPluginTable finds and extracts the Plugin = { ... } block
func extractPluginTable(source string) (string, error) {
	// Match Plugin = { with possible whitespace variations
	loc := rePluginTable.FindStringIndex(source)
	if loc == nil {
		return "", fmt.Errorf("plugin must define a Plugin table")
	}

	// Find matching closing brace by counting braces
	start := loc[1] - 1 // Position of opening brace
	braceCount := 0
	inString := false
	stringChar := byte(0)
	escaped := false
	inMultiLineString := false
	multiLineLevel := 0 // For [=[ style strings with = counts

	for i := start; i < len(source); i++ {
		c := source[i]

		// Handle multi-line string [[...]] or [=[...]=]
		if !inString && !inMultiLineString {
			if c == '[' && i+1 < len(source) {
				// Check for [[ or [=[
				level := 0
				j := i + 1
				for j < len(source) && source[j] == '=' {
					level++
					j++
				}
				if j < len(source) && source[j] == '[' {
					inMultiLineString = true
					multiLineLevel = level
					i = j // Skip to after [[
					continue
				}
			}
		}

		if inMultiLineString {
			// Look for closing ]] or ]=]
			if c == ']' {
				level := 0
				j := i + 1
				for j < len(source) && source[j] == '=' {
					level++
					j++
				}
				if j < len(source) && source[j] == ']' && level == multiLineLevel {
					inMultiLineString = false
					i = j // Skip to after ]]
				}
			}
			continue
		}

		if escaped {
			escaped = false
			continue
		}

		if c == '\\' && inString {
			escaped = true
			continue
		}

		if !inString {
			if c == '"' || c == '\'' {
				inString = true
				stringChar = c
			} else if c == '{' {
				braceCount++
			} else if c == '}' {
				braceCount--
				if braceCount == 0 {
					return source[start : i+1], nil
				}
			}
		} else {
			if c == stringChar {
				inString = false
			}
		}
	}

	return "", fmt.Errorf("unbalanced braces in Plugin table")
}

// extractStringField extracts a simple string field like: name = "value"
func extractStringField(block, field string) string {
	// Match: field = "value" or field = 'value'
	patterns := []string{
		fmt.Sprintf(`%s\s*=\s*"([^"]*)"`, regexp.QuoteMeta(field)),
		fmt.Sprintf(`%s\s*=\s*'([^']*)'`, regexp.QuoteMeta(field)),
	}

	for _, pattern := range patterns {
		re := regexp.MustCompile(pattern)
		matches := re.FindStringSubmatch(block)
		if len(matches) >= 2 {
			return matches[1]
		}
	}

	return ""
}

// extractBoolField extracts a boolean from a nested table: parent = { field = true }
func extractBoolField(block, parent, field string) bool {
	// First find the parent table
	parentPattern := fmt.Sprintf(`%s\s*=\s*\{([^}]*)\}`, regexp.QuoteMeta(parent))
	re := regexp.MustCompile(parentPattern)
	matches := re.FindStringSubmatch(block)
	if len(matches) < 2 {
		return false
	}

	parentBlock := matches[1]

	// Now find the field
	fieldPattern := fmt.Sprintf(`%s\s*=\s*(true|false)`, regexp.QuoteMeta(field))
	fieldRe := regexp.MustCompile(fieldPattern)
	fieldMatches := fieldRe.FindStringSubmatch(parentBlock)
	if len(fieldMatches) >= 2 {
		return fieldMatches[1] == "true"
	}

	return false
}

// extractStringArray extracts a string array like: events = {"a", "b", "c"}
func extractStringArray(block, field string) []string {
	// Match: field = { ... }
	pattern := fmt.Sprintf(`%s\s*=\s*\{([^}]*)\}`, regexp.QuoteMeta(field))
	re := regexp.MustCompile(pattern)
	matches := re.FindStringSubmatch(block)
	if len(matches) < 2 {
		return nil
	}

	arrayContent := matches[1]

	// Extract all quoted strings
	stringMatches := reQuotedStrings.FindAllStringSubmatch(arrayContent, -1)

	var result []string
	for _, m := range stringMatches {
		if len(m) >= 2 {
			result = append(result, m[1])
		}
	}

	return result
}

// extractNetworkPerms extracts the network permissions table
// Format: network = { ["domain.com"] = {"GET", "POST"}, ... }
func extractNetworkPerms(block string) map[string][]string {
	result := make(map[string][]string)

	// Find the network block
	loc := reNetworkBlock.FindStringIndex(block)
	if loc == nil {
		return result
	}

	// Extract the network block content
	start := loc[1] - 1
	networkBlock := extractNestedBrace(block[start:])
	if networkBlock == "" {
		return result
	}

	// Match domain entries: ["domain.com"] = {"GET", "POST"} or domain = {"GET"}
	// Pattern for bracket notation: ["domain.com"] = {...}
	bracketMatches := reBracketDomain.FindAllStringSubmatch(networkBlock, -1)
	for _, m := range bracketMatches {
		if len(m) >= 3 {
			domain := m[1]
			methods := extractQuotedStrings(m[2])
			result[domain] = toUpperStrings(methods)
		}
	}

	// Pattern for simple notation: domain = {...}
	simpleMatches := reSimpleDomain.FindAllStringSubmatch(networkBlock, -1)
	for _, m := range simpleMatches {
		if len(m) >= 3 {
			domain := m[1]
			// Skip if it looks like a reserved word
			if domain == "network" || domain == "filesystem" || domain == "events" || domain == "schedules" {
				continue
			}
			methods := extractQuotedStrings(m[2])
			result[domain] = toUpperStrings(methods)
		}
	}

	return result
}

// extractSchedules extracts scheduled tasks
// Format: schedules = { {name = "task", interval = 3600}, ... }
func extractSchedules(block string) []Schedule {
	var result []Schedule

	// Find the schedules block
	loc := reSchedulesBlock.FindStringIndex(block)
	if loc == nil {
		return result
	}

	// Extract the schedules block content
	start := loc[1] - 1
	schedulesBlock := extractNestedBrace(block[start:])
	if schedulesBlock == "" {
		return result
	}

	// Find each schedule entry: {name = "...", interval = ...}
	// We need to find nested braces within the schedules array
	depth := 0
	entryStart := -1

	for i := 1; i < len(schedulesBlock)-1; i++ {
		c := schedulesBlock[i]
		if c == '{' {
			if depth == 0 {
				entryStart = i
			}
			depth++
		} else if c == '}' {
			depth--
			if depth == 0 && entryStart >= 0 {
				entry := schedulesBlock[entryStart : i+1]
				schedule := parseScheduleEntry(entry)
				if schedule.Name != "" && schedule.Interval > 0 {
					result = append(result, schedule)
				}
				entryStart = -1
			}
		}
	}

	return result
}

// parseScheduleEntry parses a single schedule entry like {name = "task", interval = 3600}
func parseScheduleEntry(entry string) Schedule {
	var schedule Schedule

	// Extract name
	nameMatches := reNameField.FindStringSubmatch(entry)
	if len(nameMatches) >= 2 {
		schedule.Name = nameMatches[1]
	}

	// Extract interval
	intervalMatches := reIntervalField.FindStringSubmatch(entry)
	if len(intervalMatches) >= 2 {
		schedule.Interval, _ = strconv.Atoi(intervalMatches[1])
	}

	return schedule
}

// extractSettings extracts settings declarations from the manifest
// Format: settings = { {key = "api_key", type = "password", label = "API Key"}, ... }
func extractSettings(block string) []SettingField {
	var result []SettingField

	// Find the settings block
	loc := reSettingsBlock.FindStringIndex(block)
	if loc == nil {
		return result
	}

	// Extract the settings block content
	start := loc[1] - 1
	settingsBlock := extractNestedBrace(block[start:])
	if settingsBlock == "" {
		return result
	}

	// Find each setting entry: {key = "...", type = "...", ...}
	depth := 0
	entryStart := -1

	for i := 1; i < len(settingsBlock)-1; i++ {
		c := settingsBlock[i]
		if c == '{' {
			if depth == 0 {
				entryStart = i
			}
			depth++
		} else if c == '}' {
			depth--
			if depth == 0 && entryStart >= 0 {
				entry := settingsBlock[entryStart : i+1]
				setting := parseSettingEntry(entry)
				if setting.Key != "" && setting.Type != "" && setting.Label != "" {
					// Validate type
					validTypes := map[string]bool{
						"text": true, "password": true, "checkbox": true, "select": true,
					}
					if validTypes[setting.Type] {
						// Validate select has options
						if setting.Type == "select" && len(setting.Options) == 0 {
							// Skip invalid select without options
							entryStart = -1
							continue
						}
						result = append(result, setting)
					}
				}
				entryStart = -1
			}
		}
	}

	return result
}

// parseSettingEntry parses a single setting entry
func parseSettingEntry(entry string) SettingField {
	var setting SettingField

	// Extract key
	setting.Key = extractStringField(entry, "key")

	// Extract type
	setting.Type = extractStringField(entry, "type")

	// Extract label
	setting.Label = extractStringField(entry, "label")

	// Extract description (optional)
	setting.Description = extractStringField(entry, "description")

	// Extract default (can be string, bool, or number)
	setting.Default = extractDefaultValue(entry)

	// Extract options for select type
	setting.Options = extractStringArray(entry, "options")

	return setting
}

// extractUI extracts UI declarations from the manifest
// Format: ui = { lightbox_buttons = {...}, card_actions = {...} }
func extractUI(block string) *UIManifest {
	// Find the ui block
	loc := reUIBlock.FindStringIndex(block)
	if loc == nil {
		return nil
	}

	start := loc[1] - 1
	uiBlock := extractNestedBrace(block[start:])
	if uiBlock == "" {
		return nil
	}

	ui := &UIManifest{}
	ui.LightboxButtons = extractUIActions(uiBlock, "lightbox_buttons")
	ui.CardActions = extractUIActions(uiBlock, "card_actions")

	// Return nil if no actions defined
	if len(ui.LightboxButtons) == 0 && len(ui.CardActions) == 0 {
		return nil
	}

	return ui
}

// extractUIActions extracts an array of UI actions
func extractUIActions(block, field string) []UIAction {
	var result []UIAction

	// Find the field block
	fieldPattern := regexp.MustCompile(regexp.QuoteMeta(field) + `\s*=\s*\{`)
	loc := fieldPattern.FindStringIndex(block)
	if loc == nil {
		return result
	}

	start := loc[1] - 1
	actionsBlock := extractNestedBrace(block[start:])
	if actionsBlock == "" {
		return result
	}

	// Find each action entry: {id = "...", label = "...", ...}
	depth := 0
	entryStart := -1

	for i := 1; i < len(actionsBlock)-1; i++ {
		c := actionsBlock[i]
		if c == '{' {
			if depth == 0 {
				entryStart = i
			}
			depth++
		} else if c == '}' {
			depth--
			if depth == 0 && entryStart >= 0 {
				entry := actionsBlock[entryStart : i+1]
				action := parseUIAction(entry)
				if action.ID != "" && action.Label != "" {
					result = append(result, action)
				}
				entryStart = -1
			}
		}
	}

	return result
}

// parseUIAction parses a single UI action entry
func parseUIAction(entry string) UIAction {
	var action UIAction

	action.ID = extractStringField(entry, "id")
	action.Label = extractStringField(entry, "label")
	action.Icon = extractStringField(entry, "icon")

	// Parse async flag
	asyncPattern := regexp.MustCompile(`async\s*=\s*(true|false)`)
	if m := asyncPattern.FindStringSubmatch(entry); len(m) >= 2 {
		action.Async = m[1] == "true"
	}

	// Parse options if present
	action.Options = extractFormFields(entry)

	return action
}

// extractFormFields extracts form field definitions from an action
func extractFormFields(block string) []FormField {
	var result []FormField

	// Find the options block
	loc := reOptionsBlock.FindStringIndex(block)
	if loc == nil {
		return result
	}

	start := loc[1] - 1
	optionsBlock := extractNestedBrace(block[start:])
	if optionsBlock == "" {
		return result
	}

	// Valid form field types
	validFormFieldTypes := map[string]bool{
		"text": true, "password": true, "checkbox": true, "select": true, "range": true,
	}

	// Find each field entry
	depth := 0
	entryStart := -1

	for i := 1; i < len(optionsBlock)-1; i++ {
		c := optionsBlock[i]
		if c == '{' {
			if depth == 0 {
				entryStart = i
			}
			depth++
		} else if c == '}' {
			depth--
			if depth == 0 && entryStart >= 0 {
				entry := optionsBlock[entryStart : i+1]
				field := parseFormField(entry)
				if field.ID != "" && field.Type != "" && field.Label != "" && validFormFieldTypes[field.Type] {
					// Validate select has choices
					if field.Type == "select" && len(field.Choices) == 0 {
						entryStart = -1
						continue
					}
					result = append(result, field)
				}
				entryStart = -1
			}
		}
	}

	return result
}

// parseFormField parses a single form field entry
func parseFormField(entry string) FormField {
	var field FormField

	field.ID = extractStringField(entry, "id")
	field.Type = extractStringField(entry, "type")
	field.Label = extractStringField(entry, "label")

	// Parse required
	if matches := reRequiredField.FindStringSubmatch(entry); len(matches) >= 2 {
		field.Required = matches[1] == "true"
	}

	// Parse default value
	field.Default = extractDefaultValue(entry)

	// Parse choices for select type
	field.Choices = extractChoices(entry)

	// Parse range options
	field.Min = extractFloatField(entry, "min")
	field.Max = extractFloatField(entry, "max")
	field.Step = extractFloatField(entry, "step")

	return field
}

// extractChoices extracts choices array for select fields
func extractChoices(block string) []Choice {
	var result []Choice

	// Find choices block
	loc := reChoicesBlock.FindStringIndex(block)
	if loc == nil {
		return result
	}

	start := loc[1] - 1
	choicesBlock := extractNestedBrace(block[start:])
	if choicesBlock == "" {
		return result
	}

	// Find each choice entry
	depth := 0
	entryStart := -1

	for i := 1; i < len(choicesBlock)-1; i++ {
		c := choicesBlock[i]
		if c == '{' {
			if depth == 0 {
				entryStart = i
			}
			depth++
		} else if c == '}' {
			depth--
			if depth == 0 && entryStart >= 0 {
				entry := choicesBlock[entryStart : i+1]
				value := extractStringField(entry, "value")
				label := extractStringField(entry, "label")
				if value != "" && label != "" {
					result = append(result, Choice{Value: value, Label: label})
				}
				entryStart = -1
			}
		}
	}

	return result
}

// extractFloatField extracts a float64 field value
func extractFloatField(block, field string) float64 {
	pattern := fmt.Sprintf(`%s\s*=\s*([0-9.]+)`, regexp.QuoteMeta(field))
	re := regexp.MustCompile(pattern)
	matches := re.FindStringSubmatch(block)
	if len(matches) >= 2 {
		val, err := strconv.ParseFloat(matches[1], 64)
		if err == nil {
			return val
		}
	}
	return 0
}

// extractDefaultValue extracts the default value which can be string, bool, or absent
func extractDefaultValue(entry string) any {
	// Try string first
	strDefault := extractStringField(entry, "default")
	if strDefault != "" {
		return strDefault
	}

	// Try boolean
	boolMatches := reDefaultBool.FindStringSubmatch(entry)
	if len(boolMatches) >= 2 {
		return boolMatches[1] == "true"
	}

	return nil
}

// extractNestedBrace extracts content within balanced braces starting at position 0
func extractNestedBrace(s string) string {
	if len(s) == 0 || s[0] != '{' {
		return ""
	}

	depth := 0
	inString := false
	stringChar := byte(0)

	for i := 0; i < len(s); i++ {
		c := s[i]

		if !inString {
			if c == '"' || c == '\'' {
				inString = true
				stringChar = c
			} else if c == '{' {
				depth++
			} else if c == '}' {
				depth--
				if depth == 0 {
					return s[:i+1]
				}
			}
		} else {
			if c == stringChar && (i == 0 || s[i-1] != '\\') {
				inString = false
			}
		}
	}

	return ""
}

// extractQuotedStrings extracts all quoted strings from a string
func extractQuotedStrings(s string) []string {
	matches := reQuotedStrings.FindAllStringSubmatch(s, -1)

	var result []string
	for _, m := range matches {
		if len(m) >= 2 {
			result = append(result, m[1])
		}
	}
	return result
}

// toUpperStrings converts a slice of strings to uppercase
func toUpperStrings(ss []string) []string {
	result := make([]string, len(ss))
	for i, s := range ss {
		result[i] = strings.ToUpper(s)
	}
	return result
}

// ValidEvents returns the list of valid event names
func ValidEvents() []string {
	return []string{
		"app:startup",
		"app:shutdown",
		"clip:created",
		"clip:deleted",
		"clip:archived",
		"clip:unarchived",
		"watch:file_detected",
		"watch:import_complete",
		"tag:created",
		"tag:updated",
		"tag:deleted",
		"tag:added_to_clip",
		"tag:removed_from_clip",
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
