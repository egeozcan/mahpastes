package client

import "go-clipboard/internal/buildinfo"

// Version is the mp version, shown by `mp --version` and sent in the
// User-Agent. An -ldflags stamp of buildinfo.Version wins; an unstamped build
// falls back to the module version the Go toolchain records from VCS (a tag or
// a pseudo-version on a plain `go build` in a checkout), then to "dev". See
// buildinfo.Resolve.
var Version, Commit = buildinfo.Current(), buildinfo.CurrentCommit()
