package main

import (
	"testing"
)

func TestIsServing_FalseWhenIdle(t *testing.T) {
	app, cleanup := setupTestApp(t)
	defer cleanup()

	if app.serveManager.IsServing(12345) {
		t.Fatal("IsServing must return false when nothing is active")
	}
}
