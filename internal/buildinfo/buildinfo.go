// Package buildinfo holds the version stamped into mahpastes binaries (the
// desktop app, mahpastesd and mp).
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
// checkout (or a pseudo-version), else "dev".
func Current() string {
	v, _ := Resolve(Version, Commit, readBuildInfo())
	return v
}

// CurrentCommit returns the stamped Commit, else (for an unstamped "dev"
// build) the VCS revision the Go toolchain recorded, shortened to 12
// characters, else "".
func CurrentCommit() string {
	_, c := Resolve(Version, Commit, readBuildInfo())
	return c
}

func readBuildInfo() *debug.BuildInfo {
	bi, ok := debug.ReadBuildInfo()
	if !ok {
		return nil
	}
	return bi
}

// Resolve picks the version and commit to report from the -ldflags stamps
// and the toolchain's build info. The commit falls back to the VCS revision
// only when the version itself carries none ("dev").
func Resolve(stamped, stampedCommit string, bi *debug.BuildInfo) (version, commit string) {
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
