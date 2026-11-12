package autostart

import (
	"bytes"
	"encoding/xml"
	"errors"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
)

// launchAgentBackend is a per-user launchd agent. It runs the app's own
// executable (inside the .app bundle) with RunAtLoad, so launchd starts it
// once at login. AssociatedBundleIdentifiers makes macOS 13+ list it under
// mahpastes in System Settings > General > Login Items.
//
// The file is never loaded with launchctl when written: launchd would run it
// right away and start a second copy. Removing it does not unload a job
// launchd already loaded at this login either: that job has run and, without
// KeepAlive, is never run again; unloading it could stop the running app.
type launchAgentBackend struct {
	home string
}

func (b launchAgentBackend) path() string {
	return filepath.Join(b.home, "Library", "LaunchAgents", LaunchAgentLabel+".plist")
}

func (b launchAgentBackend) location() string { return b.path() }

func (b launchAgentBackend) read() (Entry, bool, bool, error) {
	data, err := os.ReadFile(b.path())
	if errors.Is(err, fs.ErrNotExist) {
		return Entry{}, false, false, nil
	}
	if err != nil {
		return Entry{}, false, false, err
	}
	entry, disabled, err := parseLaunchAgent(data)
	if err != nil {
		// Present but unreadable: report it as an entry with no target, so
		// the user sees it and can turn it off (which removes it) or on
		// again (which rewrites it).
		return Entry{}, true, false, nil
	}
	return entry, true, disabled, nil
}

func (b launchAgentBackend) write(e Entry) error {
	return writeFileAtomic(b.path(), launchAgentPlist(e), 0o644)
}

func (b launchAgentBackend) remove() error {
	err := os.Remove(b.path())
	if errors.Is(err, fs.ErrNotExist) {
		return nil
	}
	return err
}

// launchAgentPlist renders the agent's property list.
func launchAgentPlist(e Entry) []byte {
	args := []string{e.Executable}
	if e.Hidden {
		args = append(args, HiddenFlag)
	}
	var b bytes.Buffer
	b.WriteString(`<?xml version="1.0" encoding="UTF-8"?>
<!DOCTYPE plist PUBLIC "-//Apple//DTD PLIST 1.0//EN" "http://www.apple.com/DTDs/PropertyList-1.0.dtd">
<plist version="1.0">
<dict>
	<key>Label</key>
	<string>`)
	xmlText(&b, LaunchAgentLabel)
	b.WriteString(`</string>
	<key>ProgramArguments</key>
	<array>
`)
	for _, a := range args {
		b.WriteString("\t\t<string>")
		xmlText(&b, a)
		b.WriteString("</string>\n")
	}
	b.WriteString(`	</array>
	<key>RunAtLoad</key>
	<true/>
	<key>LimitLoadToSessionType</key>
	<string>Aqua</string>
	<key>ProcessType</key>
	<string>Interactive</string>
	<key>AssociatedBundleIdentifiers</key>
	<array>
		<string>`)
	xmlText(&b, BundleID)
	b.WriteString(`</string>
	</array>
</dict>
</plist>
`)
	return b.Bytes()
}

func xmlText(b *bytes.Buffer, s string) {
	_ = xml.EscapeText(b, []byte(s))
}

// parseLaunchAgent reads ProgramArguments (and the legacy Disabled key) from
// the top-level dict of a launchd property list.
func parseLaunchAgent(data []byte) (entry Entry, disabled bool, err error) {
	dec := xml.NewDecoder(bytes.NewReader(data))
	depth := 0      // element depth
	dictDepth := -1 // depth of the top-level <dict>
	lastKey := ""
	var args []string
	inArgs := false
	sawArgs := false
	for {
		tok, terr := dec.Token()
		if terr == io.EOF {
			break
		}
		if terr != nil {
			return Entry{}, false, terr
		}
		switch t := tok.(type) {
		case xml.StartElement:
			depth++
			name := t.Name.Local
			if name == "dict" && dictDepth < 0 {
				dictDepth = depth
				continue
			}
			if dictDepth < 0 {
				continue
			}
			switch {
			case depth == dictDepth+1 && name == "key":
				var k string
				if err := dec.DecodeElement(&k, &t); err != nil {
					return Entry{}, false, err
				}
				depth--
				lastKey = strings.TrimSpace(k)
				continue
			case depth == dictDepth+1 && lastKey == "ProgramArguments" && name == "array":
				inArgs, sawArgs = true, true
			case depth == dictDepth+2 && inArgs && name == "string":
				var s string
				if err := dec.DecodeElement(&s, &t); err != nil {
					return Entry{}, false, err
				}
				depth--
				args = append(args, s)
				continue
			case depth == dictDepth+1 && lastKey == "Disabled" && name == "true":
				disabled = true
			}
			if depth == dictDepth+1 {
				lastKey = ""
			}
		case xml.EndElement:
			if inArgs && t.Name.Local == "array" && depth == dictDepth+1 {
				inArgs = false
			}
			depth--
		}
	}
	if !sawArgs {
		return Entry{}, disabled, errors.New("no ProgramArguments")
	}
	if len(args) > 0 {
		entry.Executable = args[0]
	}
	for _, a := range args[1:] {
		if a == HiddenFlag {
			entry.Hidden = true
		}
	}
	return entry, disabled, nil
}
