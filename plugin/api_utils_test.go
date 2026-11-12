package plugin

import (
	"errors"
	"testing"

	lua "github.com/yuin/gopher-lua"
)

// runClipboardWrite registers a UtilsAPI whose clipboard writer is replaced
// by write (the real clipboard is never touched) and calls
// utils.clipboard_write("hello"), returning its two results.
func runClipboardWrite(t *testing.T, allowed bool, write func(string) error) (lua.LValue, lua.LValue) {
	t.Helper()
	L := lua.NewState()
	defer L.Close()
	u := NewUtilsAPI("test", allowed)
	u.writeClipboardText = write
	u.Register(L)
	if err := L.DoString(`ok, err = utils.clipboard_write("hello")`); err != nil {
		t.Fatalf("lua: %v", err)
	}
	return L.GetGlobal("ok"), L.GetGlobal("err")
}

func TestClipboardWriteReportsSuccess(t *testing.T) {
	var got string
	ok, errMsg := runClipboardWrite(t, true, func(s string) error { got = s; return nil })
	if ok != lua.LTrue || errMsg != lua.LNil {
		t.Fatalf("got (%v, %v), want (true, nil)", ok, errMsg)
	}
	if got != "hello" {
		t.Fatalf("wrote %q, want %q", got, "hello")
	}
}

func TestClipboardWriteReportsFailure(t *testing.T) {
	ok, errMsg := runClipboardWrite(t, true, func(string) error {
		return errors.New("failed to write to clipboard: no clipboard backend accepted the data")
	})
	if ok != lua.LNil {
		t.Fatalf("ok = %v, want nil when the write fails", ok)
	}
	if s, isStr := errMsg.(lua.LString); !isStr || string(s) != "failed to write to clipboard: no clipboard backend accepted the data" {
		t.Fatalf("err = %v, want the write error", errMsg)
	}
}

func TestClipboardWriteRequiresPermission(t *testing.T) {
	called := false
	ok, errMsg := runClipboardWrite(t, false, func(string) error { called = true; return nil })
	if called {
		t.Fatal("clipboard written without the clipboard permission")
	}
	if ok != lua.LNil || errMsg.Type() != lua.LTString {
		t.Fatalf("got (%v, %v), want (nil, message)", ok, errMsg)
	}
}
