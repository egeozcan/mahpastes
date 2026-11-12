// Package buildinfo holds the version stamped into mahpastes binaries.
//
// Release and Makefile builds set Version and Commit with
//
//	-ldflags "-X go-clipboard/internal/buildinfo.Version=<v> -X go-clipboard/internal/buildinfo.Commit=<sha>"
//
// (wails build passes the same flags through its -ldflags option). Wails
// builds with -buildvcs=false, so the linker flag is the only source there.
package buildinfo

import "runtime/debug"

// Version is the release version, for example "v1.5.0". "dev" means the
// build was not stamped.
var Version = "dev"

// Commit is the git commit the binary was built from, when known.
var Commit = ""

// Current returns the best available version string: the stamped Version,
// else the module version Go embeds for a plain `go build` in a tagged
// checkout, else "dev".
func Current() string {
	if Version != "" && Version != "dev" {
		return Version
	}
	if info, ok := debug.ReadBuildInfo(); ok {
		if v := info.Main.Version; v != "" && v != "(devel)" {
			return v
		}
	}
	return "dev"
}
