//go:build linux

package main

import (
	"reflect"
	"testing"
)

func TestOpenWithCommands(t *testing.T) {
	got := openWithCommands("/tmp/a b.png", "/usr/share/applications/org.kde.gwenview.desktop")
	want := [][]string{
		{"gio", "launch", "/usr/share/applications/org.kde.gwenview.desktop", "/tmp/a b.png"},
		{"gtk-launch", "org.kde.gwenview", "/tmp/a b.png"},
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("got %v want %v", got, want)
	}
	got = openWithCommands("/tmp/x", "/usr/bin/gimp")
	if !reflect.DeepEqual(got, [][]string{{"/usr/bin/gimp", "/tmp/x"}}) {
		t.Fatalf("plain exec: %v", got)
	}
}

func TestDefaultOpenAndRevealCommands(t *testing.T) {
	if c := defaultOpenCommands("/tmp/x"); c[0][0] != "xdg-open" || c[1][0] != "gio" {
		t.Fatalf("unexpected %v", c)
	}
	c := revealCommands("/tmp/my dir/a#b.txt")
	if c[0][len(c[0])-2] != "array:string:file:///tmp/my%20dir/a%23b.txt" {
		t.Fatalf("bad uri: %v", c[0])
	}
	if !reflect.DeepEqual(c[1], []string{"xdg-open", "/tmp/my dir"}) {
		t.Fatalf("bad fallback: %v", c[1])
	}
}

func TestStartFirstAvailableMissing(t *testing.T) {
	if err := startFirstAvailable([][]string{{"definitely-not-a-binary-xyz", "a"}}); err == nil {
		t.Fatal("expected error")
	}
}
