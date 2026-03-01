package plugin

import (
	"fmt"
	"log"
	"unicode/utf8"

	lua "github.com/yuin/gopher-lua"
)

const maxMetadataPairs = 50

// MetadataGetFunc retrieves all metadata for a clip.
type MetadataGetFunc func(clipID int64) (map[string]string, error)

// MetadataUpdateFunc performs an atomic read-modify-write of clip metadata.
type MetadataUpdateFunc func(clipID int64, modify func(meta map[string]string) error) error

type MetadataAPI struct {
	getFn    MetadataGetFunc
	updateFn MetadataUpdateFunc
}

func NewMetadataAPI(getFn MetadataGetFunc, updateFn MetadataUpdateFunc) *MetadataAPI {
	return &MetadataAPI{getFn: getFn, updateFn: updateFn}
}

func (m *MetadataAPI) Register(L *lua.LState) {
	mod := L.NewTable()
	mod.RawSetString("get", L.NewFunction(m.get))
	mod.RawSetString("set", L.NewFunction(m.set))
	mod.RawSetString("delete", L.NewFunction(m.del))
	mod.RawSetString("set_bulk", L.NewFunction(m.setBulk))
	L.SetGlobal("metadata", mod)
}

func (m *MetadataAPI) get(L *lua.LState) int {
	clipID := L.CheckInt64(1)
	meta, err := m.getFn(clipID)
	if err != nil {
		L.Push(lua.LNil)
		L.Push(lua.LString(err.Error()))
		return 2
	}
	result := L.NewTable()
	for k, v := range meta {
		result.RawSetString(k, lua.LString(v))
	}
	L.Push(result)
	return 1
}

func (m *MetadataAPI) set(L *lua.LState) int {
	clipID := L.CheckInt64(1)
	key := L.CheckString(2)
	value := L.CheckString(3)
	if key == "" {
		L.Push(lua.LFalse)
		L.Push(lua.LString("metadata key cannot be empty"))
		return 2
	}
	if utf8.RuneCountInString(key) > 256 {
		L.Push(lua.LFalse)
		L.Push(lua.LString("metadata key too long (max 256 chars)"))
		return 2
	}
	if utf8.RuneCountInString(value) > 4096 {
		L.Push(lua.LFalse)
		L.Push(lua.LString("metadata value too long (max 4096 chars)"))
		return 2
	}
	err := m.updateFn(clipID, func(meta map[string]string) error {
		if len(meta) >= maxMetadataPairs {
			if _, exists := meta[key]; !exists {
				return fmt.Errorf("metadata limit reached (max %d pairs)", maxMetadataPairs)
			}
		}
		meta[key] = value
		return nil
	})
	if err != nil {
		L.Push(lua.LFalse)
		L.Push(lua.LString(err.Error()))
		return 2
	}
	L.Push(lua.LTrue)
	return 1
}

func (m *MetadataAPI) del(L *lua.LState) int {
	clipID := L.CheckInt64(1)
	key := L.CheckString(2)
	err := m.updateFn(clipID, func(meta map[string]string) error {
		delete(meta, key)
		return nil
	})
	if err != nil {
		L.Push(lua.LFalse)
		L.Push(lua.LString(err.Error()))
		return 2
	}
	L.Push(lua.LTrue)
	return 1
}

func (m *MetadataAPI) setBulk(L *lua.LState) int {
	clipID := L.CheckInt64(1)
	tbl := L.CheckTable(2)
	newMeta := make(map[string]string)
	var validationErr string
	tbl.ForEach(func(k lua.LValue, v lua.LValue) {
		if validationErr != "" {
			return
		}
		ks, ok := k.(lua.LString)
		if !ok {
			validationErr = "metadata keys must be strings"
			return
		}
		if string(ks) == "" {
			validationErr = "metadata key cannot be empty"
			return
		}
		if utf8.RuneCountInString(string(ks)) > 256 {
			validationErr = "metadata key too long (max 256 chars)"
			return
		}
		vs := v.String()
		if utf8.RuneCountInString(vs) > 4096 {
			validationErr = "metadata value too long (max 4096 chars)"
			return
		}
		newMeta[string(ks)] = vs
	})
	if validationErr != "" {
		L.Push(lua.LFalse)
		L.Push(lua.LString(validationErr))
		return 2
	}
	if len(newMeta) > maxMetadataPairs {
		L.Push(lua.LFalse)
		L.Push(lua.LString(fmt.Sprintf("metadata limit exceeded (max %d pairs, got %d)", maxMetadataPairs, len(newMeta))))
		return 2
	}
	err := m.updateFn(clipID, func(meta map[string]string) error {
		// Clear existing and replace with new
		for k := range meta {
			delete(meta, k)
		}
		for k, v := range newMeta {
			meta[k] = v
		}
		return nil
	})
	if err != nil {
		log.Printf("metadata.set_bulk: failed: %v", err)
		L.Push(lua.LFalse)
		L.Push(lua.LString(err.Error()))
		return 2
	}
	L.Push(lua.LTrue)
	return 1
}
