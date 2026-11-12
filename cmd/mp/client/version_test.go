package client

import (
	"runtime/debug"
	"testing"
)

func TestResolveVersion(t *testing.T) {
	vcs := []debug.BuildSetting{{Key: "vcs.revision", Value: "d68ff3a2acc687216cc7c4b75f002010669ff393"}}
	cases := []struct {
		name                 string
		stamped, stampedSHA  string
		bi                   *debug.BuildInfo
		wantVersion, wantSHA string
	}{
		{"ldflags stamp wins", "v1.6.0", "abc123", &debug.BuildInfo{Main: debug.Module{Version: "v9.9.9"}, Settings: vcs}, "v1.6.0", "abc123"},
		{"module version from VCS", "dev", "", &debug.BuildInfo{Main: debug.Module{Version: "v1.5.1-0.20261112044129-d68ff3a2acc6"}, Settings: vcs}, "v1.5.1-0.20261112044129-d68ff3a2acc6", ""},
		{"go run reports (devel)", "dev", "", &debug.BuildInfo{Main: debug.Module{Version: "(devel)"}, Settings: vcs}, "dev", "d68ff3a2acc6"},
		{"no build info", "dev", "", nil, "dev", ""},
		{"empty stamp", "", "", &debug.BuildInfo{}, "dev", ""},
	}
	for _, c := range cases {
		v, sha := resolveVersion(c.stamped, c.stampedSHA, c.bi)
		if v != c.wantVersion || sha != c.wantSHA {
			t.Errorf("%s: got (%q, %q), want (%q, %q)", c.name, v, sha, c.wantVersion, c.wantSHA)
		}
	}
}
