//go:build !windows && !darwin

package main

// showStartupError has nothing extra to do here: the message is logged to
// stderr by the caller, where a terminal launch shows it.
func showStartupError(string) {}
