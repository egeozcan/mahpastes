package plugin

import (
	"strings"
	"testing"
	"unicode/utf8"

	lua "github.com/yuin/gopher-lua"
)

type toastSink struct {
	names    []string
	payloads []map[string]string
}

func (s *toastSink) Emit(name string, data ...any) {
	s.names = append(s.names, name)
	if len(data) > 0 {
		if m, ok := data[0].(map[string]string); ok {
			s.payloads = append(s.payloads, m)
		}
	}
}

func (s *toastSink) On(string, func(...any)) {}

// The toast payload names its plugin so a system notification for it can
// show the plugin's name.
func TestToastPayloadCarriesPluginID(t *testing.T) {
	sink := &toastSink{}
	L := lua.NewState()
	defer L.Close()
	NewToastAPI(sink, 42).Register(L)
	if err := L.DoString(`toast.show("Saved <b>3</b> clips", "success")`); err != nil {
		t.Fatal(err)
	}
	if len(sink.payloads) != 1 || sink.names[0] != "plugin:toast" {
		t.Fatalf("emitted %v", sink.names)
	}
	p := sink.payloads[0]
	if p["plugin_id"] != "42" || p["type"] != "success" || p["message"] != "Saved &lt;b&gt;3&lt;/b&gt; clips" {
		t.Fatalf("payload = %v", p)
	}
}

// The 200-byte cut used to slice bytes, splitting a multi-byte character and
// emitting invalid UTF-8, which macOS shows as a notification with no body.
func TestToastTruncationKeepsValidUTF8(t *testing.T) {
	for name, msg := range map[string]string{
		"two-byte":   strings.Repeat("\u00fc", 150),
		"three-byte": strings.Repeat("\u65e5", 100),
		"raw bytes":  strings.Repeat("a", 250) + "\xff",
		"short raw":  "abc\xc3",
	} {
		sink := &toastSink{}
		L := lua.NewState()
		NewToastAPI(sink, 1).Register(L)
		L.SetGlobal("msg", lua.LString(msg))
		if err := L.DoString(`toast.show(msg)`); err != nil {
			t.Fatal(err)
		}
		L.Close()
		got := sink.payloads[0]["message"]
		if !utf8.ValidString(got) {
			t.Errorf("%s: emitted invalid UTF-8 %q", name, got)
		}
		if len(got) > maxToastMessageLength {
			t.Errorf("%s: %d bytes, want at most %d", name, len(got), maxToastMessageLength)
		}
		if len(msg) > maxToastMessageLength && !strings.HasSuffix(got, "...") {
			t.Errorf("%s: %q does not end in an ellipsis", name, got)
		}
	}
	if got := truncateToastMessage(strings.Repeat("a", 250)); got != strings.Repeat("a", 197)+"..." {
		t.Errorf("ASCII truncation changed: %q", got)
	}
}
