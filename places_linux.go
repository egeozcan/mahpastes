//go:build linux

package main

import (
	"bytes"
	"encoding/xml"
	"errors"
	"os"
	"path/filepath"
	"regexp"
	"strings"
)

// File-manager sidebar entries for the FUSE mount. GTK choosers, Nautilus,
// Thunar and Nemo read gtk-3.0/bookmarks; Dolphin and KDE file dialogs read
// user-places.xbel. Both files belong to the user, so edits are line/element
// level, idempotent, validated, and only entries this app added are removed.

const placeTitle = "Mahpastes"

func gtkBookmarksPath() string {
	if dir := os.Getenv("XDG_CONFIG_HOME"); filepath.IsAbs(dir) {
		return filepath.Join(dir, "gtk-3.0", "bookmarks")
	}
	if home, err := os.UserHomeDir(); err == nil {
		return filepath.Join(home, ".config", "gtk-3.0", "bookmarks")
	}
	return ""
}

func kdePlacesPath() string {
	if dir := os.Getenv("XDG_DATA_HOME"); filepath.IsAbs(dir) {
		return filepath.Join(dir, "user-places.xbel")
	}
	if home, err := os.UserHomeDir(); err == nil {
		return filepath.Join(home, ".local", "share", "user-places.xbel")
	}
	return ""
}

func gtkBookmarkLine(mountPath string) string { return fileURLFromAbsPath(mountPath) }

// addGTKBookmark appends the mount to the bookmarks file (creating it) unless
// an entry for the same location exists. added reports whether this call wrote
// the entry.
func addGTKBookmark(file, mountPath string) (added bool, err error) {
	if file == "" {
		return false, nil
	}
	url := gtkBookmarkLine(mountPath)
	data, err := os.ReadFile(file)
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		return false, err
	}
	if hasGTKBookmark(data, url) {
		return false, nil
	}
	if err := os.MkdirAll(filepath.Dir(file), 0o755); err != nil {
		return false, err
	}
	out := append([]byte(nil), data...)
	if len(out) > 0 && out[len(out)-1] != '\n' {
		out = append(out, '\n')
	}
	out = append(out, []byte(url+" "+placeTitle+"\n")...)
	return true, writeUserFile(file, out)
}

func hasGTKBookmark(data []byte, url string) bool {
	for _, line := range strings.Split(string(data), "\n") {
		f := strings.Fields(line)
		if len(f) > 0 && strings.TrimRight(f[0], "/") == strings.TrimRight(url, "/") {
			return true
		}
	}
	return false
}

// removeGTKBookmark drops the lines for the mount, leaving everything else
// byte for byte. A missing file is not an error.
func removeGTKBookmark(file, mountPath string) error {
	if file == "" {
		return nil
	}
	data, err := os.ReadFile(file)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		return err
	}
	url := strings.TrimRight(gtkBookmarkLine(mountPath), "/")
	lines := strings.SplitAfter(string(data), "\n")
	var kept []string
	changed := false
	for _, line := range lines {
		f := strings.Fields(line)
		if len(f) > 0 && strings.TrimRight(f[0], "/") == url {
			changed = true
			continue
		}
		kept = append(kept, line)
	}
	if !changed {
		return nil
	}
	return writeUserFile(file, []byte(strings.Join(kept, "")))
}

func xbelBookmarkRe(href string) *regexp.Regexp {
	return regexp.MustCompile(`(?s)[ \t]*<bookmark href="` + regexp.QuoteMeta(xmlEscape(href)) + `"[^>]*>.*?</bookmark>[ \t]*\n?`)
}

func xmlEscape(s string) string {
	var b bytes.Buffer
	_ = xml.EscapeText(&b, []byte(s))
	return strings.ReplaceAll(b.String(), "&#34;", "&quot;")
}

func xbelHasHref(data []byte, href string) (bool, error) {
	d := xml.NewDecoder(bytes.NewReader(data))
	want := strings.TrimRight(href, "/")
	found := false
	for {
		tok, err := d.Token()
		if err != nil {
			if err.Error() == "EOF" {
				return found, nil
			}
			return false, err
		}
		if se, ok := tok.(xml.StartElement); ok && se.Name.Local == "bookmark" {
			for _, a := range se.Attr {
				if a.Name.Local == "href" && strings.TrimRight(a.Value, "/") == want {
					found = true
				}
			}
		}
	}
}

// addKDEPlace inserts a Places entry before </xbel>. It never creates the
// file (KDE builds its default places itself) and refuses to touch one that
// does not parse before or after the edit.
func addKDEPlace(file, mountPath string) (added bool, err error) {
	if file == "" {
		return false, nil
	}
	data, err := os.ReadFile(file)
	if errors.Is(err, os.ErrNotExist) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	href := fileURLFromAbsPath(mountPath)
	has, err := xbelHasHref(data, href)
	if err != nil {
		return false, err
	}
	if has {
		return false, nil
	}
	idx := bytes.LastIndex(data, []byte("</xbel>"))
	if idx < 0 {
		return false, errors.New("user-places.xbel has no closing xbel element")
	}
	entry := `<bookmark href="` + xmlEscape(href) + `">
  <title>` + placeTitle + `</title>
  <info>
   <metadata owner="http://freedesktop.org">
    <bookmark:icon name="folder-activities"/>
   </metadata>
   <metadata owner="http://www.kde.org">
    <ID>mahpastes-fuse-mount</ID>
    <isSystemItem>false</isSystemItem>
   </metadata>
  </info>
 </bookmark>
`
	out := append([]byte(nil), data[:idx]...)
	if len(out) > 0 && out[len(out)-1] != '\n' {
		out = append(out, '\n')
	}
	out = append(out, entry...)
	out = append(out, data[idx:]...)
	if _, err := xbelHasHref(out, href); err != nil {
		return false, err
	}
	return true, writeUserFile(file, out)
}

func removeKDEPlace(file, mountPath string) error {
	if file == "" {
		return nil
	}
	data, err := os.ReadFile(file)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		return err
	}
	if _, err := xbelHasHref(data, ""); err != nil {
		return err
	}
	re := xbelBookmarkRe(fileURLFromAbsPath(mountPath))
	out := re.ReplaceAll(data, nil)
	if bytes.Equal(out, data) {
		return nil
	}
	if _, err := xbelHasHref(out, ""); err != nil {
		return err
	}
	return writeUserFile(file, out)
}

// writeUserFile replaces file atomically, keeping its permissions.
func writeUserFile(path string, b []byte) error {
	mode := os.FileMode(0o644)
	if st, err := os.Stat(path); err == nil {
		mode = st.Mode().Perm()
	}
	f, err := os.CreateTemp(filepath.Dir(path), ".mahpastes-*.tmp")
	if err != nil {
		return err
	}
	defer os.Remove(f.Name())
	if err = f.Chmod(mode); err == nil {
		_, err = f.Write(b)
	}
	if err == nil {
		err = f.Sync()
	}
	if cerr := f.Close(); err == nil {
		err = cerr
	}
	if err != nil {
		return err
	}
	return os.Rename(f.Name(), path)
}

// surfaceMount registers the mount with the file managers and reports which
// entries this call created.
func surfaceMount(mountPath string) (gtk, kde bool, err error) {
	var errs []error
	gtk, e := addGTKBookmark(gtkBookmarksPath(), mountPath)
	errs = append(errs, e)
	kde, e = addKDEPlace(kdePlacesPath(), mountPath)
	errs = append(errs, e)
	return gtk, kde, errors.Join(errs...)
}

func unsurfaceMount(mountPath string, gtk, kde bool) error {
	var errs []error
	if gtk {
		errs = append(errs, removeGTKBookmark(gtkBookmarksPath(), mountPath))
	}
	if kde {
		errs = append(errs, removeKDEPlace(kdePlacesPath(), mountPath))
	}
	return errors.Join(errs...)
}
