package main

import (
	"context"
	_ "embed"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"runtime"
	"sync"

	webui "go-clipboard/frontend"
	coreapp "go-clipboard/internal/app"
	"go-clipboard/internal/bridgeiface"
	"go-clipboard/internal/buildinfo"
	"go-clipboard/internal/logfile"
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
	// Skip the single-instance lock during Wails binding generation: that build
	// (`-tags bindings`) only introspects bound methods and exits, so it must
	// not collide with the lock held by an already-running instance.
	if !generatingBindings {
		instanceLock, err := coreapp.AcquireInstanceLock(dataDir)
		if err != nil {
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

	// Opt-in headless mode for the native worker smoke check. Nothing below is
	// bound or started unless the flag is present, so a normal run is unaffected.
	selfTestWorker := wantsWorkerSelfTest()
	var selfTest *SelfTestService
	if selfTestWorker {
		selfTest = NewSelfTestService()
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
	headless := os.Getenv("MAHPASTES_START_HIDDEN") == "1" || selfTestWorker
	winW, winH := windowstate.DefaultWidth, windowstate.DefaultHeight
	startState := options.Normal
	saved, haveSaved := windowstate.State{}, false
	if !headless && !generatingBindings {
		if saved, haveSaved = windowstate.Load(dataDir); haveSaved {
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
		StartHidden:      headless,
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
			if !haveSaved {
				return
			}
			restoreOnce.Do(func() {
				restoreWindowGeometry(wailsbridge.New(ctx), saved)
			})
		},
		OnBeforeClose: func(ctx context.Context) bool {
			if !headless && !generatingBindings {
				saveWindowGeometry(wailsbridge.New(ctx), dataDir)
			}
			return false
		},
		OnShutdown: func(ctx context.Context) {
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
