package plugin

import (
	"database/sql"

	lua "github.com/yuin/gopher-lua"
)

// StorageAPI provides plugin-local key-value storage
type StorageAPI struct {
	db       *sql.DB
	pluginID int64
}

// NewStorageAPI creates a new storage API instance
func NewStorageAPI(db *sql.DB, pluginID int64) *StorageAPI {
	return &StorageAPI{
		db:       db,
		pluginID: pluginID,
	}
}

// Register adds the storage module to the Lua state
func (s *StorageAPI) Register(L *lua.LState) {
	storageMod := L.NewTable()

	storageMod.RawSetString("get", L.NewFunction(s.get))
	storageMod.RawSetString("set", L.NewFunction(s.set))
	storageMod.RawSetString("delete", L.NewFunction(s.delete))

	L.SetGlobal("storage", storageMod)
}

func (s *StorageAPI) get(L *lua.LState) int {
	key := L.CheckString(1)

	var value []byte
	err := s.db.QueryRow(
		"SELECT value FROM plugin_storage WHERE plugin_id = ? AND key = ?",
		s.pluginID, key,
	).Scan(&value)

	if err == sql.ErrNoRows {
		L.Push(lua.LNil)
		return 1
	}
	if err != nil {
		L.Push(lua.LNil)
		return 1
	}

	L.Push(lua.LString(string(value)))
	return 1
}

func (s *StorageAPI) set(L *lua.LState) int {
	key := L.CheckString(1)
	value := L.CheckString(2)

	_, err := s.db.Exec(`
		INSERT INTO plugin_storage (plugin_id, key, value)
		VALUES (?, ?, ?)
		ON CONFLICT(plugin_id, key) DO UPDATE SET value = excluded.value
	`, s.pluginID, key, []byte(value))

	if err != nil {
		L.Push(lua.LFalse)
		L.Push(lua.LString(err.Error()))
		return 2
	}

	L.Push(lua.LTrue)
	return 1
}

func (s *StorageAPI) delete(L *lua.LState) int {
	key := L.CheckString(1)

	_, err := s.db.Exec(
		"DELETE FROM plugin_storage WHERE plugin_id = ? AND key = ?",
		s.pluginID, key,
	)

	if err != nil {
		L.Push(lua.LFalse)
		return 1
	}

	L.Push(lua.LTrue)
	return 1
}
