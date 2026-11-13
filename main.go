package main

import (
	"context"
	_ "embed"
	"fmt"
	"log"
	"os"
	"os/signal"
	"path/filepath"
	"runtime"
	"sync"
	"syscall"

	webui "go-clipboard/frontend"
	coreapp "go-clipboard/internal/app"
	"go-clipboard/internal/bridgeiface"
	"go-clipboard/internal/buildinfo"
	"go-clipboard/internal/desktopprefs"
	"go-clipboard/internal/logfile"
	"go-clipboard/internal/openrequest"
	"go-clipboard/internal/osnotify"
	"go-clipboard/internal/wailsbridge"
	"go-clipboard/internal/windowstate"

	"github.com/wailsapp/wails/v2"
	"github.com/wailsapp/wails/v2/pkg/menu"
	"github.com/wailsapp/wails/v2/pkg/options"
	"github.com/wailsapp/wails/v2/pkg/options/assetserver"
	"github.com/wailsapp/wails/v2/pkg/options/linux"
	"github.com/wailsapp/wails/v2/pkg/options/mac"
	"github.com/wailsapp/wails/v2/pkg/options/windows"
)

// appIcon is a 256px copy of build/appicon.png for the window icon (Linux)
// and the About panel (macOS); the full-size source is 1536px.
//
//go:embed build/appicon-256.png
var appIcon []byte

func main() {
	log.SetFlags(log.LstdFlags | log.Lshortfile)

	dataDir, err := coreapp.GetDataDir()
	if err != nil {
		// No log file yet, and a Windows GUI exe or a Finder-launched .app has
		// no console: without the dialog the app would just not start.
		if !generatingBindings {
			showStartupError(fmt.Sprintf("mahpastes cannot use its data folder: %v", err))
		}
		log.Fatalf("data dir: %v", err)
	}
	// Keep a log in the data directory: stderr is discarded for a Windows GUI
	// exe and a Finder-launched .app. Skipped for binding generation.
	var logWriter *logfile.Writer
	if !generatingBindings {
		if lw, err := logfile.Open(dataDir); err != nil {
			log.Printf("log file unavailable: %v", err)
		} else {
			defer lw.Close()
			logWriter = lw
			log.SetOutput(lw.Tee(os.Stderr))
			// log.SetOutput does not capture runtime panic traces.
			if err := lw.CaptureCrashes(); err != nil {
				log.Printf("crash output unavailable: %v", err)
			}
		}
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
			showStartupError(startupFailureMessage(err.Error(), logWriter != nil, dataDir))
			log.Fatalf("%v", err)
		}
		defer instanceLock.Release()
		// Only the instance that owns the data directory rotates the log, so
		// a second launch cannot move the running instance's file away.
		if logWriter != nil {
			if err := logWriter.EnableRotation(); err != nil {
				log.Printf("log rotation: %v", err)
			}
		}
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
	// macOS hands the launch's file arguments to OnFileOpen as well.
	launchEcho := newLaunchFileEcho(launch)
	// Later launches reach this window through the instance socket.
	var instanceServer *openrequest.Server
	if !generatingBindings {
		instanceServer, err = openrequest.Listen(dataDir)
		if err != nil {
			log.Printf("instance socket unavailable; a second launch cannot reach this window: %v", err)
			instanceServer = nil
		} else {
			go instanceServer.Serve(desktopApp.submitRelaunchRequest)
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
	// headless runs (the e2e harness's MAHPASTES_START_HIDDEN, the worker
	// self-test) never show the window; a login start (--hidden) shows it
	// later, so only headless runs skip the window geometry and release check.
	headless := os.Getenv("MAHPASTES_START_HIDDEN") == "1" || selfTestWorker
	startHidden := headless || (wantHidden && runtime.GOOS != "darwin")
	desktopApp.lifecycle.saveGeometryOnHide = keepRunning && !headless && !generatingBindings

	pluginService := NewPluginService(core)
	clipboardService := NewClipboardService(core)
	transferService := NewTransferService(core)
	serveService := NewServeService(core)
	apiService := NewAPIService(core)
	shareService := NewShareService(core)
	linkService := NewLinkService(core)
	markdownService := NewMarkdownService(core)
	fileProviderService := &FileProviderService{onMount: core.SetProjectionMount}
	notificationService := NewNotificationService(core)
	appInfoService := NewAppInfoService(core)
	menuEvents := &menuEmitter{}

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
		notificationService,
		appInfoService,
	}
	if selfTest != nil {
		// Presence of this binding is how the frontend detects self-test mode.
		binds = append(binds, selfTest)
	}

	// Restore the last window geometry unless this is a headless run.
	winW, winH := windowstate.DefaultWidth, windowstate.DefaultHeight
	startState := options.Normal
	saved, haveSaved := windowstate.State{}, false
	if !headless && !generatingBindings {
		if saved, haveSaved = windowstate.Load(dataDir); haveSaved {
			// No geometry save until OnDomReady has restored this one.
			geometryRestorePending.Store(true)
			winW, winH = saved.Width, saved.Height
			if saved.Maximised {
				startState = options.Maximised
			}
		}
	}
	// OnDomReady fires again whenever the page reloads (settings.js reloads it
	// after some changes); the saved geometry is applied on the first load only.
	var restoreOnce sync.Once

	// The macOS menu bar; other platforms keep a menu-less window.
	var appMenu *menu.Menu
	if runtime.GOOS == "darwin" {
		appMenu = buildAppMenu(menuEvents)
	}

	err = wails.Run(&options.App{
		Title:            "mahpastes",
		Width:            winW,
		Height:           winH,
		WindowStartState: startState,
		MinWidth:         windowstate.MinWidth,
		MinHeight:        windowstate.MinHeight,
		StartHidden:      startHidden,
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
		Menu:             appMenu,
		OnStartup: func(ctx context.Context) {
			bridge := wailsbridge.New(ctx)
			desktopApp.setBridge(bridge)
			menuEvents.set(bridge.Emit)
			// Wails' own signal handler quits through OnBeforeClose, so a
			// SIGTERM (installers, logout, `kill`) or Ctrl+C would wait on the
			// unsaved-changes prompt. Ours skips the prompt (forceQuit), still
			// quits through Wails so OnShutdown runs (FUSE unmount, plugin
			// shutdown, final share emissions), and exits after a grace
			// period or a second signal.
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

			// Native notifications for background events while the window is
			// unfocused (off by default). The core gets a bridge that also
			// feeds them; headless runs never construct one.
			var coreBridge bridgeiface.Bridge = bridge
			var notifier *osnotify.Notifier
			if !headless && !generatingBindings {
				notifier = newDesktopNotifier(bridge, core)
				notificationService.setNotifier(notifier)
				coreBridge = osnotify.Wrap(bridge, notifier, core.ClassifyBackgroundEvent)
			}

			db, err := coreapp.InitDB()
			if err != nil {
				fatalStartup(bridge, headless, logWriter != nil, dataDir, "Could not open the database", err)
			}
			err = core.Bootstrap(ctx, coreapp.BootstrapOptions{
				DB:            db,
				DataDir:       dataDir,
				Bridge:        coreBridge,
				InitClipboard: true,
				PermissionCallback: func(pluginName, permType, requestedPath string) string {
					// A headless run has no window to answer the folder
					// picker: on Linux the GTK dialog would block the plugin
					// (and the clip event it is handling) forever. Decline.
					if headless {
						return ""
					}
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
				fatalStartup(bridge, headless, logWriter != nil, dataDir, "Could not start mahpastes", err)
			}
			if !generatingBindings {
				// "Start on launch" belongs to the desktop app; the headless
				// worker self-test must not open the user's API listener.
				if selfTest == nil {
					apiService.startOnLaunch()
				}
				fileProviderService.start(ctx, db, dataDir)
			}
			if notifier != nil {
				notifier.Start()
			}
			if !headless && !generatingBindings {
				appInfoService.start(ctx, bridge)
			}
		},
		OnDomReady: func(ctx context.Context) {
			desktopApp.domReady(ctx)
			if !haveSaved {
				return
			}
			restoreOnce.Do(func() {
				restoreWindowGeometry(wailsbridge.New(ctx), saved)
			})
		},
		// Every quit (close button without keep-running, Cmd+Q, the Dock,
		// the in-app Quit, a signal) passes here while the window getters
		// still work. With keep-running the close button only hides the
		// window, without coming here; App.WindowHidden saves the geometry
		// then, and the quit that ends the app saves it again. Saving first is
		// harmless when the unsaved-changes prompt then keeps the app open.
		OnBeforeClose: func(ctx context.Context) bool {
			if !headless && !generatingBindings {
				saveWindowGeometry(wailsbridge.New(ctx), dataDir)
			}
			return desktopApp.beforeClose(ctx)
		},
		OnShutdown: func(ctx context.Context) {
			// Stop taking handoffs first: a launch from now on must not be
			// acknowledged by a window that is going away. It keeps trying
			// the lock and starts fresh once this process has released it.
			if instanceServer != nil {
				instanceServer.Close()
			}
			appInfoService.stop()
			notificationService.stop()
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
				Message: "A local clipboard manager\nVersion " + buildinfo.Current(),
				Icon:    appIcon,
			},
			// Finder "Open With", a drop on the Dock icon and mahpastes:// links
			// arrive as Apple Events rather than arguments. On a cold launch they
			// come before OnStartup; the open-request queue holds them until the
			// frontend takes them.
			OnFileOpen: func(path string) {
				desktopApp.submitOpenRequest(launchEcho.request(path))
			},
			OnUrlOpen: func(raw string) {
				if u, ok := openrequest.ParseURL(raw); ok {
					desktopApp.submitOpenRequest(openrequest.Request{URLs: []string{u}, Activate: true})
				}
			},
		},
		// The UI has no dark styles, so keep the title bar light even when
		// Windows is in dark mode. IsZoomControlEnabled keeps WebView2's
		// default (Ctrl+wheel / Ctrl+- zoom): with a non-nil Windows block
		// Wails applies the zero value, which would switch zoom off.
		Windows: &windows.Options{
			Theme:                windows.Light,
			IsZoomControlEnabled: true,
		},
		// ProgramName fixes the GTK program name / Wayland app_id and Icon
		// the window icon (X11, un-installed runs). The GPU policy is spelled
		// out because a non-nil Linux block would otherwise default to Always.
		Linux: &linux.Options{
			Icon:             appIcon,
			ProgramName:      "mahpastes",
			WebviewGpuPolicy: linux.WebviewGpuPolicyNever,
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

// startupFailureMessage is the dialog text for a failure before Wails starts,
// naming the log folder when a log was opened.
func startupFailureMessage(msg string, haveLog bool, dataDir string) string {
	if haveLog {
		msg += "\n\nDetails are in " + logfile.Dir(dataDir)
	}
	return msg
}

// fatalStartup logs a startup failure, tells the user in a native dialog (the
// window would otherwise just vanish) and exits. Headless runs (e2e, the
// worker self-test) skip the dialog: nobody could dismiss it.
func fatalStartup(bridge *wailsbridge.Bridge, headless, haveLog bool, dataDir, title string, err error) {
	log.Printf("fatal startup error: %s: %v", title, err)
	if !headless {
		msg := err.Error()
		if haveLog {
			msg += "\n\nDetails are in " + logfile.Dir(dataDir)
		}
		bridge.ErrorDialog("mahpastes: "+title, msg)
	}
	os.Exit(1)
}
