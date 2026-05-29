package app

import (
	"log"
	"os"
)

// BootstrapAPIKey creates a first admin API key when no keys exist.
func BootstrapAPIKey(apiMgr *APIManager) {
	if os.Getenv("MAHPASTESD_BOOTSTRAP_KEY") == "false" || apiMgr == nil {
		return
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
	log.Printf("api: bootstrap admin key: %s", result.Key)
	log.Println("api: store this key securely. it will not be printed again.")
}
