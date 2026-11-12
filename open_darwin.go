//go:build darwin

package main

import (
	"bytes"
	"context"
	"fmt"
	"os/exec"
	"strings"
	"time"

	"go-clipboard/internal/wailsbridge"
)

func init() {
	openWithAppLister = listOpenWithApps
}

// openWithAppsScript asks Launch Services, through NSWorkspace, which
// applications can open the file named by its argument and which one is the
// default, and prints them as JSON. It runs under osascript's JavaScript for
// Automation bridge, so no Objective-C is compiled into the app. It sends no
// Apple Events, so it raises no Automation permission prompt.
// URLsForApplicationsToOpenURL: needs macOS 12; on older systems only the
// default application is listed.
const openWithAppsScript = `ObjC.import('AppKit');
function run(argv) {
    var ws = $.NSWorkspace.sharedWorkspace;
    var fm = $.NSFileManager.defaultManager;
    var url = $.NSURL.fileURLWithPath(argv[0]);
    var entry = function (appURL) {
        var path = ObjC.unwrap(appURL.path);
        return { path: path, name: ObjC.unwrap(fm.displayNameAtPath(path)) };
    };
    var result = { defaultApp: null, apps: [] };
    try {
        var def = ws.URLForApplicationToOpenURL(url);
        if (def && !def.isNil()) result.defaultApp = entry(def);
    } catch (e) {}
    try {
        var list = ws.URLsForApplicationsToOpenURL(url);
        var count = list.count;
        for (var i = 0; i < count; i++) result.apps.push(entry(list.objectAtIndex(i)));
    } catch (e) {}
    return JSON.stringify(result);
}`

// openWithAppsTimeout bounds the lookup; a stuck osascript is killed and the
// menu falls back to the application picker.
const openWithAppsTimeout = 10 * time.Second

// listOpenWithApps lists the applications registered for probePath's type.
func listOpenWithApps(probePath string) ([]OpenWithApp, error) {
	ctx, cancel := context.WithTimeout(context.Background(), openWithAppsTimeout)
	defer cancel()
	var stdout, stderr bytes.Buffer
	cmd := exec.CommandContext(ctx, "/usr/bin/osascript", "-l", "JavaScript", "-e", openWithAppsScript, probePath)
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		return nil, fmt.Errorf("failed to list applications: %w: %s", err, strings.TrimSpace(stderr.String()))
	}
	return openWithAppsFromScript(stdout.Bytes())
}

func openFileWithDefaultApp(path string) error {
	cmd := exec.Command("open", path)
	if out, err := cmd.CombinedOutput(); err != nil {
		return fmt.Errorf("failed to open file: %w: %s", err, string(out))
	}
	return nil
}

func openFileWithApp(filePath, appPath string) error {
	cmd := exec.Command("open", "-a", appPath, filePath)
	if out, err := cmd.CombinedOutput(); err != nil {
		return fmt.Errorf("failed to open file with app: %w: %s", err, string(out))
	}
	return nil
}

func chooseApplicationDialog(b *wailsbridge.Bridge) (string, error) {
	path, err := b.OpenFile(wailsbridge.FileDialogOptions{
		Title:            "Choose Application",
		DefaultDirectory: "/Applications",
		Filters: []wailsbridge.FileFilter{
			{DisplayName: "Applications", Pattern: "*.app"},
		},
	})
	if err != nil {
		return "", fmt.Errorf("failed to open file dialog: %w", err)
	}
	return path, nil
}

func revealInFileManager(path string) error {
	cmd := exec.Command("open", "-R", path)
	if out, err := cmd.CombinedOutput(); err != nil {
		return fmt.Errorf("failed to reveal file: %w: %s", err, string(out))
	}
	return nil
}
