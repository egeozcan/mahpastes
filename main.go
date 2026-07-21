package main

import (
	"context"
	"fmt"
	"log"
	"os"
	"path/filepath"

	webui "go-clipboard/frontend"
	coreapp "go-clipboard/internal/app"
	"go-clipboard/internal/wailsbridge"

	"github.com/wailsapp/wails/v2"
	"github.com/wailsapp/wails/v2/pkg/options"
	"github.com/wailsapp/wails/v2/pkg/options/assetserver"
	"github.com/wailsapp/wails/v2/pkg/options/mac"
)

func main() {
	log.SetFlags(log.LstdFlags | log.Lshortfile)

	dataDir, err := coreapp.GetDataDir()
	if err != nil {
		log.Fatalf("data dir: %v", err)
	}
	// Skip the single-instance lock during Wails binding generation: that build
	// (`-tags bindings`) only introspects bound methods and exits, so it must
	// not collide with the lock held by an already-running instance.
	if !generatingBindings {
		instanceLock, err := coreapp.AcquireInstanceLock(dataDir)
		if err != nil {
			log.Fatalf("%v", err)
		}
		defer instanceLock.Release()
	}

	desktopApp := NewApp()
	core := desktopApp.core

	pluginService := NewPluginService(core)
	clipboardService := NewClipboardService(core)
	transferService := NewTransferService(core)
	serveService := NewServeService(core)
	apiService := NewAPIService(core)
	shareService := NewShareService(core)
	linkService := NewLinkService(core)
	markdownService := NewMarkdownService(core)

	core.SetClipboardService(clipboardService)
	transferHandler := coreapp.NewTransferFileHandler(core)
	core.SetTransferHandler(transferHandler)

	err = wails.Run(&options.App{
		Title:       "mahpastes",
		Width:       1280,
		Height:      800,
		MinWidth:    800,
		MinHeight:   600,
		StartHidden: os.Getenv("MAHPASTES_START_HIDDEN") == "1",
		AssetServer: &assetserver.Options{
			Assets:  webui.Assets,
			Handler: transferHandler,
		},
		BackgroundColour: &options.RGBA{R: 248, G: 250, B: 252, A: 1},
		OnStartup: func(ctx context.Context) {
			bridge := wailsbridge.New(ctx)
			desktopApp.setBridge(bridge)
			pluginService.setBridge(bridge)

			// Map the mouse back/forward side buttons to in-app navigation.
			// On macOS this installs a native NSEvent monitor (WKWebView's DOM
			// reporting of the side buttons is unreliable); other platforms
			// no-op here and read the buttons from the DOM in the frontend.
			installMouseNavMonitor(func(direction string) {
				go bridge.Emit("mouse:nav", direction)
			})

			db, err := coreapp.InitDB()
			if err != nil {
				log.Fatalf("db: %v", err)
			}
			err = core.Bootstrap(ctx, coreapp.BootstrapOptions{
				DB:            db,
				DataDir:       dataDir,
				Bridge:        bridge,
				InitClipboard: true,
				PermissionCallback: func(pluginName, permType, requestedPath string) string {
					path, err := bridge.OpenDirectory(wailsbridge.FileDialogOptions{
						Title:                fmt.Sprintf("Plugin %q requests %s access", pluginName, permType),
						DefaultDirectory:     filepath.Dir(requestedPath),
						CanCreateDirectories: permType == "fs_write",
					})
					if err != nil || path == "" {
						return ""
					}
					return path
				},
			})
			if err != nil {
				log.Fatalf("bootstrap: %v", err)
			}
		},
		OnShutdown: func(ctx context.Context) { core.Shutdown(ctx) },
		DragAndDrop: &options.DragAndDrop{
			EnableFileDrop:     true,
			DisableWebViewDrop: false,
		},
		Bind: []interface{}{
			desktopApp,
			pluginService,
			clipboardService,
			transferService,
			serveService,
			apiService,
			shareService,
			linkService,
			markdownService,
		},
		Mac: &mac.Options{
			TitleBar: &mac.TitleBar{
				TitlebarAppearsTransparent: true,
				HideTitle:                  true,
				HideTitleBar:               false,
				FullSizeContent:            true,
				UseToolbar:                 false,
			},
			About: &mac.AboutInfo{
				Title:   "mahpastes",
				Message: "A local clipboard manager",
			},
		},
	})

	if err != nil {
		log.Fatalf("wails: %v", err)
	}
}
