//go:build darwin

package main

import (
	"context"
	"os"
	"os/exec"
	"strings"
	"time"

	"go-clipboard/internal/openrequest"
)

// How long the alert stays up before it dismisses itself, and how long the
// process waits for osascript beyond that. A launch nobody is watching must
// still exit.
const (
	startupAlertGiveUp  = 2 * time.Minute
	startupAlertTimeout = startupAlertGiveUp + 10*time.Second
)

// startupAlertScript takes the message as an argument and never has it
// spliced in: the text carries user-controlled paths.
const startupAlertScript = `on run argv
	display alert "mahpastes could not start" message (item 1 of argv) as critical buttons {"OK"} default button "OK" giving up after 120
end run
`

// presentStartupAlert is replaced in tests, which must never raise a dialog.
var presentStartupAlert = runStartupAlert

// startupArgs is the command line as launched. main strips --hidden from
// os.Args before the instance-lock failure reports, so os.Args cannot be asked.
var startupArgs = os.Args[1:]

// showStartupError puts a failure from before Wails starts in front of the
// user: a .app launched from Finder, the Dock or a login item has stderr on
// /dev/null, so without it the Dock icon bounces and disappears. Runs nobody
// could answer a dialog in, and runs whose stderr somebody reads, stay silent.
func showStartupError(message string) {
	if !startupAlertWanted(os.Getenv, startupArgs, stderrDiscarded()) {
		return
	}
	presentStartupAlert(message)
}

// startupAlertWanted mirrors the headless test fatalStartup uses (main computes
// it after the first caller, so it is derived again here), and adds --hidden:
// a login item's failure belongs in the log, not on the login screen.
func startupAlertWanted(getenv func(string) string, args []string, stderrDiscarded bool) bool {
	if generatingBindings || !stderrDiscarded {
		return false
	}
	if getenv("MAHPASTES_START_HIDDEN") == "1" || getenv(selfTestWorkerEnv) == "1" {
		return false
	}
	return !openrequest.WantsStartHidden(args)
}

// stderrDiscarded reports whether stderr is the null device, which is what
// launchd gives a GUI launch. A terminal, a pipe (tests, CI) or a file already
// shows or keeps the message.
func stderrDiscarded() bool {
	errInfo, err := os.Stderr.Stat()
	if err != nil {
		return false
	}
	nullInfo, err := os.Stat(os.DevNull)
	if err != nil {
		return false
	}
	return os.SameFile(errInfo, nullInfo)
}

// startupAlertCommand reads the script from stdin ("-") so the message is the
// only thing after it: osascript stops reading options there.
func startupAlertCommand(ctx context.Context, message string) *exec.Cmd {
	cmd := exec.CommandContext(ctx, "/usr/bin/osascript", "-", message)
	cmd.Stdin = strings.NewReader(startupAlertScript)
	return cmd
}

func runStartupAlert(message string) {
	ctx, cancel := context.WithTimeout(context.Background(), startupAlertTimeout)
	defer cancel()
	// Best effort: without a GUI session osascript fails at once, and the
	// caller logs the message either way.
	_ = startupAlertCommand(ctx, message).Run()
}
