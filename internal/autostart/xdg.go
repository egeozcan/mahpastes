package autostart

import (
	"bufio"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
)

// xdgFileName is the autostart entry's file name. scripts/linux/install.sh
// --uninstall removes the same file.
const xdgFileName = "mahpastes.desktop"

// xdgBackend is an XDG autostart entry (freedesktop Desktop Application
// Autostart Specification), honoured by GNOME, KDE Plasma, Xfce, Cinnamon,
// MATE, LXQt and most other desktops.
type xdgBackend struct {
	configHome string
}

func (b xdgBackend) path() string {
	return filepath.Join(b.configHome, "autostart", xdgFileName)
}

func (b xdgBackend) location() string { return b.path() }

func (b xdgBackend) read() (Entry, bool, bool, error) {
	data, err := os.ReadFile(b.path())
	if errors.Is(err, fs.ErrNotExist) {
		return Entry{}, false, false, nil
	}
	if err != nil {
		return Entry{}, false, false, err
	}
	entry, disabled := parseDesktopEntry(string(data))
	return entry, true, disabled, nil
}

func (b xdgBackend) write(e Entry) error {
	content, err := desktopEntry(e)
	if err != nil {
		return err
	}
	return writeFileAtomic(b.path(), []byte(content), 0o644)
}

func (b xdgBackend) remove() error {
	err := os.Remove(b.path())
	if errors.Is(err, fs.ErrNotExist) {
		return nil
	}
	return err
}

// desktopEntry renders the autostart .desktop file.
func desktopEntry(e Entry) (string, error) {
	execArg, err := desktopExecQuote(e.Executable)
	if err != nil {
		return "", err
	}
	exec := execArg
	if e.Hidden {
		exec += " " + HiddenFlag
	}
	var sb strings.Builder
	sb.WriteString("[Desktop Entry]\n")
	sb.WriteString("Type=Application\n")
	sb.WriteString("Name=mahpastes\n")
	sb.WriteString("Comment=Start mahpastes when you log in\n")
	sb.WriteString("Exec=" + desktopEscapeValue(exec) + "\n")
	sb.WriteString("Icon=mahpastes\n")
	sb.WriteString("Terminal=false\n")
	sb.WriteString("NoDisplay=true\n")
	sb.WriteString("X-GNOME-Autostart-enabled=true\n")
	return sb.String(), nil
}

// desktopExecQuote quotes one Exec argument as the Desktop Entry spec asks:
// inside double quotes, `"`, "`", `$` and `\` are backslash-escaped, and a
// literal `%` is written `%%` so it is not read as a field code.
func desktopExecQuote(arg string) (string, error) {
	for _, r := range arg {
		if r < 0x20 || r == 0x7f {
			return "", fmt.Errorf("path %q contains a control character", arg)
		}
	}
	var sb strings.Builder
	sb.WriteByte('"')
	for _, r := range arg {
		switch r {
		case '"', '`', '$', '\\':
			sb.WriteByte('\\')
			sb.WriteRune(r)
		case '%':
			sb.WriteString("%%")
		default:
			sb.WriteRune(r)
		}
	}
	sb.WriteByte('"')
	return sb.String(), nil
}

// desktopEscapeValue applies the general string-value escaping of a desktop
// file on top of the Exec quoting: a backslash is written `\\`.
func desktopEscapeValue(v string) string {
	return strings.ReplaceAll(v, `\`, `\\`)
}

// desktopUnescapeValue reverses the general value escaping (\s \n \t \r \\).
func desktopUnescapeValue(v string) string {
	var sb strings.Builder
	for i := 0; i < len(v); i++ {
		c := v[i]
		if c != '\\' || i+1 == len(v) {
			sb.WriteByte(c)
			continue
		}
		i++
		switch v[i] {
		case 's':
			sb.WriteByte(' ')
		case 'n':
			sb.WriteByte('\n')
		case 't':
			sb.WriteByte('\t')
		case 'r':
			sb.WriteByte('\r')
		case '\\':
			sb.WriteByte('\\')
		default:
			sb.WriteByte('\\')
			sb.WriteByte(v[i])
		}
	}
	return sb.String()
}

// splitDesktopExec splits an (already value-unescaped) Exec line into
// arguments, honouring double quotes and the escapes allowed inside them, and
// turning `%%` back into `%`.
func splitDesktopExec(exec string) []string {
	var args []string
	var cur strings.Builder
	inArg, inQuote := false, false
	for i := 0; i < len(exec); i++ {
		c := exec[i]
		switch {
		case inQuote && c == '\\' && i+1 < len(exec):
			i++
			cur.WriteByte(exec[i])
		case c == '"':
			inQuote = !inQuote
			inArg = true
		case !inQuote && (c == ' ' || c == '\t'):
			if inArg {
				args = append(args, cur.String())
				cur.Reset()
				inArg = false
			}
		default:
			cur.WriteByte(c)
			inArg = true
		}
	}
	if inArg {
		args = append(args, cur.String())
	}
	for i, a := range args {
		args[i] = strings.ReplaceAll(a, "%%", "%")
	}
	return args
}

// parseDesktopEntry reads the [Desktop Entry] group of an autostart file.
// disabled is true when the desktop switched the entry off.
func parseDesktopEntry(content string) (entry Entry, disabled bool) {
	sc := bufio.NewScanner(strings.NewReader(content))
	inGroup := false
	for sc.Scan() {
		line := strings.TrimSpace(sc.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		if strings.HasPrefix(line, "[") {
			inGroup = line == "[Desktop Entry]"
			continue
		}
		if !inGroup {
			continue
		}
		key, value, ok := strings.Cut(line, "=")
		if !ok {
			continue
		}
		key, value = strings.TrimSpace(key), strings.TrimSpace(value)
		switch key {
		case "Exec":
			args := splitDesktopExec(desktopUnescapeValue(value))
			entry = Entry{}
			if len(args) > 0 {
				entry.Executable = args[0]
			}
			for _, a := range args[1:] {
				if a == HiddenFlag {
					entry.Hidden = true
				}
			}
		case "Hidden":
			if value == "true" {
				disabled = true
			}
		case "X-GNOME-Autostart-enabled":
			if value == "false" {
				disabled = true
			}
		}
	}
	return entry, disabled
}

// writeFileAtomic replaces path through a temp file in the same directory.
func writeFileAtomic(path string, data []byte, perm os.FileMode) error {
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	tmp, err := os.CreateTemp(dir, "."+filepath.Base(path)+".*.tmp")
	if err != nil {
		return err
	}
	name := tmp.Name()
	defer os.Remove(name)
	if _, err := tmp.Write(data); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Chmod(perm); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	return os.Rename(name, path)
}
