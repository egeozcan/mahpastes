package main

import (
	"context"
	"fmt"
	"log"
	"os"
	"os/signal"
	"path/filepath"
	"runtime"
	"syscall"

	webui "go-clipboard/frontend"
	coreapp "go-clipboard/internal/app"
	"go-clipboard/internal/desktopprefs"
	"go-clipboard/internal/openrequest"
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
	// Opt-in headless mode for the native worker smoke check. Nothing below is
	// bound or started unless the flag is present, so a normal run is unaffected.
	selfTestWorker := wantsWorkerSelfTest()

	// Files and mahpastes:// URLs on the command line (Send To, a drop onto the
	// exe or launcher, a file manager's Open With), or nothing for a plain launch.
	launch := launchRequest(selfTestWorker)
	// --hidden: started by the launch at login entry, which asks for no window.
	wantHidden := !selfTestWorker && !generatingBindings && openrequest.WantsStartHidden(os.Args[1:])
	// Wails dev and bindings builds parse os.Args and reject a flag they do not
	// know, so the app's own flags (--paste-clipboard, --hidden) go before
	// wails.Run.
	os.Args = openrequest.StripOwnFlags(os.Args)

	// Skip the single-instance lock during Wails binding generation: that build
	// (`-tags bindings`) only introspects bound methods and exits, so it must
	// not collide with the lock held by an already-running instance.
	if !generatingBindings {
		// A second launch against the same data dir hands its request to the
		// running window (which comes forward) and exits. The self-test never
		// does: its exit code is a CI verdict, and a handoff would fake a pass.
		instanceLock, handedOff, err := acquireInstanceOrHandOff(dataDir, launch, !selfTestWorker)
		if handedOff {
			return
		}
		if err != nil {
			showStartupError(err.Error())
			log.Fatalf("%v", err)
		}
		defer instanceLock.Release()
	}

	var selfTest *SelfTestService
	if selfTestWorker {
		selfTest = NewSelfTestService()
	}

	desktopApp := NewApp()
	core := desktopApp.core

	// Launch arguments wait in the open-request queue until the frontend has
	// loaded and takes them.
	if !launch.Empty() {
		desktopApp.submitOpenRequest(launch)
	}
	// Later launches reach this window through the instance socket.
	var instanceServer *openrequest.Server
	if !generatingBindings {
		instanceServer, err = openrequest.Listen(dataDir)
		if err != nil {
			log.Printf("instance socket unavailable; a second launch cannot reach this window: %v", err)
			instanceServer = nil
		} else {
			go instanceServer.Serve(desktopApp.submitOpenRequest)
			defer instanceServer.Close()
		}
	}

	// Settings Wails needs before it starts. The database only opens in
	// OnStartup, so these live in a small file in the data directory.
	prefs, err := desktopprefs.Load(dataDir)
	if err != nil {
		log.Printf("desktop prefs: %v (using defaults)", err)
	}
	// On Windows and Linux there is no tray or Dock: a hidden window comes
	// back only when a second launch reaches this one over the instance
	// socket. Without the socket, never hide it.
	canReshow := runtime.GOOS == "darwin" || instanceServer != nil
	keepRunning := prefs.KeepRunning(runtime.GOOS) && !selfTestWorker && !generatingBindings
	if keepRunning && !canReshow {
		log.Printf("closing the window will quit: without the instance socket a hidden window could not be shown again")
		keepRunning = false
	}
	if wantHidden && !canReshow {
		log.Printf("showing the window despite %s: without the instance socket it could not be shown again", openrequest.StartHiddenFlag)
		wantHidden = false
	}
	desktopApp.lifecycle.dataDir = dataDir
	desktopApp.lifecycle.keepRunningActive = keepRunning
	// macOS hides the app after the page loads instead of using StartHidden:
	// a window that was never shown does not come back on a Dock click.
	desktopApp.lifecycle.hideAfterLoad = wantHidden && runtime.GOOS == "darwin"
	startHidden := os.Getenv("MAHPASTES_START_HIDDEN") == "1" || selfTestWorker ||
		(wantHidden && runtime.GOOS != "darwin")

	pluginService := NewPluginService(core)
	clipboardService := NewClipboardService(core)
	transferService := NewTransferService(core)
	serveService := NewServeService(core)
	apiService := NewAPIService(core)
	shareService := NewShareService(core)
	linkService := NewLinkService(core)
	markdownService := NewMarkdownService(core)
	fileProviderService := &FileProviderService{onMount: core.SetProjectionMount}

	core.SetClipboardService(clipboardService)
	transferHandler := coreapp.NewTransferFileHandler(core)
	core.SetTransferHandler(transferHandler)

	binds := []interface{}{
		desktopApp,
		pluginService,
		clipboardService,
		transferService,
		serveService,
		apiService,
		shareService,
		linkService,
		markdownService,
		fileProviderService,
	}
	if selfTest != nil {
		// Presence of this binding is how the frontend detects self-test mode.
		binds = append(binds, selfTest)
	}

	err = wails.Run(&options.App{
		Title:       "mahpastes",
		Width:       1280,
		Height:      800,
		MinWidth:    800,
		MinHeight:   600,
		StartHidden: startHidden,
		// Closing the window hides it and the app keeps serving (watch
		// folders, tag servers, the API, Finder/file manager access). Quit
		// stays reachable: Cmd+Q or the Dock on macOS, the in-app Quit and
		// Ctrl+Q everywhere. HideWindowOnClose must not be replaced by an
		// OnBeforeClose that hides instead: Wails routes Cmd+Q through
		// OnBeforeClose too, which would make the app impossible to quit.
		HideWindowOnClose: keepRunning,
		AssetServer: &assetserver.Options{
			Assets:  webui.Assets,
			Handler: transferHandler,
		},
		BackgroundColour: &options.RGBA{R: 248, G: 250, B: 252, A: 1},
		OnStartup: func(ctx context.Context) {
			bridge := wailsbridge.New(ctx)
			desktopApp.setBridge(bridge)
			// Wails v2 installs no signal handler, so a SIGTERM (installers,
			// logout, `kill`) or Ctrl+C would end the process without
			// OnShutdown: no FUSE unmount, no plugin shutdown, no final share
			// emissions. Quit through Wails instead.
			if !generatingBindings {
				sigs := make(chan os.Signal, 2)
				signal.Notify(sigs, os.Interrupt, syscall.SIGTERM)
				go quitOnSignal(sigs, desktopApp.signalQuit, os.Exit, signalQuitGrace)
			}
			if !generatingBindings && !selfTestWorker {
				go logLoginEntryProblems()
			}
			desktopApp.attachOpenRequests(bridge)
			pluginService.setBridge(bridge)
			if selfTest != nil {
				selfTest.setBridge(bridge)
				selfTest.startWatchdog()
			}

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
			if !generatingBindings {
				fileProviderService.start(ctx, db, dataDir)
			}
		},
		OnDomReady:    desktopApp.domReady,
		OnBeforeClose: desktopApp.beforeClose,
		OnShutdown: func(ctx context.Context) {
			// Stop taking handoffs first: a launch from now on must not be
			// acknowledged by a window that is going away. It keeps trying
			// the lock and starts fresh once this process has released it.
			if instanceServer != nil {
				instanceServer.Close()
			}
			fileProviderService.stop()
			core.Shutdown(ctx)
		},
		DragAndDrop: &options.DragAndDrop{
			EnableFileDrop:     true,
			DisableWebViewDrop: false,
		},
		Bind: binds,
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
			// Finder "Open With", a drop on the Dock icon and mahpastes:// links
			// arrive as Apple Events rather than arguments. On a cold launch they
			// come before OnStartup; the open-request queue holds them until the
			// frontend takes them.
			OnFileOpen: func(path string) {
				desktopApp.submitOpenRequest(openrequest.Request{Files: []string{path}, Activate: true})
			},
			OnUrlOpen: func(raw string) {
				if u, ok := openrequest.ParseURL(raw); ok {
					desktopApp.submitOpenRequest(openrequest.Request{URLs: []string{u}, Activate: true})
				}
			},
		},
	})

	if err != nil {
		log.Fatalf("wails: %v", err)
	}

	// Printed after the webview is gone, so its result is the last thing on stdout
	// and the exit code is what CI reads.
	if selfTest != nil {
		os.Exit(selfTest.finish())
	}
}
