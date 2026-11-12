package main

import (
	"testing"

	"github.com/wailsapp/wails/v2/pkg/menu"
)

func TestAppMenuStructureAndDispatch(t *testing.T) {
	var got []string
	m := &menuEmitter{}
	built := buildAppMenu(m)

	titles := map[string]*menu.MenuItem{}
	for _, it := range built.Items {
		titles[it.Label] = it
	}
	for _, want := range []string{"File", "View", "Help"} {
		if titles[want] == nil || titles[want].SubMenu == nil {
			t.Fatalf("menu %q missing", want)
		}
	}

	var settings *menu.MenuItem
	for _, it := range titles["File"].SubMenu.Items {
		if it.Label == "Settings…" {
			settings = it
		}
	}
	if settings == nil || settings.Accelerator == nil {
		t.Fatal("File > Settings… with an accelerator is missing")
	}
	if settings.Accelerator.Key != "," {
		t.Fatalf("Settings accelerator key = %q, want ,", settings.Accelerator.Key)
	}

	// Before the bridge exists a click is a no-op rather than a panic.
	settings.Click(&menu.CallbackData{MenuItem: settings})

	done := make(chan struct{})
	m.set(func(name string, data ...any) {
		got = append(got, name)
		if len(data) == 1 {
			got = append(got, data[0].(string))
		}
		close(done)
	})
	settings.Click(&menu.CallbackData{MenuItem: settings})
	<-done
	if len(got) != 2 || got[0] != menuActionEvent || got[1] != "settings" {
		t.Fatalf("emitted %v, want [%s settings]", got, menuActionEvent)
	}
}
