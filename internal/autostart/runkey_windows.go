//go:build windows

package autostart

import (
	"errors"

	"golang.org/x/sys/windows/registry"
)

const (
	runKeyPath             = `Software\Microsoft\Windows\CurrentVersion\Run`
	startupApprovedRunPath = `Software\Microsoft\Windows\CurrentVersion\Explorer\StartupApproved\Run`
)

// runKeyBackend is a per-user Run key value: Explorer starts it at login. No
// administrator rights are needed and nothing is installed.
type runKeyBackend struct{}

func newRunKeyBackend() backend { return runKeyBackend{} }

func (runKeyBackend) location() string {
	return `HKEY_CURRENT_USER\` + runKeyPath + `\` + runKeyValueName
}

func (runKeyBackend) read() (Entry, bool, bool, error) {
	k, err := registry.OpenKey(registry.CURRENT_USER, runKeyPath, registry.QUERY_VALUE)
	if errors.Is(err, registry.ErrNotExist) {
		return Entry{}, false, false, nil
	}
	if err != nil {
		return Entry{}, false, false, err
	}
	defer k.Close()
	val, _, err := k.GetStringValue(runKeyValueName)
	if errors.Is(err, registry.ErrNotExist) {
		return Entry{}, false, false, nil
	}
	if err != nil {
		return Entry{}, false, false, err
	}
	return parseRunKeyCommand(val), true, startupApprovedOff(), nil
}

// startupApprovedOff reads the Startup apps switch for the value. Missing
// means enabled.
func startupApprovedOff() bool {
	k, err := registry.OpenKey(registry.CURRENT_USER, startupApprovedRunPath, registry.QUERY_VALUE)
	if err != nil {
		return false
	}
	defer k.Close()
	data, _, err := k.GetBinaryValue(runKeyValueName)
	if err != nil {
		return false
	}
	return startupApprovedDisabled(data)
}

func (runKeyBackend) write(e Entry) error {
	k, _, err := registry.CreateKey(registry.CURRENT_USER, runKeyPath, registry.SET_VALUE)
	if err != nil {
		return err
	}
	defer k.Close()
	if err := k.SetStringValue(runKeyValueName, runKeyCommand(e)); err != nil {
		return err
	}
	// Turning it on here also undoes an earlier "Disabled" in Startup apps:
	// without a StartupApproved value Windows treats the entry as enabled.
	clearStartupApproved()
	return nil
}

func (runKeyBackend) remove() error {
	k, err := registry.OpenKey(registry.CURRENT_USER, runKeyPath, registry.SET_VALUE)
	if errors.Is(err, registry.ErrNotExist) {
		return nil
	}
	if err != nil {
		return err
	}
	defer k.Close()
	err = k.DeleteValue(runKeyValueName)
	if err != nil && !errors.Is(err, registry.ErrNotExist) {
		return err
	}
	clearStartupApproved()
	return nil
}

func clearStartupApproved() {
	k, err := registry.OpenKey(registry.CURRENT_USER, startupApprovedRunPath, registry.SET_VALUE)
	if err != nil {
		return
	}
	defer k.Close()
	_ = k.DeleteValue(runKeyValueName)
}
