package main

import (
	"context"
	"fmt"
	"log"
	"os"
	"os/signal"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"

	webui "go-clipboard/frontend"
	coreapp "go-clipboard/internal/app"
)

func main() {
	log.SetFlags(log.LstdFlags | log.Lshortfile)

	dataDir, err := coreapp.GetDataDir()
	if err != nil {
		log.Fatalf("data dir: %v", err)
	}
	lock, err := coreapp.AcquireInstanceLock(dataDir)
	if err != nil {
		log.Fatalf("%v", err)
	}
	defer lock.Release()

	db, err := coreapp.InitDB()
	if err != nil {
		log.Fatalf("db: %v", err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	broker := webui.NewSSEBroker()
	core := coreapp.NewApp()
	if err := core.Bootstrap(ctx, coreapp.BootstrapOptions{
		DB:                 db,
		DataDir:            dataDir,
		Bridge:             broker,
		InitClipboard:      false,
		PermissionCallback: headlessPermissionCallback(dataDir),
		FSConfinementRoot:  dataDir,
	}); err != nil {
		log.Fatalf("bootstrap: %v", err)
	}

	core.APIManager().MountWebUI(webui.Assets, broker)
	coreapp.BootstrapAPIKey(core.APIManager())

	port := 44557
	if p := os.Getenv("MAHPASTESD_PORT"); p != "" {
		parsed, err := strconv.Atoi(p)
		if err != nil {
			log.Fatalf("MAHPASTESD_PORT=%q is not an integer", p)
		}
		port = parsed
	}
	bindAll := os.Getenv("MAHPASTESD_BIND_ALL") == "1"
	if bindAll {
		log.Println("WARNING: listening on 0.0.0.0 without TLS.")
		log.Println("API keys and session cookies are sent in plaintext.")
		log.Println("Use a reverse proxy that terminates TLS before network exposure.")
		if os.Getenv("MAHPASTESD_TRUST_PROXY") != "1" {
			log.Println("Behind a TLS-terminating proxy, set MAHPASTESD_TRUST_PROXY=1 so")
			log.Println("session cookies get the Secure flag (via X-Forwarded-Proto) and the")
			log.Println("login rate-limiter buckets on the real client IP (X-Forwarded-For).")
		}
	}

	status, err := core.APIManager().Start(port, bindAll)
	if err != nil {
		log.Fatalf("api: %v", err)
	}
	log.Printf("listening on %s", status.URL)

	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)
	<-sigCh
	log.Println("shutting down...")
	cancel()
	core.Shutdown(ctx)
}

func headlessPermissionCallback(dataDir string) func(name, kind, p string) string {
	absData, err := filepath.Abs(dataDir)
	if err != nil {
		return func(name, kind, p string) string { return "" }
	}
	if resolved, err := filepath.EvalSymlinks(absData); err == nil {
		absData = resolved
	}
	return func(name, kind, p string) string {
		canonical, err := canonicalizeViaAncestor(p)
		if err != nil {
			return ""
		}
		rel, err := filepath.Rel(absData, canonical)
		if err != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
			return ""
		}
		return p
	}
}

func canonicalizeViaAncestor(p string) (string, error) {
	abs, err := filepath.Abs(p)
	if err != nil {
		return "", err
	}
	cur := filepath.Clean(abs)
	var suffix []string
	for {
		if _, err := os.Lstat(cur); err == nil {
			resolved, err := filepath.EvalSymlinks(cur)
			if err != nil {
				return "", err
			}
			for i := len(suffix) - 1; i >= 0; i-- {
				resolved = filepath.Join(resolved, suffix[i])
			}
			return filepath.Clean(resolved), nil
		}
		parent := filepath.Dir(cur)
		if parent == cur {
			return "", fmt.Errorf("no existing ancestor for %q", p)
		}
		suffix = append(suffix, filepath.Base(cur))
		cur = parent
	}
}
