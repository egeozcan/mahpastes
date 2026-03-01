package plugin

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"log"

	lua "github.com/yuin/gopher-lua"
)

const maxMetadataPairs = 50

type MetadataAPI struct {
	db *sql.DB
}

func NewMetadataAPI(db *sql.DB) *MetadataAPI {
	return &MetadataAPI{db: db}
}

func (m *MetadataAPI) Register(L *lua.LState) {
	mod := L.NewTable()
	mod.RawSetString("get", L.NewFunction(m.get))
	mod.RawSetString("set", L.NewFunction(m.set))
	mod.RawSetString("delete", L.NewFunction(m.del))
	mod.RawSetString("set_bulk", L.NewFunction(m.setBulk))
	L.SetGlobal("metadata", mod)
}

func (m *MetadataAPI) getMetadata(clipID int64) (map[string]string, error) {
	var raw string
	err := m.db.QueryRow("SELECT COALESCE(metadata, '{}') FROM clips WHERE id = ?", clipID).Scan(&raw)
	if err != nil {
		return nil, err
	}
	var meta map[string]string
	if err := json.Unmarshal([]byte(raw), &meta); err != nil {
		return map[string]string{}, nil
	}
	return meta, nil
}

func (m *MetadataAPI) saveMetadata(clipID int64, meta map[string]string) error {
	raw, err := json.Marshal(meta)
	if err != nil {
		return err
	}
	_, err = m.db.Exec("UPDATE clips SET metadata = ? WHERE id = ?", string(raw), clipID)
	return err
}

func (m *MetadataAPI) get(L *lua.LState) int {
	clipID := L.CheckInt64(1)
	meta, err := m.getMetadata(clipID)
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
	meta, err := m.getMetadata(clipID)
	if err != nil {
		L.Push(lua.LFalse)
		L.Push(lua.LString(err.Error()))
		return 2
	}
	if len(meta) >= maxMetadataPairs {
		if _, exists := meta[key]; !exists {
			L.Push(lua.LFalse)
			L.Push(lua.LString(fmt.Sprintf("metadata limit reached (max %d pairs)", maxMetadataPairs)))
			return 2
		}
	}
	meta[key] = value
	if err := m.saveMetadata(clipID, meta); err != nil {
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
	meta, err := m.getMetadata(clipID)
	if err != nil {
		L.Push(lua.LFalse)
		L.Push(lua.LString(err.Error()))
		return 2
	}
	delete(meta, key)
	if err := m.saveMetadata(clipID, meta); err != nil {
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
	meta := make(map[string]string)
	tbl.ForEach(func(k lua.LValue, v lua.LValue) {
		if ks, ok := k.(lua.LString); ok {
			meta[string(ks)] = v.String()
		}
	})
	if len(meta) > maxMetadataPairs {
		L.Push(lua.LFalse)
		L.Push(lua.LString(fmt.Sprintf("metadata limit exceeded (max %d pairs, got %d)", maxMetadataPairs, len(meta))))
		return 2
	}
	if err := m.saveMetadata(clipID, meta); err != nil {
		log.Printf("metadata.set_bulk: failed: %v", err)
		L.Push(lua.LFalse)
		L.Push(lua.LString(err.Error()))
		return 2
	}
	L.Push(lua.LTrue)
	return 1
}
