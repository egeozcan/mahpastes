package app

import (
	"fmt"
	"html"
	"strconv"

	"go-clipboard/internal/osnotify"
)

// SettingOSNotifications turns native OS notifications for background events
// on ("true") or off (anything else, the default). Machine-local: it is left
// out of backups and refused over REST, because switching it on has host
// side effects (a macOS permission prompt, HKCU registry keys on Windows).
const SettingOSNotifications = "os_notifications"

// isDesktopOnlySetting reports whether key may only be changed from the
// desktop app, never through the REST settings endpoint.
func isDesktopOnlySetting(key string) bool {
	return isAPILaunchSetting(key) || key == SettingOSNotifications
}

// OSNotificationsEnabled reports the persisted notification setting.
func (a *App) OSNotificationsEnabled() bool {
	if a.db == nil {
		return false
	}
	v, err := a.GetSetting(SettingOSNotifications)
	return err == nil && v == "true"
}

// SetOSNotificationsEnabled persists the notification setting.
func (a *App) SetOSNotificationsEnabled(enabled bool) error {
	if a.db == nil {
		return fmt.Errorf("no database")
	}
	return a.SetSetting(SettingOSNotifications, strconv.FormatBool(enabled))
}

// ClassifyBackgroundEvent maps a frontend event to an OS notification:
// imports (watch folders and REST uploads), watch import failures, clips
// received from a followed share, plugin task results and plugin toasts.
// Everything else returns ok=false. It runs on the emitting goroutine, so
// name lookups are deferred to Notice.Label.
func (a *App) ClassifyBackgroundEvent(name string, data ...any) (osnotify.Notice, bool) {
	var payload any
	if len(data) > 0 {
		payload = data[0]
	}
	switch name {
	case "watch:import":
		filename := ""
		switch c := payload.(type) {
		case ClipPreview:
			filename = c.Filename
		case *ClipPreview:
			if c != nil {
				filename = c.Filename
			}
		}
		return osnotify.Notice{Key: "import", Detail: filename, Format: formatImported}, true
	case "watch:error":
		m, _ := payload.(map[string]string)
		detail := m["file"]
		if m["error"] != "" {
			detail += ": " + m["error"]
		}
		return osnotify.Notice{Key: "import-error", Detail: detail, Format: formatImportFailed}, true
	case "share:clip-received":
		m, _ := payload.(map[string]any)
		tagID := anyInt64(m["local_tag_id"])
		return osnotify.Notice{
			Key:    "share:" + strconv.FormatInt(tagID, 10),
			Label:  func() string { return a.tagNameByID(tagID) },
			Format: formatShareReceived,
		}, true
	case "plugin:task:completed", "plugin:task:failed":
		m, _ := payload.(map[string]interface{})
		pluginID := anyInt64(m["plugin_id"])
		detail, _ := m["name"].(string)
		format := formatTaskDone
		key := "task-done:"
		if name == "plugin:task:failed" {
			if e, _ := m["error"].(string); e != "" {
				detail += ": " + e
			}
			format = formatTaskFailed
			key = "task-failed:"
		}
		return osnotify.Notice{
			Key:    key + strconv.FormatInt(pluginID, 10),
			Detail: detail,
			Label:  func() string { return a.pluginNameByID(pluginID) },
			Format: format,
		}, true
	case "plugin:toast":
		m, _ := payload.(map[string]string)
		pluginID, _ := strconv.ParseInt(m["plugin_id"], 10, 64)
		return osnotify.Notice{
			Key:    "toast:" + strconv.FormatInt(pluginID, 10),
			Detail: html.UnescapeString(m["message"]),
			Label:  func() string { return a.pluginNameByID(pluginID) },
			Format: formatPluginMessage,
		}, true
	}
	return osnotify.Notice{}, false
}

func formatImported(count int, _, detail string) (string, string) {
	if count == 1 {
		if detail == "" {
			return "Clip imported", "Imported a new clip"
		}
		return "Clip imported", "Imported " + detail
	}
	return "Clips imported", fmt.Sprintf("Imported %d clips", count)
}

func formatImportFailed(count int, _, detail string) (string, string) {
	if count == 1 {
		return "Import failed", "Could not import " + detail
	}
	return "Imports failed", fmt.Sprintf("%d files could not be imported. Latest: %s", count, detail)
}

func formatShareReceived(count int, label, _ string) (string, string) {
	title := "Shared clip received"
	if count > 1 {
		title = "Shared clips received"
	}
	clips := "1 new clip"
	if count > 1 {
		clips = fmt.Sprintf("%d new clips", count)
	}
	if label == "" {
		return title, clips + " from a followed share"
	}
	return title, clips + " in " + label
}

func pluginTitle(label string) string {
	if label == "" {
		return "Plugin"
	}
	return label
}

func formatTaskDone(count int, label, detail string) (string, string) {
	if count == 1 {
		return pluginTitle(label), "Finished: " + detail
	}
	return pluginTitle(label), fmt.Sprintf("%d tasks finished. Latest: %s", count, detail)
}

func formatTaskFailed(count int, label, detail string) (string, string) {
	if count == 1 {
		return pluginTitle(label), "Failed: " + detail
	}
	return pluginTitle(label), fmt.Sprintf("%d tasks failed. Latest: %s", count, detail)
}

func formatPluginMessage(count int, label, detail string) (string, string) {
	if count == 1 {
		return pluginTitle(label), detail
	}
	return pluginTitle(label), fmt.Sprintf("%d messages. Latest: %s", count, detail)
}

func (a *App) tagNameByID(id int64) string {
	if a.db == nil || id <= 0 {
		return ""
	}
	var name string
	if err := a.db.QueryRow("SELECT name FROM tags WHERE id = ?", id).Scan(&name); err != nil {
		return ""
	}
	return name
}

func (a *App) pluginNameByID(id int64) string {
	if a.db == nil || id <= 0 {
		return ""
	}
	var name string
	if err := a.db.QueryRow("SELECT name FROM plugins WHERE id = ?", id).Scan(&name); err != nil {
		return ""
	}
	return name
}

// anyInt64 reads an event payload number, whatever integer type the emitter
// used.
func anyInt64(v any) int64 {
	switch n := v.(type) {
	case int64:
		return n
	case int:
		return int64(n)
	case int32:
		return int64(n)
	case float64:
		return int64(n)
	case string:
		i, _ := strconv.ParseInt(n, 10, 64)
		return i
	}
	return 0
}
