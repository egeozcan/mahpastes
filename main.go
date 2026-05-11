package main

import (
	"embed"
	"log"
	"os"

	"github.com/wailsapp/wails/v2"
	"github.com/wailsapp/wails/v2/pkg/options"
	"github.com/wailsapp/wails/v2/pkg/options/assetserver"
	"github.com/wailsapp/wails/v2/pkg/options/mac"
)

//go:embed all:frontend
var assets embed.FS

func main() {
	log.SetFlags(log.LstdFlags | log.Lshortfile)

	// Acquire single-instance lock before doing any other startup work.
	// Different MAHPASTES_DATA_DIR values map to different lock files, so
	// multiple instances against different data directories are allowed.
	dataDir, err := getDataDir()
	if err != nil {
		log.Fatalf("Error resolving data directory: %v", err)
	}
	instanceLock, err := AcquireInstanceLock(dataDir)
	if err != nil {
		log.Fatalf("%v", err)
	}
	defer instanceLock.Release()

	// Create an instance of the app structure
	app := NewApp()

	// Create services (separate structs to stay under Wails ~49 method binding limit)
	pluginService := NewPluginService(app)
	clipboardService := NewClipboardService(app)
	transferService := NewTransferService(app)
	serveService := NewServeService(app)
	apiService := NewAPIService(app)
	shareService := NewShareService(app)

	// Wire service references so App can delegate to them from API endpoints
	app.clipboardService = clipboardService

	// Create the transfer file handler and store it on the app for token registration.
	transferHandler := &TransferFileHandler{app: app}
	app.transferHandler = transferHandler

	// Create application with options
	err = wails.Run(&options.App{
		Title:     "mahpastes",
		Width:     1280,
		Height:    800,
		MinWidth:  800,
		MinHeight: 600,
		StartHidden: os.Getenv("MAHPASTES_START_HIDDEN") == "1",
		AssetServer: &assetserver.Options{
			Assets:  assets,
			Handler: transferHandler,
		},
		BackgroundColour: &options.RGBA{R: 248, G: 250, B: 252, A: 1},
		OnStartup:        app.startup,
		OnShutdown:       app.shutdown,
		DragAndDrop: &options.DragAndDrop{
			EnableFileDrop:     true,
			DisableWebViewDrop: false,
		},
		Bind: []interface{}{
			app,
			pluginService,
			clipboardService,
			transferService,
			serveService,
			apiService,
			shareService,
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
		log.Fatalf("Error starting Wails: %v", err)
	}
}
