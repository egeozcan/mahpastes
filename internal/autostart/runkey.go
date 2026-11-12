package autostart

import "strings"

// runKeyValueName is the value under HKCU\...\CurrentVersion\Run (and the
// matching Explorer\StartupApproved\Run value Windows keeps for the Startup
// apps switch).
const runKeyValueName = "mahpastes"

// runKeyCommand renders the Run value: the executable in double quotes (a
// path with spaces must be quoted, and Windows paths cannot contain `"`),
// then the flag.
func runKeyCommand(e Entry) string {
	cmd := `"` + e.Executable + `"`
	if e.Hidden {
		cmd += " " + HiddenFlag
	}
	return cmd
}

// parseRunKeyCommand reads a Run value back. An unquoted value is taken to
// end its executable at the first space, as CreateProcess would for a path
// without spaces.
func parseRunKeyCommand(cmd string) Entry {
	cmd = strings.TrimSpace(cmd)
	var exe, rest string
	if strings.HasPrefix(cmd, `"`) {
		end := strings.Index(cmd[1:], `"`)
		if end < 0 {
			return Entry{Executable: cmd[1:]}
		}
		exe, rest = cmd[1:1+end], cmd[2+end:]
	} else {
		exe, rest, _ = strings.Cut(cmd, " ")
	}
	e := Entry{Executable: exe}
	for _, a := range strings.Fields(rest) {
		if a == HiddenFlag {
			e.Hidden = true
		}
	}
	return e
}

// startupApprovedDisabled reports whether a StartupApproved\Run value marks
// the entry as switched off in Settings > Apps > Startup or Task Manager. The
// first byte is 2 (or 6) while enabled and 3 (or 7) once disabled: the low bit
// is the "disabled" flag.
func startupApprovedDisabled(data []byte) bool {
	return len(data) > 0 && data[0]&1 == 1
}
