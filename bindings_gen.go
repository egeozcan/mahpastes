//go:build bindings

package main

// generatingBindings is true only when this binary is compiled by Wails with
// `-tags bindings` to introspect the bound methods. In that mode wails.Run
// dumps the binding definitions and exits without launching the GUI, so main()
// must not contend for the single-instance data-dir lock — doing so makes
// binding generation (and therefore `wails build`/`wails dev`) fail whenever a
// real instance is already running.
const generatingBindings = true
