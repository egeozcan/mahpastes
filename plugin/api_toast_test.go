package plugin

import (
	"testing"

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
