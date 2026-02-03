package plugin

import (
	"fmt"
	"regexp"
	"strconv"
	"strings"
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

	return manifest, nil
}

// extractPluginTable finds and extracts the Plugin = { ... } block
func extractPluginTable(source string) (string, error) {
	// Match Plugin = { with possible whitespace variations
	re := regexp.MustCompile(`(?m)^Plugin\s*=\s*\{`)
	loc := re.FindStringIndex(source)
	if loc == nil {
		return "", fmt.Errorf("plugin must define a Plugin table")
	}

	// Find matching closing brace by counting braces
	start := loc[1] - 1 // Position of opening brace
	braceCount := 0
	inString := false
	stringChar := byte(0)
	escaped := false

	for i := start; i < len(source); i++ {
		c := source[i]

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
	stringRe := regexp.MustCompile(`["']([^"']+)["']`)
	stringMatches := stringRe.FindAllStringSubmatch(arrayContent, -1)

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
	networkPattern := regexp.MustCompile(`network\s*=\s*\{`)
	loc := networkPattern.FindStringIndex(block)
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
	bracketPattern := regexp.MustCompile(`\["([^"]+)"\]\s*=\s*\{([^}]*)\}`)
	bracketMatches := bracketPattern.FindAllStringSubmatch(networkBlock, -1)
	for _, m := range bracketMatches {
		if len(m) >= 3 {
			domain := m[1]
			methods := extractQuotedStrings(m[2])
			result[domain] = toUpperStrings(methods)
		}
	}

	// Pattern for simple notation: domain = {...}
	simplePattern := regexp.MustCompile(`(\w+)\s*=\s*\{([^}]*)\}`)
	simpleMatches := simplePattern.FindAllStringSubmatch(networkBlock, -1)
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
	schedulesPattern := regexp.MustCompile(`schedules\s*=\s*\{`)
	loc := schedulesPattern.FindStringIndex(block)
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
	namePattern := regexp.MustCompile(`name\s*=\s*["']([^"']+)["']`)
	nameMatches := namePattern.FindStringSubmatch(entry)
	if len(nameMatches) >= 2 {
		schedule.Name = nameMatches[1]
	}

	// Extract interval
	intervalPattern := regexp.MustCompile(`interval\s*=\s*(\d+)`)
	intervalMatches := intervalPattern.FindStringSubmatch(entry)
	if len(intervalMatches) >= 2 {
		schedule.Interval, _ = strconv.Atoi(intervalMatches[1])
	}

	return schedule
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
	re := regexp.MustCompile(`["']([^"']+)["']`)
	matches := re.FindAllStringSubmatch(s, -1)

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
