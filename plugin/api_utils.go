package plugin

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"log"
	"net/url"
	"time"

	lua "github.com/yuin/gopher-lua"
	"golang.design/x/clipboard"
)

// UtilsAPI provides utility functions to plugins
type UtilsAPI struct {
	pluginName       string
	clipboardAllowed bool
}

// NewUtilsAPI creates a new utils API
func NewUtilsAPI(pluginName string, clipboardAllowed bool) *UtilsAPI {
	return &UtilsAPI{pluginName: pluginName, clipboardAllowed: clipboardAllowed}
}

// Register adds utility functions to the Lua state
func (u *UtilsAPI) Register(L *lua.LState) {
	// log function
	L.SetGlobal("log", L.NewFunction(u.logFn))

	// utils module (for time and other utilities)
	utilsMod := L.NewTable()
	utilsMod.RawSetString("time", L.NewFunction(u.timeFn))
	utilsMod.RawSetString("sha256", L.NewFunction(u.sha256Fn))
	utilsMod.RawSetString("hmac_sha256", L.NewFunction(u.hmacSha256Fn))
	utilsMod.RawSetString("url_encode", L.NewFunction(u.urlEncodeFn))
	utilsMod.RawSetString("url_decode", L.NewFunction(u.urlDecodeFn))
	utilsMod.RawSetString("clipboard_write", L.NewFunction(u.clipboardWriteFn))
	L.SetGlobal("utils", utilsMod)

	// json module
	jsonMod := L.NewTable()
	jsonMod.RawSetString("encode", L.NewFunction(u.jsonEncode))
	jsonMod.RawSetString("decode", L.NewFunction(u.jsonDecode))
	L.SetGlobal("json", jsonMod)

	// base64 module
	b64Mod := L.NewTable()
	b64Mod.RawSetString("encode", L.NewFunction(u.base64Encode))
	b64Mod.RawSetString("decode", L.NewFunction(u.base64Decode))
	L.SetGlobal("base64", b64Mod)
}

func (u *UtilsAPI) logFn(L *lua.LState) int {
	msg := L.CheckString(1)
	log.Printf("[plugin:%s] %s", u.pluginName, msg)
	return 0
}

// timeFn returns the current Unix timestamp (seconds since epoch)
func (u *UtilsAPI) timeFn(L *lua.LState) int {
	L.Push(lua.LNumber(time.Now().Unix()))
	return 1
}

func (u *UtilsAPI) jsonEncode(L *lua.LState) int {
	val := L.Get(1)
	goVal := luaToGo(val)

	data, err := json.Marshal(goVal)
	if err != nil {
		L.Push(lua.LNil)
		L.Push(lua.LString(err.Error()))
		return 2
	}

	L.Push(lua.LString(string(data)))
	return 1
}

func (u *UtilsAPI) jsonDecode(L *lua.LState) int {
	str := L.CheckString(1)

	var goVal interface{}
	if err := json.Unmarshal([]byte(str), &goVal); err != nil {
		L.Push(lua.LNil)
		L.Push(lua.LString(err.Error()))
		return 2
	}

	L.Push(goToLua(L, goVal))
	return 1
}

func (u *UtilsAPI) base64Encode(L *lua.LState) int {
	data := L.CheckString(1)
	encoded := base64.StdEncoding.EncodeToString([]byte(data))
	L.Push(lua.LString(encoded))
	return 1
}

func (u *UtilsAPI) base64Decode(L *lua.LState) int {
	encoded := L.CheckString(1)
	data, err := base64.StdEncoding.DecodeString(encoded)
	if err != nil {
		L.Push(lua.LNil)
		L.Push(lua.LString(err.Error()))
		return 2
	}
	L.Push(lua.LString(string(data)))
	return 1
}

// sha256Fn computes SHA-256 hash and returns hex string
func (u *UtilsAPI) sha256Fn(L *lua.LState) int {
	data := L.CheckString(1)
	hash := sha256.Sum256([]byte(data))
	L.Push(lua.LString(hex.EncodeToString(hash[:])))
	return 1
}

// hmacSha256Fn computes HMAC-SHA256 and returns hex string
func (u *UtilsAPI) hmacSha256Fn(L *lua.LState) int {
	key := L.CheckString(1)
	data := L.CheckString(2)
	mac := hmac.New(sha256.New, []byte(key))
	mac.Write([]byte(data))
	L.Push(lua.LString(hex.EncodeToString(mac.Sum(nil))))
	return 1
}

// urlEncodeFn URL-encodes a string using query escaping (spaces become "+", not "%20").
// This is correct for query parameters; for URL path segments use a different encoder.
func (u *UtilsAPI) urlEncodeFn(L *lua.LState) int {
	str := L.CheckString(1)
	L.Push(lua.LString(url.QueryEscape(str)))
	return 1
}

// urlDecodeFn URL-decodes a string
func (u *UtilsAPI) urlDecodeFn(L *lua.LState) int {
	str := L.CheckString(1)
	decoded, err := url.QueryUnescape(str)
	if err != nil {
		L.Push(lua.LNil)
		L.Push(lua.LString(err.Error()))
		return 2
	}
	L.Push(lua.LString(decoded))
	return 1
}

// clipboardWriteFn writes text to the system clipboard.
// Requires clipboard = true in the plugin manifest.
// Note: clipboard.Init() must be called by the host app before any plugin can use this.
func (u *UtilsAPI) clipboardWriteFn(L *lua.LState) int {
	if !u.clipboardAllowed {
		L.Push(lua.LNil)
		L.Push(lua.LString("clipboard access not permitted (add clipboard = true to Plugin manifest)"))
		return 2
	}
	text := L.CheckString(1)
	log.Printf("[plugin:%s] writing to clipboard", u.pluginName)
	// clipboard.Write returns a channel that signals when the clipboard content is
	// replaced by another process. We discard it because we only need fire-and-forget
	// write semantics here.
	clipboard.Write(clipboard.FmtText, []byte(text))
	L.Push(lua.LTrue)
	return 1
}

// luaToGo converts a Lua value to a Go value.
// For tables, it uses a heuristic to determine if the table is an array or map:
// - If all keys are consecutive integers starting from 1, it's treated as an array
// - Otherwise, it's treated as a map with string keys
// Note: Sparse arrays, 0-indexed tables, or mixed key types may produce unexpected results.
func luaToGo(val lua.LValue) interface{} {
	switch v := val.(type) {
	case lua.LBool:
		return bool(v)
	case lua.LNumber:
		return float64(v)
	case lua.LString:
		return string(v)
	case *lua.LTable:
		// Check if it's an array or map
		isArray := true
		maxIndex := 0
		v.ForEach(func(k, _ lua.LValue) {
			if num, ok := k.(lua.LNumber); ok {
				idx := int(num)
				if idx > maxIndex {
					maxIndex = idx
				}
			} else {
				isArray = false
			}
		})

		if isArray && maxIndex > 0 {
			arr := make([]interface{}, maxIndex)
			v.ForEach(func(k, val lua.LValue) {
				if num, ok := k.(lua.LNumber); ok {
					idx := int(num) - 1 // Lua is 1-indexed
					if idx >= 0 && idx < maxIndex {
						arr[idx] = luaToGo(val)
					}
				}
			})
			return arr
		}

		m := make(map[string]interface{})
		v.ForEach(func(k, val lua.LValue) {
			m[k.String()] = luaToGo(val)
		})
		return m
	case *lua.LNilType:
		return nil
	default:
		return nil
	}
}

// goToLua converts a Go value to a Lua value
func goToLua(L *lua.LState, val interface{}) lua.LValue {
	switch v := val.(type) {
	case nil:
		return lua.LNil
	case bool:
		return lua.LBool(v)
	case int:
		return lua.LNumber(v)
	case int64:
		return lua.LNumber(v)
	case float64:
		return lua.LNumber(v)
	case string:
		return lua.LString(v)
	case []interface{}:
		tbl := L.NewTable()
		for i, item := range v {
			tbl.RawSetInt(i+1, goToLua(L, item))
		}
		return tbl
	case map[string]interface{}:
		tbl := L.NewTable()
		for k, item := range v {
			tbl.RawSetString(k, goToLua(L, item))
		}
		return tbl
	default:
		return lua.LNil
	}
}
