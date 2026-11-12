package client

import (
	"runtime/debug"

	"go-clipboard/internal/buildinfo"
)

// Version is the mp version, shown by `mp --version` and sent in the
// User-Agent. An -ldflags stamp of buildinfo.Version wins; an unstamped build
// falls back to the module version the Go toolchain records from VCS (a tag or
// a pseudo-version on a plain `go build` in a checkout), then to "dev".
var Version, Commit = resolveVersion(buildinfo.Version, buildinfo.Commit, readBuildInfo())

func readBuildInfo() *debug.BuildInfo {
	bi, ok := debug.ReadBuildInfo()
	if !ok {
		return nil
	}
	return bi
}

// resolveVersion picks the version and commit to report from the -ldflags
// stamps and the toolchain's build info. The commit falls back to the VCS
// revision only when the version itself carries none ("dev").
func resolveVersion(stamped, stampedCommit string, bi *debug.BuildInfo) (version, commit string) {
	version, commit = stamped, stampedCommit
	if version == "" || version == "dev" {
		version = "dev"
		if bi != nil && bi.Main.Version != "" && bi.Main.Version != "(devel)" {
			version = bi.Main.Version
		}
	}
	if commit == "" && version == "dev" && bi != nil {
		for _, s := range bi.Settings {
			if s.Key == "vcs.revision" && s.Value != "" {
				commit = s.Value
				if len(commit) > 12 {
					commit = commit[:12]
				}
			}
		}
	}
	return version, commit
}
