package main

import (
	"context"
	"errors"
	"fmt"
	"os"
	"time"

	coreapp "go-clipboard/internal/app"
	"go-clipboard/internal/openrequest"
)

// handoffTimeout bounds how long a second launch keeps trying to reach the
// running instance. Long enough to cover a first instance that won the lock a
// moment ago and has not opened its socket yet (a double-click starts two
// processes almost together), or one that is shutting down and about to free
// the lock; short enough that a launch against a data dir held by mahpastesd,
// which never listens, fails visibly soon.
var handoffTimeout = 3 * time.Second

// handoffAttempt is how long one round of connection attempts lasts before the
// lock is tried again. An instance that is shutting down closes its socket
// first and releases the lock last, so the launch must notice the free lock
// rather than wait on a socket that will not come back.
var handoffAttempt = 500 * time.Millisecond

// launchRequest turns this process's command line into an open request. The
// bindings build and the worker self-test never treat arguments as files.
func launchRequest(selfTest bool) openrequest.Request {
	if generatingBindings || selfTest {
		return openrequest.Request{Activate: true}
	}
	cwd, _ := os.Getwd()
	return openrequest.FromArgs(os.Args[1:], cwd)
}

// handOffToRunningInstance sends req to the desktop instance that holds the
// data directory's lock, which raises its window and imports the files. It
// returns nil only when that instance acknowledged the request, so the caller
// can exit quietly.
func handOffToRunningInstance(dataDir string, lockErr *coreapp.InstanceLockError, req openrequest.Request, timeout time.Duration) error {
	// Windows only lets the foreground process hand the foreground on. This
	// process was just launched by the user and may do so; the running
	// instance, a background process, may not take it by itself.
	allowSetForegroundWindow(lockErr.HolderPID)

	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()
	return openrequest.Send(ctx, dataDir, req)
}

// acquireInstanceOrHandOff takes the data directory's instance lock. When
// another process holds it, the launch request is forwarded to that process
// and handedOff is true: the caller exits successfully without starting a
// second window. If the holder goes away meanwhile (it was shutting down),
// the lock is taken after all and this launch starts normally. Any other
// outcome without a lock is returned as an error.
func acquireInstanceOrHandOff(dataDir string, req openrequest.Request, allowHandoff bool) (lock *coreapp.InstanceLock, handedOff bool, err error) {
	deadline := time.Now().Add(handoffTimeout)
	for {
		lock, err = coreapp.AcquireInstanceLock(dataDir)
		if err == nil {
			return lock, false, nil
		}
		var lockErr *coreapp.InstanceLockError
		if !allowHandoff || !errors.As(err, &lockErr) {
			return nil, false, err
		}
		attempt := time.Until(deadline)
		if attempt > handoffAttempt {
			attempt = handoffAttempt
		}
		herr := handOffToRunningInstance(dataDir, lockErr, req, attempt)
		if herr == nil {
			return nil, true, nil
		}
		if errors.Is(herr, openrequest.ErrRefused) || !time.Now().Before(deadline) {
			// The holder is not a desktop window that accepts requests
			// (mahpastesd, an older build, a hung one), or refused this one:
			// report the lock error as before, with the reason the handoff
			// failed for the log.
			return nil, false, fmt.Errorf("%w (could not hand off to it: %w)", err, herr)
		}
		// Don't spin when an attempt failed fast (the holder dropped the
		// connection while exiting).
		time.Sleep(50 * time.Millisecond)
	}
}
