package main

import (
	"encoding/base64"
	"encoding/json"
	"os"
	"strings"
	"testing"
	"unicode/utf8"

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
	// meta is what the plugin recorded, keyed by the clip it recorded it on, so
	// a test can tell metadata written to a clip that was later replaced from
	// metadata written to the one that survived.
	meta map[int64]map[string]string
	// metaErr makes metadata.set raise instead of returning a value.
	metaErr bool
	// resultType is the content type clips.get reports for clips the plugin
	// created, which decides whether ensure_jpeg converts them. Set it before
	// running an action; empty means JPEG, so conversion is skipped.
	resultType string
	createdIDs map[int64]bool
	nextClipID int64
}

// newClipID hands out a distinct id per created clip so a test can tell the
// converted clip apart from the one it replaced.
func (h *falHarness) newClipID() int64 {
	h.nextClipID++
	h.createdIDs[h.nextClipID] = true
	return h.nextClipID
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

	h := &falHarness{
		L:          L,
		request:    &falRequest{headers: map[string]string{}},
		clip:       &falClip{},
		meta:       map[int64]map[string]string{},
		createdIDs: map[int64]bool{},
		nextClipID: 41,
	}

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
		id := L.CheckInt64(1)
		contentType := "image/jpeg"
		if h.createdIDs[id] && h.resultType != "" {
			contentType = h.resultType
		}
		clip := L.NewTable()
		clip.RawSetString("id", lua.LNumber(id))
		clip.RawSetString("filename", lua.LString("photo.png"))
		// JPEG unless a test says otherwise, so ensure_jpeg leaves the saved
		// clip alone.
		clip.RawSetString("content_type", lua.LString(contentType))
		L.Push(clip)
		return 1
	}))
	clipsMod.RawSetString("create", L.NewFunction(func(L *lua.LState) int {
		opts := L.CheckTable(1)
		h.clip.via = "create"
		h.clip.filename = opts.RawGetString("filename").String()
		h.clip.contentType = opts.RawGetString("content_type").String()
		created := L.NewTable()
		created.RawSetString("id", lua.LNumber(h.newClipID()))
		L.Push(created)
		return 1
	}))
	clipsMod.RawSetString("create_from_url", L.NewFunction(func(L *lua.LState) int {
		h.clip.via = "create_from_url"
		h.clip.sourceURL = L.CheckString(1)
		h.clip.filename = L.CheckTable(2).RawGetString("name").String()
		created := L.NewTable()
		created.RawSetString("id", lua.LNumber(h.newClipID()))
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

	metadataMod := L.NewTable()
	metadataMod.RawSetString("set", L.NewFunction(func(L *lua.LState) int {
		if h.metaErr {
			L.RaiseError("metadata store unavailable")
		}
		clipID := L.CheckInt64(1)
		if h.meta[clipID] == nil {
			h.meta[clipID] = map[string]string{}
		}
		h.meta[clipID][L.CheckString(2)] = L.CheckString(3)
		L.Push(lua.LTrue)
		return 1
	}))
	L.SetGlobal("metadata", metadataMod)

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

// resultMeta returns the metadata of the single clip the action recorded on,
// failing if the writes were spread across clips — a run that leaves half its
// metadata on a clip it went on to delete is a bug, not a partial pass.
func (h *falHarness) resultMeta(t *testing.T) (int64, map[string]string) {
	t.Helper()
	if len(h.meta) != 1 {
		t.Fatalf("expected metadata on exactly one clip, got %d: %v", len(h.meta), h.meta)
	}
	for id, meta := range h.meta {
		return id, meta
	}
	return 0, nil
}

// wantMeta asserts the given keys were recorded on the result. Keys not listed
// are ignored, so a case only states what it cares about.
func wantMeta(t *testing.T, h *falHarness, want map[string]string) {
	t.Helper()
	_, meta := h.resultMeta(t)
	for key, value := range want {
		if got := meta[key]; got != value {
			t.Errorf("metadata %q: got %q, want %q", key, got, value)
		}
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

// Real inline results routinely exceed gopher-lua's pattern recursion limit.
// Their size must not affect whether the completed fal result can be stored.
func TestFalLargeInlineEditResultIsStored(t *testing.T) {
	response := `{"images":[{"url":"data:image/jpeg;base64,` + strings.Repeat("A", 1_100_000) + `"}]}`
	h := newFalHarness(t, response, map[string]string{"api_key": "k"})
	h.run(t, "edit", []int{7}, map[string]string{"prompt": "make it blue", "model": "nanobanana2"})

	if h.clip.via != "create" {
		t.Errorf("large inline result should be stored directly, got %q", h.clip.via)
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

// Results carry the parameters that produced them, so a clip can be traced back
// to its request once the task toast is gone.
func TestFalRecordsGenerationMetadata(t *testing.T) {
	t.Run("generate", func(t *testing.T) {
		h := newFalHarness(t, falSyncResponse, map[string]string{"api_key": "k", "safety_filter": "Strict"})
		h.run(t, "generate", nil, map[string]string{
			"prompt": "a cat", "model": "flux2turbo", "aspect_ratio": "16:9", "resolution": "2K"})

		wantMeta(t, h, map[string]string{
			"generator":     "fal.ai",
			"operation":     "generate",
			"model":         "fal-ai/flux-2/turbo",
			"prompt":        "a cat",
			"aspect_ratio":  "16:9",
			"image_size":    "2048x1152",
			"safety_filter": "strict",
		})
		// Nothing was derived from an existing clip.
		_, meta := h.resultMeta(t)
		if got, ok := meta["source_clips"]; ok {
			t.Errorf("generate should record no source clips, got %q", got)
		}
	})

	// Gemini models take a named resolution instead of explicit dimensions.
	t.Run("generate with a named resolution", func(t *testing.T) {
		h := newFalHarness(t, falSyncResponse, map[string]string{"api_key": "k"})
		h.run(t, "generate", nil, map[string]string{
			"prompt": "a cat", "model": "nanobananapro", "resolution": "4K"})

		wantMeta(t, h, map[string]string{
			"model":         "fal-ai/nano-banana-pro",
			"resolution":    "4K",
			"safety_filter": "off",
		})
		_, meta := h.resultMeta(t)
		if got, ok := meta["image_size"]; ok {
			t.Errorf("no explicit dimensions were sent, got image_size %q", got)
		}
	})

	t.Run("edit names its source", func(t *testing.T) {
		h := newFalHarness(t, falSyncResponse, map[string]string{"api_key": "k"})
		h.run(t, "edit", []int{7}, map[string]string{
			"prompt": "make it blue", "model": "nanobanana2", "safety": "moderate"})

		wantMeta(t, h, map[string]string{
			"operation":     "edit",
			"model":         "fal-ai/nano-banana-2/edit",
			"prompt":        "make it blue",
			"safety_filter": "moderate",
			"source_clips":  "7",
		})
	})

	t.Run("bulk edit names every source", func(t *testing.T) {
		h := newFalHarness(t, falSyncResponse, map[string]string{"api_key": "k"})
		h.run(t, "edit", []int{1, 2, 3}, map[string]string{"prompt": "merge", "model": "nanobanana2"})

		wantMeta(t, h, map[string]string{
			"operation":    "edit",
			"prompt":       "merge",
			"source_clips": "1, 2, 3",
		})
	})

	t.Run("upscale", func(t *testing.T) {
		h := newFalHarness(t, falCDNResponse, map[string]string{"api_key": "k"})
		h.run(t, "upscale", []int{7}, map[string]string{"model": "topaz", "scale": "4"})

		wantMeta(t, h, map[string]string{
			"operation":      "upscale",
			"model":          "fal-ai/topaz/upscale/image",
			"upscale_factor": "4",
			"source_clips":   "7",
		})
	})

	t.Run("restore", func(t *testing.T) {
		h := newFalHarness(t, falCDNResponse, map[string]string{"api_key": "k"})
		h.run(t, "restore", []int{7}, nil)

		wantMeta(t, h, map[string]string{
			"operation":        "restore",
			"model":            "fal-ai/image-apps-v2/photo-restoration",
			"fix_colors":       "true",
			"remove_scratches": "true",
		})
	})

	t.Run("colorize", func(t *testing.T) {
		h := newFalHarness(t, falCDNResponse, map[string]string{"api_key": "k"})
		h.run(t, "colorize", []int{7}, nil)

		wantMeta(t, h, map[string]string{
			"generator": "fal.ai",
			"operation": "colorize",
			"model":     "fal-ai/ddcolor",
		})
		// Clarity-style canned prompts aside, no action without a user prompt
		// should claim one.
		_, meta := h.resultMeta(t)
		if got, ok := meta["prompt"]; ok {
			t.Errorf("colorize takes no prompt, got %q", got)
		}
	})

	t.Run("vectorize", func(t *testing.T) {
		h := newFalHarness(t, falCDNResponse, map[string]string{"api_key": "k"})
		h.run(t, "vectorize", []int{7}, nil)

		wantMeta(t, h, map[string]string{
			"operation":    "vectorize",
			"model":        "fal-ai/recraft/vectorize",
			"source_clips": "7",
		})
	})

	// A multi-clip transform produces one result per input, and each has to name
	// the clip it actually came from.
	t.Run("each result names its own source", func(t *testing.T) {
		h := newFalHarness(t, falCDNResponse, map[string]string{"api_key": "k"})
		h.run(t, "colorize", []int{7, 8}, nil)

		if len(h.meta) != 2 {
			t.Fatalf("expected metadata on two result clips, got %d: %v", len(h.meta), h.meta)
		}
		sources := map[string]bool{}
		for _, meta := range h.meta {
			sources[meta["source_clips"]] = true
			if meta["operation"] != "colorize" {
				t.Errorf("operation: got %q, want %q", meta["operation"], "colorize")
			}
		}
		for _, want := range []string{"7", "8"} {
			if !sources[want] {
				t.Errorf("no result names clip %s as its source, got %v", want, sources)
			}
		}
	})
}

// The image is generated and saved before any metadata is written, so a
// metadata store that fails outright must not turn a paid result into a failed
// action.
func TestFalMetadataFailureDoesNotFailTheAction(t *testing.T) {
	h := newFalHarness(t, falSyncResponse, map[string]string{"api_key": "k"})
	h.metaErr = true

	// h.run fails the test if the action reports anything but success.
	h.run(t, "generate", nil, map[string]string{"prompt": "a cat"})

	if h.clip.via != "create" {
		t.Errorf("the result should still have been saved, got %q", h.clip.via)
	}
}

// A parameter the model never received must not be recorded as if it applied.
func TestFalMetadataRecordsOnlyWhatWasSent(t *testing.T) {
	t.Run("no filter parameter", func(t *testing.T) {
		// GPT Image 2 has no filter knob, so the setting never reached it.
		h := newFalHarness(t, falSyncResponse, map[string]string{"api_key": "k", "safety_filter": "Strict"})
		h.run(t, "generate", nil, map[string]string{"prompt": "x", "model": "gptimage2"})

		_, meta := h.resultMeta(t)
		if got, ok := meta["safety_filter"]; ok {
			t.Errorf("gpt-image-2 applied no filter, got safety_filter %q", got)
		}
	})

	t.Run("no scale parameter", func(t *testing.T) {
		// The creative upscaler picks its own factor and ignores the form's.
		h := newFalHarness(t, falCDNResponse, map[string]string{"api_key": "k"})
		h.run(t, "upscale", []int{7}, map[string]string{"model": "creative", "scale": "4"})

		_, meta := h.resultMeta(t)
		if got, ok := meta["upscale_factor"]; ok {
			t.Errorf("creative upscaler ignores the scale, got upscale_factor %q", got)
		}
		if meta["operation"] != "upscale" {
			t.Errorf("operation: got %q, want %q", meta["operation"], "upscale")
		}
	})
}

// ensure_jpeg replaces the saved clip with a converted one, so the metadata has
// to land on the survivor rather than the clip that was just deleted.
func TestFalMetadataFollowsTheConvertedClip(t *testing.T) {
	h := newFalHarness(t, falCDNResponse, map[string]string{"api_key": "k"})
	h.resultType = "image/png"
	h.run(t, "colorize", []int{7}, nil)

	if h.nextClipID < 43 {
		t.Fatalf("expected the result to be converted into a second clip, ids stopped at %d", h.nextClipID)
	}
	// resultMeta also fails if any key went to the clip that was replaced.
	id, _ := h.resultMeta(t)
	if id != h.nextClipID {
		t.Errorf("metadata went to clip %d, want the converted clip %d", id, h.nextClipID)
	}
}

// A prompt longer than the backend's per-value cap is trimmed here rather than
// being silently rejected on write. The cap counts characters, so a non-ASCII
// prompt must not be cut to a fraction of it.
func TestFalMetadataTrimsLongPrompts(t *testing.T) {
	cases := []struct {
		name string
		char string
	}{
		{"ascii", "a"},
		{"multi-byte", "猫"},
		{"four-byte", "🐈"},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			prompt := strings.Repeat(tc.char, 5000)
			h := newFalHarness(t, falSyncResponse, map[string]string{"api_key": "k"})
			h.run(t, "generate", nil, map[string]string{"prompt": prompt, "model": "nanobanana2"})

			_, meta := h.resultMeta(t)
			got := meta["prompt"]
			// The backend's own limit, counted the way the backend counts it.
			if n := utf8.RuneCountInString(got); n != 4096 {
				t.Errorf("recorded prompt: got %d characters, want 4096", n)
			}
			if !utf8.ValidString(got) {
				t.Error("trim split a character in half")
			}
			if !strings.HasPrefix(prompt, got) {
				t.Error("trim should keep the start of the prompt verbatim")
			}
			if h.request.payload["prompt"] != prompt {
				t.Error("the full prompt should still be sent to the model")
			}
		})
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
