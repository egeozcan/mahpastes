// Package buildinfo holds the version the binaries were built as. Release and
// master builds stamp it with
//
//	-ldflags "-X go-clipboard/internal/buildinfo.Version=<v> -X go-clipboard/internal/buildinfo.Commit=<sha>"
//
// (see the Makefile's LDFLAGS and scripts/version.sh). Anything built without
// those flags, such as `go run`, `go test` or `wails dev`, reports "dev".
package buildinfo

// Version is the release the binary was built from, without a leading "v"
// ("1.5.0", or "1.5.0-12-gabc1234" between releases), or "dev".
var Version = "dev"

// Commit is the git commit the binary was built from, or "".
var Commit = ""
