package main

import (
	"fmt"
	"io"
	"mime"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"time"

	"go-clipboard/cmd/mp/client"

	"github.com/spf13/cobra"
)

// --- clip open ---

var clipOpenCmd = &cobra.Command{
	Use:   "open <id>",
	Short: "Open a clip in its default application",
	Long: `Download a clip to a private folder and open it with the application the
operating system uses for that file type, or with --app.

The copy stays in mp's cache folder (mahpastes/mp-open under your user cache
directory) because the application reads it after mp has exited; delete that
folder whenever you like. Editing the copy does not change the clip.

Executable file types (.exe, .bat, .command, .desktop and similar) are not
opened by default; use "mp clip download" for those.

--in-app instead brings the running mahpastes window forward and shows the clip,
through its mahpastes:// link. That needs the desktop app on this computer
with the link handler registered (macOS, and Linux after scripts/linux/install.sh);
it does not use the API.`,
	Example: `  # Open in the default application
  mp clip open 42

  # Open with a specific application
  mp clip open 42 --app "Preview"        # macOS: an application name or path
  mp clip open 42 --app gimp             # Linux/Windows: a program to run

  # Show the clip in the mahpastes window
  mp clip open 42 --in-app`,
	Args: cobra.ExactArgs(1),
	RunE: runClipOpen,
}

var (
	clipOpenApp   string
	clipOpenInApp bool
)

func init() {
	clipOpenCmd.Flags().StringVar(&clipOpenApp, "app", "", "Application to open the clip with")
	clipOpenCmd.Flags().BoolVar(&clipOpenInApp, "in-app", false, "Show the clip in the mahpastes desktop app instead")
	clipOpenCmd.MarkFlagsMutuallyExclusive("app", "in-app")
}

// startOpener starts an opener command. Tests replace it so that nothing is
// ever launched.
var startOpener = startDetached

// openerWait is how long mp waits for an opener to fail before it assumes the
// file was handed over. open, xdg-open and rundll32 return at once; a program
// given with --app keeps running, and mp must not wait for it.
var openerWait = 3 * time.Second

// startDetached starts name with args, without stdio so that it can outlive
// mp, and reports a failure that happens within openerWait.
func startDetached(name string, args ...string) error {
	cmd := exec.Command(name, args...)
	if err := cmd.Start(); err != nil {
		return err
	}
	done := make(chan error, 1)
	go func() { done <- cmd.Wait() }()
	select {
	case err := <-done:
		if err != nil {
			return fmt.Errorf("%s: %w", name, err)
		}
		return nil
	case <-time.After(openerWait):
		_ = cmd.Process.Release()
		return nil
	}
}

// openerCommand returns the command that opens target (a file path or a URL)
// with the default handler, or with app.
func openerCommand(goos, target, app string) (string, []string) {
	switch goos {
	case "darwin":
		if app != "" {
			return "open", []string{"-a", app, target}
		}
		return "open", []string{target}
	case "windows":
		if app != "" {
			return app, []string{target}
		}
		return "rundll32", []string{"url.dll,FileProtocolHandler", target}
	default:
		if app != "" {
			return app, []string{target}
		}
		return "xdg-open", []string{target}
	}
}

// executableExtensions are file types that the default handler runs rather
// than displays on at least one OS. A clip is whatever someone pasted, shared
// or dropped into a watch folder, so "open" never runs one.
var executableExtensions = map[string]bool{
	".exe": true, ".com": true, ".bat": true, ".cmd": true, ".ps1": true, ".psm1": true,
	".vbs": true, ".vbe": true, ".js": true, ".jse": true, ".wsf": true, ".wsh": true,
	".msi": true, ".msp": true, ".scr": true, ".lnk": true, ".url": true, ".hta": true,
	".cpl": true, ".pif": true, ".reg": true, ".appref-ms": true, ".application": true,
	".jar": true, ".command": true, ".tool": true, ".terminal": true, ".desktop": true,
	".app": true, ".pkg": true, ".scpt": true, ".workflow": true,
}

// commonExtensions names files whose clip has no filename. mime's own table
// lists several extensions per type in no useful order.
var commonExtensions = map[string]string{
	"text/plain":       ".txt",
	"text/markdown":    ".md",
	"text/html":        ".html",
	"text/csv":         ".csv",
	"application/json": ".json",
	"application/pdf":  ".pdf",
	"image/png":        ".png",
	"image/jpeg":       ".jpg",
	"image/gif":        ".gif",
	"image/webp":       ".webp",
	"image/svg+xml":    ".svg",
}

// openFileName is a safe local name for a clip: its own filename without any
// directory part, or clip_<id> plus an extension for its content type.
func openFileName(id int64, filename, contentType string) string {
	name := strings.TrimSpace(filename)
	// The server's filename is untrusted: keep the last element only, with
	// either separator, and nothing that names a directory.
	if i := strings.LastIndexAny(name, `/\`); i >= 0 {
		name = name[i+1:]
	}
	name = strings.Map(func(r rune) rune {
		if r < 0x20 || r == 0x7f || strings.ContainsRune(`<>:"|?*`, r) {
			return '_'
		}
		return r
	}, name)
	name = strings.TrimRight(name, ". ")
	if name == "" || name == "." || name == ".." {
		name = "clip_" + strconv.FormatInt(id, 10)
		mediaType, _, _ := mime.ParseMediaType(contentType)
		if ext, ok := commonExtensions[mediaType]; ok {
			name += ext
		} else if exts, _ := mime.ExtensionsByType(mediaType); len(exts) > 0 {
			name += exts[0]
		}
	}
	if len(name) > 200 {
		ext := filepath.Ext(name)
		if len(ext) > 20 {
			ext = ""
		}
		name = name[:200-len(ext)] + ext
	}
	return name
}

// openCacheDir is where opened copies go: a folder only this user can read.
func openCacheDir() (string, error) {
	base, err := os.UserCacheDir()
	if err != nil {
		return "", fmt.Errorf("find a cache folder for the opened copy: %w", err)
	}
	dir := filepath.Join(base, "mahpastes", "mp-open")
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return "", fmt.Errorf("create %s: %w", dir, err)
	}
	return dir, nil
}

func runClipOpen(cmd *cobra.Command, args []string) error {
	id, err := strconv.ParseInt(args[0], 10, 64)
	if err != nil || id < 1 {
		return fmt.Errorf("invalid clip ID: %s", args[0])
	}

	if clipOpenInApp {
		return openClipInApp(id)
	}

	c, err := client.New()
	if err != nil {
		return err
	}
	var clip struct {
		Filename    string `json:"filename"`
		ContentType string `json:"content_type"`
	}
	if err := c.GetJSON(fmt.Sprintf("/api/v1/clips/%d", id), &clip); err != nil {
		return err
	}
	name := openFileName(id, clip.Filename, clip.ContentType)
	if clipOpenApp == "" && executableExtensions[strings.ToLower(filepath.Ext(name))] {
		return fmt.Errorf("clip %d is an executable file type (%s); not opening it. Use \"mp clip download %d\" to save it instead", id, filepath.Ext(name), id)
	}

	cacheDir, err := openCacheDir()
	if err != nil {
		return err
	}
	// One folder per open, so opening the same clip twice, or two clips with
	// the same name, never overwrites a file an application still has open.
	dir, err := os.MkdirTemp(cacheDir, fmt.Sprintf("clip-%d-", id))
	if err != nil {
		return fmt.Errorf("create a folder for the opened copy: %w", err)
	}
	path := filepath.Join(dir, name)

	body, _, err := c.GetRaw(fmt.Sprintf("/api/v1/clips/%d/data", id))
	if err != nil {
		os.RemoveAll(dir)
		return err
	}
	defer body.Close()
	if err := writeOpenCopy(path, body); err != nil {
		os.RemoveAll(dir)
		return err
	}

	opener, openerArgs := openerCommand(runtime.GOOS, path, clipOpenApp)
	if err := startOpener(opener, openerArgs...); err != nil {
		return fmt.Errorf("open %s: %w", path, err)
	}

	if jsonOutput {
		printJSON(map[string]interface{}{"id": id, "path": path, "app": clipOpenApp})
		return nil
	}
	fmt.Printf("Opened clip %d from %s\n", id, path)
	return nil
}

func writeOpenCopy(path string, r io.Reader) error {
	f, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
	if err != nil {
		return fmt.Errorf("create %s: %w", path, err)
	}
	if _, err := io.Copy(f, r); err != nil {
		f.Close()
		return fmt.Errorf("write %s: %w", path, err)
	}
	if err := f.Close(); err != nil {
		return fmt.Errorf("write %s: %w", path, err)
	}
	return nil
}

// clipLink is the mahpastes:// link that shows clip id in the desktop app
// (internal/openrequest's grammar).
func clipLink(id int64) string {
	return "mahpastes://clip/" + strconv.FormatInt(id, 10)
}

// windowsSchemeRegistered reports whether some installer registered the
// mahpastes: URL scheme. Tests replace it.
var windowsSchemeRegistered = func() bool {
	return exec.Command("reg", "query", `HKCR\mahpastes`, "/v", "URL Protocol").Run() == nil
}

// openClipInApp hands the clip's mahpastes:// link to the OS, which passes it
// to the running app (or starts it).
func openClipInApp(id int64) error {
	if runtime.GOOS == "windows" && !windowsSchemeRegistered() {
		// The zip build registers nothing, and rundll32 would only show "no
		// app to open this link".
		return fmt.Errorf("--in-app needs the mahpastes:// link handler, which is not registered on this computer; run \"mahpastes.exe %s\" instead", clipLink(id))
	}
	link := clipLink(id)
	opener, openerArgs := openerCommand(runtime.GOOS, link, "")
	if err := startOpener(opener, openerArgs...); err != nil {
		return fmt.Errorf("open %s: %w", link, err)
	}
	if jsonOutput {
		printJSON(map[string]interface{}{"id": id, "url": link})
		return nil
	}
	fmt.Printf("Asked mahpastes to show clip %d\n", id)
	return nil
}
