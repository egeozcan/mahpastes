package plugin

import (
	"encoding/base64"
	"encoding/json"
	"log"

	lua "github.com/yuin/gopher-lua"
)

// UtilsAPI provides utility functions to plugins
type UtilsAPI struct {
	pluginName string
}

// NewUtilsAPI creates a new utils API
func NewUtilsAPI(pluginName string) *UtilsAPI {
	return &UtilsAPI{pluginName: pluginName}
}

// Register adds utility functions to the Lua state
func (u *UtilsAPI) Register(L *lua.LState) {
	// log function
	L.SetGlobal("log", L.NewFunction(u.logFn))

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

// luaToGo converts a Lua value to a Go value
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
