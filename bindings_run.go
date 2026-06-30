//go:build !bindings

package main

// generatingBindings is false for every normal build (dev, production, plain
// `go build`); the binary launches the GUI and must hold the instance lock.
const generatingBindings = false
