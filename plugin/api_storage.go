package plugin

import (
	"database/sql"
	"log"

	lua "github.com/yuin/gopher-lua"
)

// StorageAPI provides plugin-local key-value storage
type StorageAPI struct {
	db       *sql.DB
	pluginID int64
	// urlKeys holds the keys declared as url-typed settings in the manifest.
	// Lua may read these (they are the plugin's own configuration) but never
	// write them: a url setting's value drives the user's network grant, and
	// a value-derived rule would let the plugin self-grant arbitrary hosts.
	urlKeys map[string]bool
}

// NewStorageAPI creates a new storage API instance. urlKeys may be nil for
// manifests without url settings.
func NewStorageAPI(db *sql.DB, pluginID int64, urlKeys map[string]bool) *StorageAPI {
	return &StorageAPI{
		db:       db,
		pluginID: pluginID,
		urlKeys:  urlKeys,
	}
}

// Register adds the storage module to the Lua state
func (s *StorageAPI) Register(L *lua.LState) {
	storageMod := L.NewTable()

	storageMod.RawSetString("get", L.NewFunction(s.get))
	storageMod.RawSetString("set", L.NewFunction(s.set))
	storageMod.RawSetString("delete", L.NewFunction(s.delete))
	storageMod.RawSetString("list", L.NewFunction(s.list))

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
		log.Printf("storage.get: failed to query key: %v", err)
		L.Push(lua.LNil)
		return 1
	}

	L.Push(lua.LString(string(value)))
	return 1
}

func (s *StorageAPI) set(L *lua.LState) int {
	key := L.CheckString(1)
	value := L.CheckString(2)

	// url-typed settings are user-owned: their value records where the user
	// pointed the plugin, and only the user (via the plugin settings panel)
	// may change it — together with the network grant it carries.
	if s.urlKeys[key] {
		L.Push(lua.LFalse)
		L.Push(lua.LString("storage.set: key '" + key + "' is a url setting managed by the user and cannot be written by the plugin"))
		return 2
	}

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

func (s *StorageAPI) list(L *lua.LState) int {
	rows, err := s.db.Query(
		"SELECT key FROM plugin_storage WHERE plugin_id = ? ORDER BY key",
		s.pluginID,
	)
	if err != nil {
		L.Push(lua.LNil)
		L.Push(lua.LString(err.Error()))
		return 2
	}
	defer rows.Close()

	result := L.NewTable()
	for rows.Next() {
		var key string
		if err := rows.Scan(&key); err != nil {
			log.Printf("storage.list: failed to scan row: %v", err)
			continue
		}
		result.Append(lua.LString(key))
	}

	L.Push(result)
	return 1
}
