package webui

import "embed"

// Assets contains the production web UI and Wails-generated runtime bindings.
//
//go:embed all:js all:css all:dist all:vendor all:wailsjs all:*.html
var Assets embed.FS
