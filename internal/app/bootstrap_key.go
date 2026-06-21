package app

import (
	"log"
	"os"
	"path/filepath"
	"strconv"
)

// bootstrapKeyFile is the basename of the file the bootstrap admin key is
// written to (mode 0600) inside the data directory.
const bootstrapKeyFile = "bootstrap_admin_key"

// BootstrapAPIKey creates a first admin API key when no keys exist.
//
// The key is written to a 0600 file in the data directory and only its path is
// logged. Logging the secret itself would expose this durable, non-expiring
// admin credential to journald/Docker/log shippers — a wider audience than the
// data dir's filesystem permissions.
func BootstrapAPIKey(apiMgr *APIManager) {
	if apiMgr == nil {
		return
	}
	// Opt out with any falsey value (0/false/off-style) parsed via ParseBool,
	// not just the exact string "false". An unparseable value keeps the default
	// (enabled).
	if v, ok := os.LookupEnv("MAHPASTESD_BOOTSTRAP_KEY"); ok {
		if enabled, err := strconv.ParseBool(v); err == nil && !enabled {
			return
		}
	}

	keys, err := apiMgr.ListKeys()
	if err != nil {
		log.Printf("api: bootstrap check failed: %v", err)
		return
	}
	if len(keys) > 0 {
		return
	}
	result, err := apiMgr.CreateKey("bootstrap", "admin", 0)
	if err != nil {
		log.Printf("api: failed to bootstrap key: %v", err)
		return
	}

	log.Println("api: no API keys found - bootstrapping admin key")
	if path, werr := writeBootstrapKey(apiMgr.dataDir, result.Key); werr == nil {
		log.Printf("api: bootstrap admin key written to %s (mode 0600)", path)
		log.Println("api: read it once, then delete the file and rotate the key.")
		return
	} else {
		log.Printf("api: could not persist bootstrap key (%v); printing it once instead", werr)
	}
	// Fallback only when the file could not be written (e.g. no data dir).
	log.Printf("api: bootstrap admin key: %s", result.Key)
	log.Println("api: store this key securely. it will not be printed again.")
}

func writeBootstrapKey(dataDir, key string) (string, error) {
	if dataDir == "" {
		return "", os.ErrNotExist
	}
	path := filepath.Join(dataDir, bootstrapKeyFile)
	if err := os.WriteFile(path, []byte(key+"\n"), 0600); err != nil {
		return "", err
	}
	return path, nil
}
