//go:build linux

package main

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/xml"
	"errors"
	"fmt"
	"io"
	"net/url"
	"os"
	"path/filepath"
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

// sameFileURL compares two file URLs by their decoded path, so the escaping
// another program chose (GLib leaves ' ( ) unescaped, Go does not) or a
// trailing slash does not hide an entry for the same folder.
func sameFileURL(a, b string) bool {
	norm := func(s string) string {
		u, err := url.Parse(s)
		if err != nil || u.Scheme != "file" {
			return strings.TrimRight(s, "/")
		}
		return u.Host + "|" + strings.TrimRight(u.Path, "/")
	}
	return norm(a) == norm(b)
}

// gtkOwnLine is the exact line addGTKBookmark writes. Nautilus's own "Add
// Bookmark" writes the URL without a label, so removal can tell them apart.
func gtkOwnLine(mountPath string) string {
	return fileURLFromAbsPath(mountPath) + " " + placeTitle
}

// addGTKBookmark appends the mount to the bookmarks file (creating it) unless
// an entry for the same location exists. added reports whether this call wrote
// the entry.
func addGTKBookmark(file, mountPath string) (added bool, err error) {
	if file == "" {
		return false, nil
	}
	data, err := os.ReadFile(file)
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		return false, err
	}
	if hasGTKBookmark(data, fileURLFromAbsPath(mountPath)) {
		return false, nil
	}
	if err := os.MkdirAll(filepath.Dir(file), 0o755); err != nil {
		return false, err
	}
	out := append([]byte(nil), data...)
	if len(out) > 0 && out[len(out)-1] != '\n' {
		out = append(out, '\n')
	}
	out = append(out, []byte(gtkOwnLine(mountPath)+"\n")...)
	if err := writeUserFile(file, out); err != nil {
		return false, err
	}
	return true, nil
}

func hasGTKBookmark(data []byte, url string) bool {
	for _, line := range strings.Split(string(data), "\n") {
		if f := strings.Fields(line); len(f) > 0 && sameFileURL(f[0], url) {
			return true
		}
	}
	return false
}

// removeGTKBookmark drops the line this app wrote for the mount, leaving
// everything else (including a user's own bookmark of the same folder) byte
// for byte. A missing file is not an error.
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
	own := gtkOwnLine(mountPath)
	var kept []string
	changed := false
	for _, line := range strings.SplitAfter(string(data), "\n") {
		if strings.TrimRight(line, "\r\n") == own {
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

func xmlEscape(s string) string {
	var b bytes.Buffer
	_ = xml.EscapeText(&b, []byte(s))
	return strings.ReplaceAll(b.String(), "&#34;", "&quot;")
}

// kdePlaceID is the kde.org <ID> of the app's place. KFilePlacesModel keys
// items by ID, so it is unique per mount (two profiles never collide), and it
// marks the entry as the app's own for removal.
func kdePlaceID(href string) string {
	sum := sha256.Sum256([]byte(href))
	return "mahpastes-" + hex.EncodeToString(sum[:6])
}

// xbelBookmark is a <bookmark> element of user-places.xbel and its byte span.
type xbelBookmark struct {
	start, end int64
	href, id   string
}

// parseXBEL validates the whole document and lists its bookmark elements with
// their exact byte spans, so an edit never relies on how the file is laid out.
func parseXBEL(data []byte) ([]xbelBookmark, error) {
	d := xml.NewDecoder(bytes.NewReader(data))
	var (
		out     []xbelBookmark
		cur     *xbelBookmark
		depth   int // element depth inside the current bookmark
		kdeMeta int // depth of an open kde.org <metadata>, 0 when outside
		inID    bool
		idText  strings.Builder
		sawRoot bool
		open    int
	)
	for {
		start := d.InputOffset()
		tok, err := d.Token()
		if errors.Is(err, io.EOF) {
			if !sawRoot || open != 0 {
				return nil, errors.New("user-places.xbel has no complete xbel element")
			}
			return out, nil
		}
		if err != nil {
			return nil, err
		}
		switch t := tok.(type) {
		case xml.StartElement:
			if !sawRoot {
				if t.Name.Local != "xbel" {
					return nil, fmt.Errorf("user-places.xbel root is <%s>, not <xbel>", t.Name.Local)
				}
				sawRoot = true
			}
			open++
			if cur == nil {
				if t.Name.Local == "bookmark" && t.Name.Space == "" {
					cur = &xbelBookmark{start: start}
					for _, a := range t.Attr {
						if a.Name.Local == "href" && a.Name.Space == "" {
							cur.href = a.Value
						}
					}
					depth = 1
				}
				continue
			}
			depth++
			if t.Name.Local == "metadata" {
				for _, a := range t.Attr {
					if a.Name.Local == "owner" && a.Value == "http://www.kde.org" {
						kdeMeta = depth
					}
				}
			}
			if kdeMeta != 0 && t.Name.Local == "ID" {
				inID = true
				idText.Reset()
			}
		case xml.CharData:
			if inID {
				idText.Write(t)
			}
		case xml.EndElement:
			open--
			if cur == nil {
				continue
			}
			if inID && t.Name.Local == "ID" {
				inID = false
				if cur.id == "" {
					cur.id = strings.TrimSpace(idText.String())
				}
			}
			if depth == kdeMeta {
				kdeMeta = 0
			}
			depth--
			if depth == 0 {
				cur.end = d.InputOffset()
				out = append(out, *cur)
				cur = nil
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
	before, err := parseXBEL(data)
	if err != nil {
		return false, err
	}
	for _, b := range before {
		if sameFileURL(b.href, href) {
			return false, nil
		}
	}
	idx := bytes.LastIndex(data, []byte("</xbel>"))
	if idx < 0 {
		return false, errors.New("user-places.xbel has no closing xbel element")
	}
	entry := ` <bookmark href="` + xmlEscape(href) + `">
  <title>` + placeTitle + `</title>
  <info>
   <metadata owner="http://freedesktop.org">
    <bookmark:icon name="folder-activities"/>
   </metadata>
   <metadata owner="http://www.kde.org">
    <ID>` + kdePlaceID(href) + `</ID>
    <isSystemItem>false</isSystemItem>
   </metadata>
  </info>
 </bookmark>
`
	// The entry starts on its own line and carries its own indentation.
	head := bytes.TrimRight(data[:idx], " \t")
	out := append([]byte(nil), head...)
	if len(out) > 0 && out[len(out)-1] != '\n' {
		out = append(out, '\n')
	}
	out = append(out, entry...)
	out = append(out, data[idx:]...)
	after, err := parseXBEL(out)
	if err != nil {
		return false, err
	}
	if len(after) != len(before)+1 {
		return false, errors.New("user-places.xbel edit did not add exactly one place")
	}
	if err := writeUserFile(file, out); err != nil {
		return false, err
	}
	return true, nil
}

// removeKDEPlace removes the place this app added: the bookmark whose href is
// the mount and whose kde.org ID is the app's. A place the user made for the
// same folder has another ID and stays.
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
	before, err := parseXBEL(data)
	if err != nil {
		return err
	}
	href := fileURLFromAbsPath(mountPath)
	id := kdePlaceID(href)
	out := data
	removed := 0
	// Cut from the end so earlier spans stay valid.
	for i := len(before) - 1; i >= 0; i-- {
		b := before[i]
		if b.id != id || !sameFileURL(b.href, href) {
			continue
		}
		start, end := int(b.start), int(b.end)
		// Take the element's own line: indentation, trailing blanks and one
		// newline.
		ls := start
		for ls > 0 && (out[ls-1] == ' ' || out[ls-1] == '\t') {
			ls--
		}
		if ls == 0 || out[ls-1] == '\n' {
			start = ls
		}
		for end < len(out) && (out[end] == ' ' || out[end] == '\t') {
			end++
		}
		if end < len(out) && out[end] == '\n' {
			end++
		}
		out = append(append([]byte(nil), out[:start]...), out[end:]...)
		removed++
	}
	if removed == 0 {
		return nil
	}
	after, err := parseXBEL(out)
	if err != nil {
		return err
	}
	if len(after) != len(before)-removed {
		return errors.New("user-places.xbel edit removed more than the app's place")
	}
	return writeUserFile(file, out)
}

// writeUserFile replaces file atomically, keeping its permissions. A symlink
// (dotfile managers link these files) is followed, so the link survives and
// its target is updated.
func writeUserFile(path string, b []byte) error {
	if st, err := os.Lstat(path); err == nil && st.Mode()&os.ModeSymlink != 0 {
		real, err := filepath.EvalSymlinks(path)
		if err != nil {
			return fmt.Errorf("%s is a symlink that cannot be followed: %w", path, err)
		}
		path = real
	}
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
	gtk, gerr := addGTKBookmark(gtkBookmarksPath(), mountPath)
	if gerr != nil {
		gerr = fmt.Errorf("GTK bookmarks: %w", gerr)
	}
	kde, kerr := addKDEPlace(kdePlacesPath(), mountPath)
	if kerr != nil {
		kerr = fmt.Errorf("KDE Places: %w", kerr)
	}
	return gtk, kde, errors.Join(gerr, kerr)
}

// unsurfaceMount removes the entries the app added (gtk and kde say which)
// and reports which of them are still in place because removal failed.
func unsurfaceMount(mountPath string, gtk, kde bool) (gtkLeft, kdeLeft bool, err error) {
	var errs []error
	if gtk {
		if e := removeGTKBookmark(gtkBookmarksPath(), mountPath); e != nil {
			gtkLeft = true
			errs = append(errs, fmt.Errorf("GTK bookmarks: %w", e))
		}
	}
	if kde {
		if e := removeKDEPlace(kdePlacesPath(), mountPath); e != nil {
			kdeLeft = true
			errs = append(errs, fmt.Errorf("KDE Places: %w", e))
		}
	}
	return gtkLeft, kdeLeft, errors.Join(errs...)
}
