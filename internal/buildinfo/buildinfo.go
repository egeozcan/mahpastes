// Package buildinfo holds build-time version information, stamped with
// -ldflags "-X go-clipboard/internal/buildinfo.Version=<v> -X go-clipboard/internal/buildinfo.Commit=<sha>".
package buildinfo

// Version is the release version; "dev" for unstamped builds.
var Version = "dev"

// Commit is the source commit the binary was built from, if stamped.
var Commit = ""
