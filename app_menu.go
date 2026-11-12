package main

import (
	"sync"

	"github.com/wailsapp/wails/v2/pkg/menu"
	"github.com/wailsapp/wails/v2/pkg/menu/keys"
)

// menuActionEvent carries the id of a clicked application-menu item to the
// frontend (js/menu-actions.js), which maps it onto the matching keyboard
// shortcut action and runs it only if that action is available right now.
const menuActionEvent = "menu:action"

// menuEmitter forwards a menu click to the frontend. The Wails bridge only
// exists after OnStartup, so the menu closes over this holder; clicks before
// then do nothing.
type menuEmitter struct {
	mu   sync.Mutex
	emit func(name string, data ...any)
}

func (m *menuEmitter) set(emit func(name string, data ...any)) {
	m.mu.Lock()
	m.emit = emit
	m.mu.Unlock()
}

func (m *menuEmitter) send(id string) {
	m.mu.Lock()
	emit := m.emit
	m.mu.Unlock()
	if emit != nil {
		// Off the UI thread: Emit must never block a native menu callback.
		go emit(menuActionEvent, id)
	}
}

// buildAppMenu builds the macOS menu bar: the standard application, Edit and
// Window menus Wails would install by default, plus File (import, Settings),
// View (the app's views) and Help. Accelerators are limited to Cmd
// combinations that no in-app shortcut uses, because native accelerators win
// over the page's key handling.
func buildAppMenu(m *menuEmitter) *menu.Menu {
	item := func(label, id string, accel *keys.Accelerator) *menu.MenuItem {
		return menu.Text(label, accel, func(*menu.CallbackData) { m.send(id) })
	}

	file := menu.NewMenuFromItems(
		item("Import Files…", "import-files", keys.CmdOrCtrl("o")),
		item("Import Folder…", "import-folder", keys.Combo("o", keys.CmdOrCtrlKey, keys.ShiftKey)),
		menu.Separator(),
		item("Settings…", "settings", keys.CmdOrCtrl(",")),
	)
	view := menu.NewMenuFromItems(
		item("Archive", "archive", nil),
		item("Folder Mode", "folder-mode", nil),
		menu.Separator(),
		item("Watch Folders", "watch", nil),
		item("Serve", "serve", nil),
		item("Plugins", "plugins", nil),
	)
	help := menu.NewMenuFromItems(
		item("Keyboard Shortcuts", "shortcuts", nil),
		item("mahpastes Documentation", "documentation", nil),
	)

	return menu.NewMenuFromItems(
		menu.AppMenu(),
		menu.SubMenu("File", file),
		menu.EditMenu(),
		menu.SubMenu("View", view),
		menu.WindowMenu(),
		menu.SubMenu("Help", help),
	)
}
