package packaging

import (
	"os"
	"regexp"
	"strings"
	"testing"

	"go-clipboard/internal/autostart"
)

func readCask(t *testing.T) string {
	t.Helper()
	raw, err := os.ReadFile("homebrew/mahpastes.rb")
	if err != nil {
		t.Fatal(err)
	}
	return string(raw)
}

// stanza returns the cask text from the line starting with name up to the
// next blank line.
func stanza(t *testing.T, cask, name string) string {
	t.Helper()
	i := strings.Index(cask, "\n  "+name+" ")
	if i < 0 {
		t.Fatalf("cask has no %s stanza", name)
	}
	rest := cask[i+1:]
	if j := strings.Index(rest, "\n\n"); j >= 0 {
		rest = rest[:j]
	}
	return rest
}

// Homebrew deprecated the string comparison form (`">= :ventura"`): it warns
// on every load and will stop loading once the deprecation is disabled.
func TestCaskMacOSDependencyIsNotTheDeprecatedStringForm(t *testing.T) {
	cask := readCask(t)
	if regexp.MustCompile(`depends_on\s+macos:\s*["']`).MatchString(cask) {
		t.Error(`depends_on macos: uses a string comparison; write a bare symbol (":ventura" means that release or newer)`)
	}
	if !regexp.MustCompile(`(?m)^\s*depends_on macos: :ventura$`).MatchString(cask) {
		t.Error("cask no longer requires macOS Ventura or newer")
	}
}

// zap used to leave the Launch at login agent, the File Provider containers
// and the `mp clip open` cache behind.
func TestCaskZapCoversEverythingTheAppWrites(t *testing.T) {
	zap := stanza(t, readCask(t), "zap")
	id := autostart.BundleID
	for _, path := range []string{
		"~/Library/LaunchAgents/" + autostart.LaunchAgentLabel + ".plist",
		"~/Library/Application Support/mahpastes",
		"~/Library/Caches/mahpastes",
		"~/Library/Caches/" + id,
		"~/Library/Containers/" + id + ".FileProvider",
		"~/Library/Application Scripts/" + id + ".FileProvider",
		"~/Library/Application Support/FileProvider/" + id + ".FileProvider",
		// Prefixed with the signing team identifier.
		"~/Library/Group Containers/*." + id,
		"~/Library/Application Scripts/*." + id,
	} {
		if !strings.Contains(zap, `"`+path+`"`) {
			t.Errorf("zap does not remove %s", path)
		}
	}
}

// Homebrew runs the uninstall stanza (all but quit/signal) on upgrade and
// reinstall as well, so removing the login agent there would switch Launch at
// login off with every update. It must not be unloaded with launchctl either
// (see internal/autostart/launchagent.go).
func TestCaskUninstallOnlyQuits(t *testing.T) {
	cask := readCask(t)
	uninstall := stanza(t, cask, "uninstall")
	if !strings.Contains(uninstall, `quit: "`+autostart.BundleID+`"`) {
		t.Errorf("uninstall does not quit %s:\n%s", autostart.BundleID, uninstall)
	}
	for _, directive := range []string{"delete:", "trash:", "launchctl:", "rmdir:"} {
		if strings.Contains(uninstall, directive) {
			t.Errorf("uninstall has %s, which also runs on every upgrade", directive)
		}
	}
	if !strings.Contains(stanza(t, cask, "caveats"), "Launch at login") {
		t.Error("caveats no longer say that a plain uninstall keeps the login item")
	}
}
