package main

import (
	"encoding/base64"
	"encoding/json"
	"os"
	"strings"
	"testing"

	"go-clipboard/plugin"

	lua "github.com/yuin/gopher-lua"
)

// The fal.ai plugin talks to fal's REST API directly, so its request payloads
// can't be covered by the Playwright suite without spending real credits.
// These tests load plugins/fal-ai.lua into a Lua state with stubbed plugin APIs
// and assert on what it would have sent and stored.

const (
	falSyncResponse = `{"images":[{"url":"data:image/jpeg;base64,ZmFrZS1qcGVn"}]}`
	falCDNResponse  = `{"image":{"url":"https://v3b.fal.media/files/b/abc/out.png"}}`
)

type falRequest struct {
	url     string
	payload map[string]any
	headers map[string]string
}

type falClip struct {
	via         string
	filename    string
	contentType string
	sourceURL   string
}

type falHarness struct {
	L       *lua.LState
	request *falRequest
	clip    *falClip
}

// newFalHarness loads the plugin with every API it uses stubbed out. sourceSize
// is the reported dimensions of input clips; settings backs storage.get.
func newFalHarness(t *testing.T, response string, settings map[string]string) *falHarness {
	t.Helper()

	src, err := os.ReadFile("plugins/fal-ai.lua")
	if err != nil {
		t.Fatalf("read plugin: %v", err)
	}

	L := lua.NewState()
	t.Cleanup(L.Close)
	plugin.NewUtilsAPI("fal-test", false).Register(L)

	h := &falHarness{L: L, request: &falRequest{headers: map[string]string{}}, clip: &falClip{}}

	httpMod := L.NewTable()
	httpMod.RawSetString("post", L.NewFunction(func(L *lua.LState) int {
		h.request.url = L.CheckString(1)
		opts := L.CheckTable(2)
		if err := json.Unmarshal([]byte(opts.RawGetString("body").String()), &h.request.payload); err != nil {
			t.Errorf("request body is not valid JSON: %v", err)
		}
		if headers, ok := opts.RawGetString("headers").(*lua.LTable); ok {
			headers.ForEach(func(k, v lua.LValue) { h.request.headers[k.String()] = v.String() })
		}
		resp := L.NewTable()
		resp.RawSetString("status", lua.LNumber(200))
		resp.RawSetString("body", lua.LString(response))
		L.Push(resp)
		return 1
	}))
	L.SetGlobal("http", httpMod)

	clipsMod := L.NewTable()
	clipsMod.RawSetString("get_data", L.NewFunction(func(L *lua.LState) int {
		L.Push(lua.LString(base64.StdEncoding.EncodeToString([]byte("fake-png"))))
		L.Push(lua.LString("image/png"))
		return 2
	}))
	clipsMod.RawSetString("get", L.NewFunction(func(L *lua.LState) int {
		clip := L.NewTable()
		clip.RawSetString("id", lua.LNumber(L.CheckInt64(1)))
		clip.RawSetString("filename", lua.LString("photo.png"))
		// Already JPEG, so ensure_jpeg leaves the saved clip alone.
		clip.RawSetString("content_type", lua.LString("image/jpeg"))
		L.Push(clip)
		return 1
	}))
	clipsMod.RawSetString("create", L.NewFunction(func(L *lua.LState) int {
		opts := L.CheckTable(1)
		h.clip.via = "create"
		h.clip.filename = opts.RawGetString("filename").String()
		h.clip.contentType = opts.RawGetString("content_type").String()
		created := L.NewTable()
		created.RawSetString("id", lua.LNumber(42))
		L.Push(created)
		return 1
	}))
	clipsMod.RawSetString("create_from_url", L.NewFunction(func(L *lua.LState) int {
		h.clip.via = "create_from_url"
		h.clip.sourceURL = L.CheckString(1)
		h.clip.filename = L.CheckTable(2).RawGetString("name").String()
		created := L.NewTable()
		created.RawSetString("id", lua.LNumber(43))
		L.Push(created)
		return 1
	}))
	clipsMod.RawSetString("delete", L.NewFunction(func(L *lua.LState) int { L.Push(lua.LTrue); return 1 }))
	L.SetGlobal("clips", clipsMod)

	imageMod := L.NewTable()
	imageMod.RawSetString("info", L.NewFunction(func(L *lua.LState) int {
		info := L.NewTable()
		info.RawSetString("width", lua.LNumber(3000))
		info.RawSetString("height", lua.LNumber(1000))
		L.Push(info)
		return 1
	}))
	imageMod.RawSetString("convert", L.NewFunction(func(L *lua.LState) int {
		converted := L.NewTable()
		converted.RawSetString("data", lua.LString("Y29udmVydGVk"))
		L.Push(converted)
		return 1
	}))
	L.SetGlobal("image", imageMod)

	storageMod := L.NewTable()
	storageMod.RawSetString("get", L.NewFunction(func(L *lua.LState) int {
		if v, ok := settings[L.CheckString(1)]; ok {
			L.Push(lua.LString(v))
		} else {
			L.Push(lua.LNil)
		}
		return 1
	}))
	L.SetGlobal("storage", storageMod)

	taskMod := L.NewTable()
	for _, name := range []string{"start", "progress", "complete", "fail"} {
		taskMod.RawSetString(name, L.NewFunction(func(L *lua.LState) int { L.Push(lua.LNumber(1)); return 1 }))
	}
	L.SetGlobal("task", taskMod)

	noop := L.NewFunction(func(L *lua.LState) int { return 0 })
	toastMod := L.NewTable()
	toastMod.RawSetString("show", noop)
	L.SetGlobal("toast", toastMod)
	tagsMod := L.NewTable()
	tagsMod.RawSetString("add_to_clip", noop)
	L.SetGlobal("tags", tagsMod)
	L.SetGlobal("log", noop)

	if err := L.DoString(string(src)); err != nil {
		t.Fatalf("load plugin: %v", err)
	}
	return h
}

// run invokes on_ui_action and fails the test if the action reports an error.
func (h *falHarness) run(t *testing.T, action string, clipIDs []int, options map[string]string) {
	t.Helper()

	ids := h.L.NewTable()
	for _, id := range clipIDs {
		ids.Append(lua.LNumber(id))
	}
	opts := h.L.NewTable()
	for k, v := range options {
		opts.RawSetString(k, lua.LString(v))
	}

	err := h.L.CallByParam(lua.P{Fn: h.L.GetGlobal("on_ui_action"), NRet: 1, Protect: true},
		lua.LString(action), ids, opts, h.L.NewTable())
	if err != nil {
		t.Fatalf("on_ui_action(%q): %v", action, err)
	}
	result, ok := h.L.Get(-1).(*lua.LTable)
	h.L.Pop(1)
	if !ok {
		t.Fatalf("on_ui_action(%q) returned %v", action, h.L.Get(-1))
	}
	if result.RawGetString("success") != lua.LTrue {
		t.Fatalf("on_ui_action(%q) failed: %v", action, result.RawGetString("error"))
	}
}

func TestFalPluginManifestParses(t *testing.T) {
	src, err := os.ReadFile("plugins/fal-ai.lua")
	if err != nil {
		t.Fatal(err)
	}
	manifest, err := plugin.ParseManifest(string(src))
	if err != nil {
		t.Fatalf("parse manifest: %v", err)
	}

	settings := map[string]string{}
	for _, s := range manifest.Settings {
		settings[s.Key] = s.Type
	}
	for key, want := range map[string]string{
		"api_key":       "password",
		"cdn_retention": "select",
		"store_history": "checkbox",
		"safety_filter": "select",
	} {
		if settings[key] != want {
			t.Errorf("setting %q: got type %q, want %q", key, settings[key], want)
		}
	}
	if manifest.UI == nil || len(manifest.UI.GlobalActions) == 0 {
		t.Fatal("manifest lost its global generate action")
	}
}

// Every generate model must resolve to a real endpoint and produce a payload
// the model's schema accepts.
func TestFalGenerateEndpoints(t *testing.T) {
	cases := []struct {
		model    string
		endpoint string
		sync     bool
	}{
		{"nanobanana2", "https://fal.run/fal-ai/nano-banana-2", true},
		{"nanobananapro", "https://fal.run/fal-ai/nano-banana-pro", true},
		{"flux2max", "https://fal.run/fal-ai/flux-2-max", true},
		{"flux2pro", "https://fal.run/fal-ai/flux-2-pro", true},
		{"flux2turbo", "https://fal.run/fal-ai/flux-2/turbo", true},
		{"seedream5", "https://fal.run/bytedance/seedream/v5/pro/text-to-image", true},
		{"seedream45", "https://fal.run/fal-ai/bytedance/seedream/v4.5/text-to-image", true},
		{"gptimage2", "https://fal.run/openai/gpt-image-2", true},
		{"ideogram4", "https://fal.run/ideogram/v4", true},
		{"zimage", "https://fal.run/fal-ai/z-image/turbo", true},
		// Recraft has no sync_mode, so its result comes back over the CDN.
		{"recraft4", "https://fal.run/fal-ai/recraft/v4/text-to-image", false},
	}

	for _, tc := range cases {
		t.Run(tc.model, func(t *testing.T) {
			h := newFalHarness(t, falSyncResponse, map[string]string{"api_key": "k"})
			h.run(t, "generate", nil, map[string]string{
				"prompt": "a cat", "model": tc.model, "aspect_ratio": "16:9", "resolution": "2K"})

			if h.request.url != tc.endpoint {
				t.Errorf("endpoint: got %q, want %q", h.request.url, tc.endpoint)
			}
			if h.request.payload["prompt"] != "a cat" {
				t.Errorf("prompt not sent: %v", h.request.payload)
			}
			if got := h.request.payload["sync_mode"] == true; got != tc.sync {
				t.Errorf("sync_mode: got %v, want %v", got, tc.sync)
			}
		})
	}
}

func TestFalEditEndpoints(t *testing.T) {
	want := map[string]string{
		"nanobanana2":   "https://fal.run/fal-ai/nano-banana-2/edit",
		"nanobananapro": "https://fal.run/fal-ai/nano-banana-pro/edit",
		"flux2max":      "https://fal.run/fal-ai/flux-2-max/edit",
		"flux2pro":      "https://fal.run/fal-ai/flux-2-pro/edit",
		"flux2turbo":    "https://fal.run/fal-ai/flux-2/turbo/edit",
		"seedream5":     "https://fal.run/bytedance/seedream/v5/pro/edit",
		"seedream45":    "https://fal.run/fal-ai/bytedance/seedream/v4.5/edit",
		"gptimage2":     "https://fal.run/openai/gpt-image-2/edit",
	}

	for model, endpoint := range want {
		t.Run(model, func(t *testing.T) {
			h := newFalHarness(t, falSyncResponse, map[string]string{"api_key": "k"})
			h.run(t, "edit", []int{7}, map[string]string{"prompt": "make it blue", "model": model})

			if h.request.url != endpoint {
				t.Errorf("endpoint: got %q, want %q", h.request.url, endpoint)
			}
			// Every current edit endpoint takes a list, not a single image_url.
			urls, ok := h.request.payload["image_urls"].([]any)
			if !ok || len(urls) != 1 {
				t.Fatalf("image_urls: got %v", h.request.payload["image_urls"])
			}
			if !strings.HasPrefix(urls[0].(string), "data:image/png;base64,") {
				t.Errorf("input not inlined as a data URI: %v", urls[0])
			}
		})
	}
}

// A bulk edit is a single call carrying every selected image.
func TestFalBulkEditSendsOneRequest(t *testing.T) {
	h := newFalHarness(t, falSyncResponse, map[string]string{"api_key": "k"})
	h.run(t, "edit", []int{1, 2, 3}, map[string]string{"prompt": "merge", "model": "nanobanana2"})

	urls, ok := h.request.payload["image_urls"].([]any)
	if !ok || len(urls) != 3 {
		t.Fatalf("image_urls: got %v", h.request.payload["image_urls"])
	}
}

func TestFalSingleImageActions(t *testing.T) {
	cases := []struct {
		action, model, endpoint, filename string
	}{
		{"colorize", "", "https://fal.run/fal-ai/ddcolor", "photo_colorize.png"},
		{"restore", "", "https://fal.run/fal-ai/image-apps-v2/photo-restoration", "photo_restore.png"},
		{"vectorize", "", "https://fal.run/fal-ai/recraft/vectorize", "photo_vectorize.svg"},
		{"upscale", "clarity", "https://fal.run/fal-ai/clarity-upscaler", "photo_upscale.png"},
		{"upscale", "topaz", "https://fal.run/fal-ai/topaz/upscale/image", "photo_upscale.png"},
		{"upscale", "seedvr", "https://fal.run/fal-ai/seedvr/upscale/image", "photo_upscale.png"},
		{"upscale", "esrgan", "https://fal.run/fal-ai/esrgan", "photo_upscale.png"},
		{"upscale", "creative", "https://fal.run/fal-ai/creative-upscaler", "photo_upscale.png"},
	}

	for _, tc := range cases {
		t.Run(tc.action+"/"+tc.model, func(t *testing.T) {
			h := newFalHarness(t, falCDNResponse, map[string]string{"api_key": "k"})
			h.run(t, tc.action, []int{7}, map[string]string{"model": tc.model, "scale": "4"})

			if h.request.url != tc.endpoint {
				t.Errorf("endpoint: got %q, want %q", h.request.url, tc.endpoint)
			}
			if h.clip.via != "create_from_url" {
				t.Errorf("CDN result should be downloaded, got %q", h.clip.via)
			}
			// The extension follows the CDN URL, not the source clip.
			if h.clip.filename != tc.filename {
				t.Errorf("filename: got %q, want %q", h.clip.filename, tc.filename)
			}
		})
	}
}

// sync_mode results arrive inline, so they are decoded straight into a clip and
// never fetched back from fal's CDN.
func TestFalInlineResultsSkipTheCDN(t *testing.T) {
	h := newFalHarness(t, falSyncResponse, map[string]string{"api_key": "k"})
	h.run(t, "generate", nil, map[string]string{"prompt": "a cat"})

	if h.clip.via != "create" {
		t.Errorf("inline result should be stored directly, got %q", h.clip.via)
	}
	if h.clip.contentType != "image/jpeg" {
		t.Errorf("content type: got %q, want image/jpeg", h.clip.contentType)
	}
	if !strings.HasSuffix(h.clip.filename, ".jpg") {
		t.Errorf("filename should match the inline format, got %q", h.clip.filename)
	}
}

// The retention headers are the only thing standing between fal's default
// (keep generated media forever) and the results we already downloaded.
func TestFalRetentionHeaders(t *testing.T) {
	t.Run("defaults", func(t *testing.T) {
		h := newFalHarness(t, falSyncResponse, map[string]string{"api_key": "k"})
		h.run(t, "generate", nil, map[string]string{"prompt": "a cat"})

		if got := h.request.headers["X-Fal-Object-Lifecycle-Preference"]; got != `{"expiration_duration_seconds":300}` {
			t.Errorf("lifecycle header: got %q", got)
		}
		if got := h.request.headers["X-Fal-Store-IO"]; got != "0" {
			t.Errorf("payload storage should be off by default, got %q", got)
		}
	})

	t.Run("longer retention", func(t *testing.T) {
		h := newFalHarness(t, falSyncResponse, map[string]string{"api_key": "k", "cdn_retention": "24 hours"})
		h.run(t, "generate", nil, map[string]string{"prompt": "a cat"})

		if got := h.request.headers["X-Fal-Object-Lifecycle-Preference"]; got != `{"expiration_duration_seconds":86400}` {
			t.Errorf("lifecycle header: got %q", got)
		}
	})

	t.Run("opted out", func(t *testing.T) {
		h := newFalHarness(t, falSyncResponse, map[string]string{
			"api_key": "k", "cdn_retention": "fal account default", "store_history": "true"})
		h.run(t, "generate", nil, map[string]string{"prompt": "a cat"})

		if _, ok := h.request.headers["X-Fal-Object-Lifecycle-Preference"]; ok {
			t.Error("account default should send no lifecycle header")
		}
		if _, ok := h.request.headers["X-Fal-Store-IO"]; ok {
			t.Error("opting into history should send no store-io header")
		}
	})
}

// Models state their accepted dimensions differently; the plugin has to land
// inside each range without silently reshaping the image.
func TestFalImageSizeFitsModelLimits(t *testing.T) {
	size := func(t *testing.T, payload map[string]any) (float64, float64) {
		t.Helper()
		dims, ok := payload["image_size"].(map[string]any)
		if !ok {
			t.Fatalf("image_size: got %v", payload["image_size"])
		}
		return dims["width"].(float64), dims["height"].(float64)
	}

	t.Run("flux caps at 2048", func(t *testing.T) {
		h := newFalHarness(t, falSyncResponse, map[string]string{"api_key": "k"})
		h.run(t, "generate", nil, map[string]string{
			"prompt": "x", "model": "flux2turbo", "aspect_ratio": "16:9", "resolution": "4K"})

		w, hh := size(t, h.request.payload)
		if w != 2048 || hh != 1152 {
			t.Errorf("got %vx%v, want 2048x1152", w, hh)
		}
	})

	t.Run("seedream 4.5 reaches its floor", func(t *testing.T) {
		h := newFalHarness(t, falSyncResponse, map[string]string{"api_key": "k"})
		h.run(t, "generate", nil, map[string]string{
			"prompt": "x", "model": "seedream45", "aspect_ratio": "1:1", "resolution": "1K"})

		w, hh := size(t, h.request.payload)
		if w < 1920 || hh < 1920 || w > 4096 || hh > 4096 {
			t.Errorf("got %vx%v, want both sides within 1920-4096", w, hh)
		}
	})

	t.Run("extreme source keeps its aspect", func(t *testing.T) {
		// A 3000x1000 source can't satisfy Seedream 4.5's 1920 short-side
		// floor without distorting, so the long side fills instead.
		h := newFalHarness(t, falSyncResponse, map[string]string{"api_key": "k"})
		h.run(t, "edit", []int{7}, map[string]string{"prompt": "x", "model": "seedream45"})

		w, hh := size(t, h.request.payload)
		if w != 4096 {
			t.Errorf("width: got %v, want the 4096 ceiling", w)
		}
		if ratio := w / hh; ratio < 2.9 || ratio > 3.1 {
			t.Errorf("aspect ratio drifted: got %vx%v (%.2f), want ~3.0", w, hh, ratio)
		}
	})
}

// One user-facing content filter level has to reach three incompatible model
// knobs, inverted, without the numbers leaking into the UI.
func TestFalSafetyFilterMapsPerModel(t *testing.T) {
	cases := []struct {
		level        string
		geminiTol    string
		fluxTol      string
		checkerOn    bool
		defaultLevel bool
	}{
		{level: "", geminiTol: "6", fluxTol: "5", checkerOn: false, defaultLevel: true},
		{level: "Off (most permissive)", geminiTol: "6", fluxTol: "5", checkerOn: false},
		{level: "Relaxed", geminiTol: "5", fluxTol: "4", checkerOn: false},
		{level: "Moderate", geminiTol: "3", fluxTol: "3", checkerOn: true},
		{level: "Strict", geminiTol: "1", fluxTol: "1", checkerOn: true},
		// Anything unrecognised must not silently disable filtering.
		{level: "bogus", geminiTol: "6", fluxTol: "5", checkerOn: false},
	}

	for _, tc := range cases {
		name := tc.level
		if tc.defaultLevel {
			name = "unset"
		}
		t.Run(name, func(t *testing.T) {
			settings := map[string]string{"api_key": "k"}
			if tc.level != "" {
				settings["safety_filter"] = tc.level
			}

			// Gemini grades tolerance 1-6.
			h := newFalHarness(t, falSyncResponse, settings)
			h.run(t, "generate", nil, map[string]string{"prompt": "x", "model": "nanobanana2"})
			if got := h.request.payload["safety_tolerance"]; got != tc.geminiTol {
				t.Errorf("gemini safety_tolerance: got %v, want %q", got, tc.geminiTol)
			}

			// FLUX [pro]/[max] grade it 1-5 and also carry the on/off checker.
			h = newFalHarness(t, falSyncResponse, settings)
			h.run(t, "generate", nil, map[string]string{"prompt": "x", "model": "flux2pro"})
			if got := h.request.payload["safety_tolerance"]; got != tc.fluxTol {
				t.Errorf("flux safety_tolerance: got %v, want %q", got, tc.fluxTol)
			}
			if got := h.request.payload["enable_safety_checker"]; got != tc.checkerOn {
				t.Errorf("flux enable_safety_checker: got %v, want %v", got, tc.checkerOn)
			}

			// Everything else has only the checker.
			h = newFalHarness(t, falSyncResponse, settings)
			h.run(t, "generate", nil, map[string]string{"prompt": "x", "model": "seedream45"})
			if got := h.request.payload["enable_safety_checker"]; got != tc.checkerOn {
				t.Errorf("seedream45 enable_safety_checker: got %v, want %v", got, tc.checkerOn)
			}

			// The setting applies to edits and upscales too, not just generate.
			h = newFalHarness(t, falSyncResponse, settings)
			h.run(t, "edit", []int{7}, map[string]string{"prompt": "x", "model": "nanobanana2"})
			if got := h.request.payload["safety_tolerance"]; got != tc.geminiTol {
				t.Errorf("edit safety_tolerance: got %v, want %q", got, tc.geminiTol)
			}

			h = newFalHarness(t, falCDNResponse, settings)
			h.run(t, "upscale", []int{7}, map[string]string{"model": "clarity"})
			if got := h.request.payload["enable_safety_checker"]; got != tc.checkerOn {
				t.Errorf("clarity enable_safety_checker: got %v, want %v", got, tc.checkerOn)
			}
		})
	}
}

// GPT Image 2 has no filter parameter, so the setting must not invent one.
func TestFalSafetyFilterSkipsModelsWithoutOne(t *testing.T) {
	h := newFalHarness(t, falSyncResponse, map[string]string{"api_key": "k", "safety_filter": "Strict"})
	h.run(t, "generate", nil, map[string]string{"prompt": "x", "model": "gptimage2"})

	for _, key := range []string{"safety_tolerance", "enable_safety_checker"} {
		if _, ok := h.request.payload[key]; ok {
			t.Errorf("gpt-image-2 payload should omit %q, got %v", key, h.request.payload[key])
		}
	}
}

// The Content Filter dropdown on the Generate and AI Edit forms overrides the
// plugin-wide default for that one run.
func TestFalPerActionSafetyOverride(t *testing.T) {
	// Plugin default is the loosest; the form asks for the strictest.
	settings := map[string]string{"api_key": "k", "safety_filter": "Off (most permissive)"}

	t.Run("generate", func(t *testing.T) {
		h := newFalHarness(t, falSyncResponse, settings)
		h.run(t, "generate", nil, map[string]string{
			"prompt": "x", "model": "nanobanana2", "safety": "strict"})

		if got := h.request.payload["safety_tolerance"]; got != "1" {
			t.Errorf("safety_tolerance: got %v, want %q", got, "1")
		}
	})

	t.Run("edit", func(t *testing.T) {
		h := newFalHarness(t, falSyncResponse, settings)
		h.run(t, "edit", []int{7}, map[string]string{
			"prompt": "x", "model": "flux2pro", "safety": "moderate"})

		if got := h.request.payload["safety_tolerance"]; got != "3" {
			t.Errorf("safety_tolerance: got %v, want %q", got, "3")
		}
		if got := h.request.payload["enable_safety_checker"]; got != true {
			t.Errorf("enable_safety_checker: got %v, want true", got)
		}
	})

	t.Run("bulk edit", func(t *testing.T) {
		h := newFalHarness(t, falSyncResponse, settings)
		h.run(t, "edit", []int{1, 2}, map[string]string{
			"prompt": "x", "model": "nanobanana2", "safety": "relaxed"})

		if got := h.request.payload["safety_tolerance"]; got != "5" {
			t.Errorf("safety_tolerance: got %v, want %q", got, "5")
		}
	})

	// "default" and unrecognised values defer to the plugin setting.
	for _, choice := range []string{"default", "", "bogus"} {
		t.Run("defers/"+choice, func(t *testing.T) {
			h := newFalHarness(t, falSyncResponse, map[string]string{
				"api_key": "k", "safety_filter": "Strict"})
			h.run(t, "generate", nil, map[string]string{
				"prompt": "x", "model": "nanobanana2", "safety": choice})

			if got := h.request.payload["safety_tolerance"]; got != "1" {
				t.Errorf("safety_tolerance: got %v, want the setting's %q", got, "1")
			}
		})
	}
}

// The Upscale form has no filter dropdown, so Clarity follows the setting.
func TestFalUpscaleFollowsSafetySetting(t *testing.T) {
	h := newFalHarness(t, falCDNResponse, map[string]string{"api_key": "k", "safety_filter": "Strict"})
	h.run(t, "upscale", []int{7}, map[string]string{"model": "clarity"})

	if got := h.request.payload["enable_safety_checker"]; got != true {
		t.Errorf("enable_safety_checker: got %v, want true", got)
	}
}

// Every action form that takes a prompt must expose the filter, or the setting
// is the only way to reach it.
func TestFalPromptFormsExposeSafetyDropdown(t *testing.T) {
	src, err := os.ReadFile("plugins/fal-ai.lua")
	if err != nil {
		t.Fatal(err)
	}
	manifest, err := plugin.ParseManifest(string(src))
	if err != nil {
		t.Fatalf("parse manifest: %v", err)
	}

	groups := map[string][]plugin.UIAction{
		"lightbox": manifest.UI.LightboxButtons,
		"bulk":     manifest.UI.BulkActions,
		"global":   manifest.UI.GlobalActions,
	}
	found := 0
	for group, actions := range groups {
		for _, action := range actions {
			takesPrompt := false
			hasSafety := false
			for _, opt := range action.Options {
				if opt.ID == "prompt" {
					takesPrompt = true
				}
				if opt.ID == "safety" {
					hasSafety = true
					if len(opt.Choices) != 5 {
						t.Errorf("%s/%s: safety has %d choices, want 5", group, action.ID, len(opt.Choices))
					}
					if opt.Default != "default" {
						t.Errorf("%s/%s: safety defaults to %v, want %q", group, action.ID, opt.Default, "default")
					}
				}
			}
			if takesPrompt && !hasSafety {
				t.Errorf("%s/%s takes a prompt but exposes no Content Filter", group, action.ID)
			}
			if hasSafety {
				found++
			}
		}
	}
	if found != 3 {
		t.Errorf("expected the filter on 3 action forms, found %d", found)
	}
}

func TestFalRequiresAPIKey(t *testing.T) {
	h := newFalHarness(t, falSyncResponse, nil)
	err := h.L.CallByParam(lua.P{Fn: h.L.GetGlobal("on_ui_action"), NRet: 1, Protect: true},
		lua.LString("generate"), h.L.NewTable(), h.L.NewTable(), h.L.NewTable())
	if err != nil {
		t.Fatalf("on_ui_action: %v", err)
	}
	result := h.L.Get(-1).(*lua.LTable)
	if result.RawGetString("success") != lua.LFalse {
		t.Error("expected failure without an API key")
	}
	if h.request.url != "" {
		t.Errorf("no request should be sent without a key, got %q", h.request.url)
	}
}
