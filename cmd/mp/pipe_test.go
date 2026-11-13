package main

import (
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

// withStdin points os.Stdin at a regular file holding data (not a terminal).
func withStdin(t *testing.T, data string) {
	t.Helper()
	f, err := os.CreateTemp(t.TempDir(), "stdin")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := f.WriteString(data); err != nil {
		t.Fatal(err)
	}
	if _, err := f.Seek(0, io.SeekStart); err != nil {
		t.Fatal(err)
	}
	old := os.Stdin
	os.Stdin = f
	t.Cleanup(func() { os.Stdin = old; f.Close() })
}

// captureStdout runs fn and returns what it wrote to os.Stdout.
func captureStdout(t *testing.T, fn func() error) (string, error) {
	t.Helper()
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	old := os.Stdout
	os.Stdout = w
	out := make(chan string)
	go func() {
		b, _ := io.ReadAll(r)
		out <- string(b)
	}()
	runErr := fn()
	os.Stdout = old
	w.Close()
	return <-out, runErr
}

// uploadServer accepts clip uploads, recording each part's filename, and
// answers with sequential IDs.
func uploadServer(t *testing.T, names *[]string, posts *int) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost || r.URL.Path != "/api/v1/clips" {
			http.NotFound(w, r)
			return
		}
		*posts++
		mr, err := r.MultipartReader()
		if err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		name := ""
		for {
			p, err := mr.NextPart()
			if err != nil {
				break
			}
			if p.FileName() != "" {
				name = p.FileName()
			}
			io.Copy(io.Discard, p)
		}
		*names = append(*names, name)
		w.WriteHeader(http.StatusCreated)
		fmt.Fprintf(w, `{"id":%d,"filename":%q}`, len(*names), name)
	}))
	t.Cleanup(srv.Close)
	t.Setenv("MP_API_URL", srv.URL)
	t.Setenv("MP_API_KEY", "k")
	return srv
}

func resetUploadFlags() {
	clipUploadQuiet, clipUploadFilename, clipUploadContentType, clipUploadTag, clipUploadExpire = false, "", "", "", ""
	jsonOutput = false
}

func TestUploadStdinDefaultNameAndQuiet(t *testing.T) {
	defer resetUploadFlags()
	cases := []struct {
		name, in string
		args     []string
		ext      string
	}{
		{"png no args", "\x89PNG\r\n\x1a\n" + strings.Repeat("x", 100), nil, ".png"},
		{"text via dash", "hello world\n", []string{"-"}, ".txt"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			var names []string
			posts := 0
			uploadServer(t, &names, &posts)
			withStdin(t, tc.in)
			clipUploadQuiet = true
			out, err := captureStdout(t, func() error { return runClipUpload(clipUploadCmd, tc.args) })
			if err != nil {
				t.Fatal(err)
			}
			if out != "1\n" {
				t.Fatalf("quiet output = %q, want bare ID", out)
			}
			re := regexp.MustCompile(`^paste-\d{8}-\d{6}` + regexp.QuoteMeta(tc.ext) + `$`)
			if len(names) != 1 || !re.MatchString(names[0]) {
				t.Fatalf("uploaded names = %v, want paste-<ts>%s", names, tc.ext)
			}
		})
	}
}

func TestUploadStdinExplicitFilenameKept(t *testing.T) {
	defer resetUploadFlags()
	var names []string
	posts := 0
	uploadServer(t, &names, &posts)
	withStdin(t, "# notes\n")
	clipUploadFilename = "notes.md"
	out, err := captureStdout(t, func() error { return runClipUpload(clipUploadCmd, nil) })
	if err != nil {
		t.Fatal(err)
	}
	if len(names) != 1 || names[0] != "notes.md" || !strings.Contains(out, "Uploaded: notes.md (ID: 1)") {
		t.Fatalf("names=%v out=%q", names, out)
	}
}

func TestUploadRejectsBadInputBeforeAnyRequest(t *testing.T) {
	defer resetUploadFlags()
	var names []string
	posts := 0
	uploadServer(t, &names, &posts)

	if err := runClipUpload(clipUploadCmd, []string{"-", "other.txt"}); err == nil {
		t.Fatal(`"-" mixed with files: want an error`)
	}
	withStdin(t, "")
	if err := runClipUpload(clipUploadCmd, nil); !errors.Is(err, errEmptyStdin) {
		t.Fatalf("empty stdin: err = %v, want errEmptyStdin", err)
	}
	if posts != 0 {
		t.Fatalf("%d uploads sent for invalid input", posts)
	}
}

func TestUploadPartialFailureExitsNonZero(t *testing.T) {
	defer resetUploadFlags()
	var names []string
	posts := 0
	uploadServer(t, &names, &posts)
	good := filepath.Join(t.TempDir(), "good.txt")
	if err := os.WriteFile(good, []byte("ok"), 0o644); err != nil {
		t.Fatal(err)
	}
	clipUploadQuiet = true
	out, err := captureStdout(t, func() error {
		return runClipUpload(clipUploadCmd, []string{good, filepath.Join(t.TempDir(), "missing.txt")})
	})
	if out != "1\n" {
		t.Fatalf("output = %q, want the successful ID", out)
	}
	if err == nil || !strings.Contains(err.Error(), "1 of 2") {
		t.Fatalf("err = %v, want a partial-failure error", err)
	}
}

func TestClipListQuietAndOrder(t *testing.T) {
	var got url.Values
	requests := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests++
		got = r.URL.Query()
		w.Write([]byte(`{"clips":[{"id":3,"filename":"a"},{"id":9,"filename":"b"}],"total":2}`))
	}))
	defer srv.Close()
	t.Setenv("MP_API_URL", srv.URL)
	t.Setenv("MP_API_KEY", "k")
	defer func() {
		clipListQuiet, clipListSort, clipListDir, clipListSearch = false, "", "", ""
		jsonOutput = false
	}()

	clipListQuiet, clipListSort, clipListDir, clipListSearch = true, "name", "asc", "needle"
	out, err := captureStdout(t, func() error { return runClipList(clipListCmd, nil) })
	if err != nil {
		t.Fatal(err)
	}
	if out != "3\n9\n" {
		t.Fatalf("quiet output = %q", out)
	}
	if got.Get("sort") != "name" || got.Get("dir") != "asc" || got.Get("search") != "needle" || got.Get("search_content") != "true" {
		t.Fatalf("query = %v", got)
	}

	for _, bad := range [][2]string{{"nmae", ""}, {"", "up"}} {
		clipListSort, clipListDir = bad[0], bad[1]
		if err := runClipList(clipListCmd, nil); err == nil {
			t.Fatalf("sort=%q dir=%q: want a validation error", bad[0], bad[1])
		}
	}
	if requests != 1 {
		t.Fatalf("invalid --sort/--dir still sent requests (%d total)", requests)
	}
}

func TestClipDataLatest(t *testing.T) {
	var listQuery map[string][]string
	mux := http.NewServeMux()
	mux.HandleFunc("GET /api/v1/tags", func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(`[{"id":5,"name":"notes"}]`))
	})
	mux.HandleFunc("GET /api/v1/clips", func(w http.ResponseWriter, r *http.Request) {
		listQuery = r.URL.Query()
		w.Write([]byte(`{"clips":[{"id":42}]}`))
	})
	mux.HandleFunc("GET /api/v1/clips/42/data", func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte("newest"))
	})
	srv := httptest.NewServer(mux)
	defer srv.Close()
	t.Setenv("MP_API_URL", srv.URL)
	t.Setenv("MP_API_KEY", "k")
	defer func() { clipDataLatest, clipDataTag = false, "" }()

	clipDataLatest, clipDataTag = true, "notes"
	out, err := captureStdout(t, func() error { return runClipData(clipDataCmd, nil) })
	if err != nil {
		t.Fatal(err)
	}
	if out != "newest" || listQuery["limit"][0] != "1" || listQuery["tag"][0] != "5" {
		t.Fatalf("out=%q query=%v", out, listQuery)
	}

	if err := runClipData(clipDataCmd, []string{"42"}); err == nil {
		t.Fatal("--latest with an ID: want an error")
	}
	clipDataLatest = false
	if err := runClipData(clipDataCmd, []string{"42"}); err == nil {
		t.Fatal("--tag without --latest: want an error")
	}
	clipDataTag = ""
	if err := runClipData(clipDataCmd, nil); err == nil {
		t.Fatal("no ID and no --latest: want an error")
	}
}

func TestGuardTerminal(t *testing.T) {
	bin := "\x89PNG\r\n\x1a\n\x00\x00"
	if _, err := guardTerminal(strings.NewReader(bin), true); err == nil {
		t.Fatal("binary on a terminal: want refusal")
	}
	r, err := guardTerminal(strings.NewReader(bin), false)
	if err != nil {
		t.Fatal(err)
	}
	if b, _ := io.ReadAll(r); string(b) != bin {
		t.Fatal("unchecked output must pass through unchanged")
	}
	text := strings.Repeat("line of text\n", 100)
	r, err = guardTerminal(strings.NewReader(text), true)
	if err != nil {
		t.Fatal(err)
	}
	if b, _ := io.ReadAll(r); string(b) != text {
		t.Fatal("text must be replayed in full after the peek")
	}
}

// oneByteReader hands out a byte per Read, so every sequence straddles a
// chunk boundary.
type oneByteReader struct{ r io.Reader }

func (o oneByteReader) Read(p []byte) (int, error) {
	if len(p) == 0 {
		return 0, nil
	}
	return o.r.Read(p[:1])
}

// A clip can come from someone else (a followed share, another API key, a
// plugin), so text bound for a terminal must not carry control sequences,
// wherever in the stream they sit.
func TestGuardTerminalRefusesControlSequences(t *testing.T) {
	long := strings.Repeat("a", 600)
	refused := map[string]struct{ data, shown string }{
		"clear screen":         {"clear \x1b[2J done\n", ""},
		"osc 52":               {"x\x1b]52;c;aGk=\x1b\\ done\n", ""},
		"bell":                 {"ding\a", ""},
		"backspace":            {"safe\b\b\b\bevil", ""},
		"c1 csi":               {"x\u009b2Jy", ""},
		"unfinished sequence":  {"tail\x1b[3", "tail"},
		"oversized sequence":   {"x\x1b[" + strings.Repeat("1;", 80) + "m", ""},
		"binary past the head": {long + "\x00\x01\x02\a\x1b[2J", long},
		"escape past the head": {long + "ok\x1b]0;title\a", long + "ok"},
		// A terminal in 8-bit mode reads these bare bytes as CSI, OSC and
		// DCS; only the UTF-8 form (C2 9B) used to be refused.
		"raw c1 csi":             {"hello \x9b2J", ""},
		"raw c1 osc":             {"x\x9d52;c;aGk=\x9c", ""},
		"raw c1 dcs":             {"x\x90q\x9c", ""},
		"raw c1 past the head":   {long + "ok\x9b2J", long + "ok"},
		"latin-1":                {"caf\xe9\n", ""},
		"ends on a lead byte":    {"latin-1 \xc2", "latin-1 "},
		"cut-off utf-8":          {"cut \xe6\x97", "cut "},
		"overlong escape":        {"x\xc0\x9b2J", ""},
		"overlong 3-byte c1":     {"x\xe0\x82\x9b2J", ""},
		"surrogate":              {"x\xed\xa0\x80", ""},
		"lone carriage return":   {"safe line\rEVIL", ""},
		"carriage return at end": {"line\r", "line"},
		"cr cr lf":               {"line\r\r\n", ""},
		"conceal":                {"see \x1b[8mhidden\x1b[0m\n", ""},
		"conceal among colours":  {"\x1b[1;31;08mhidden\x1b[0m\n", ""},
		"conceal after a colour": {"\x1b[38;5;196;8mhidden\x1b[0m\n", ""},
		"conceal after rgb":      {"\x1b[48;2;1;2;3;8mhidden\x1b[0m\n", ""},
		"conceal, colon colour":  {"\x1b[38:5:196;8mhidden\x1b[0m\n", ""},
		"bidi override":          {"file\u202egnp.exe\n", ""},
		"bidi isolate":           {"a\u2067b\u2069\n", ""},
		"bidi embedding":         {"a\u202bb\u202c\n", ""},
	}
	for name, tc := range refused {
		for _, split := range []bool{false, true} {
			var src io.Reader = strings.NewReader(tc.data)
			if split {
				src = oneByteReader{src}
			}
			r, err := guardTerminal(src, true)
			var got []byte
			if err == nil {
				got, err = io.ReadAll(r)
			}
			if err == nil {
				t.Errorf("%s (split=%v): written to the terminal, want a refusal", name, split)
			}
			if string(got) != tc.shown {
				t.Errorf("%s (split=%v): wrote %q before refusing, want %q", name, split, got, tc.shown)
			}
		}
	}

	passes := map[string]string{
		"utf-8, tabs, crlf": "caf\u00e9 \u00a0\u00c2 \u65e5\u672c\tx\r\npage\f\n",
		"colours":           "\x1b[1;31mred\x1b[0m \x1b[38;2;255;0;0;48:5:17mrgb\x1b[m\n",
		"colour past head":  long + "\x1b[32mgreen\x1b[0m\n",
		"4-byte utf-8":      "emoji \U0001F600 and \U0010FFFF\n",
		"colour index 8":    "\x1b[38;5;8mgrey\x1b[48;2;8;8;8mdark\x1b[38:5:8m\x1b[38:2::8:8:8m\x1b[0m\n",
		"sgr 18, 80, 28":    "\x1b[18;80;28mx\x1b[0m\n",
		"rtl text, marks":   "\u05e9\u05dc\u05d5\u05dd \u200f\u200e ok\n",
	}
	for name, data := range passes {
		for _, split := range []bool{false, true} {
			var src io.Reader = strings.NewReader(data)
			if split {
				src = oneByteReader{src}
			}
			r, err := guardTerminal(src, true)
			if err != nil {
				t.Errorf("%s (split=%v): refused: %v", name, split, err)
				continue
			}
			if got, err := io.ReadAll(r); err != nil || string(got) != data {
				t.Errorf("%s (split=%v): got %q, %v; want it unchanged", name, split, got, err)
			}
		}
	}

	// --force and redirected output stay byte-exact.
	raw := long + "\x00\x1b[2J\x1b]52;c;aGk=\a"
	r, err := guardTerminal(strings.NewReader(raw), false)
	if err != nil {
		t.Fatal(err)
	}
	if got, _ := io.ReadAll(r); string(got) != raw {
		t.Fatal("unchecked output must pass through unchanged")
	}
}

// dedupServer answers uploads as the real API does: 201 for new content, 200
// with the existing clip when the same bytes were uploaded before ("seeded"
// is already in the library as clip 1, "kept.md"). It records every PUT.
func dedupServer(t *testing.T, puts *[]string) {
	t.Helper()
	ids := map[string]int{"seeded": 1}
	names := map[int]string{1: "kept.md"}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodGet && r.URL.Path == "/api/v1/tags":
			w.Write([]byte(`[{"id":5,"name":"notes"}]`))
		case r.Method == http.MethodPut:
			*puts = append(*puts, r.URL.Path)
			w.Write([]byte(`{}`))
		case r.Method == http.MethodPost && r.URL.Path == "/api/v1/clips":
			mr, err := r.MultipartReader()
			if err != nil {
				http.Error(w, err.Error(), http.StatusBadRequest)
				return
			}
			p, err := mr.NextPart()
			if err != nil {
				http.Error(w, err.Error(), http.StatusBadRequest)
				return
			}
			b, _ := io.ReadAll(p)
			id, dup := ids[string(b)]
			if !dup {
				id = len(ids) + 1
				ids[string(b)], names[id] = id, p.FileName()
				w.WriteHeader(http.StatusCreated)
			}
			fmt.Fprintf(w, `{"id":%d,"filename":%q}`, id, names[id])
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(srv.Close)
	t.Setenv("MP_API_URL", srv.URL)
	t.Setenv("MP_API_KEY", "k")
}

// An upload that dedups onto a clip it did not create must not tag it or put
// an expiry on it: the reaper would delete a clip that was meant to be kept.
func TestUploadDuplicateIsNotTaggedOrExpired(t *testing.T) {
	defer resetUploadFlags()
	var puts []string
	dedupServer(t, &puts)

	withStdin(t, "seeded")
	clipUploadTag, clipUploadExpire, clipUploadFilename = "notes", "30m", "scratch.txt"
	out, err := captureStdout(t, func() error { return runClipUpload(clipUploadCmd, nil) })
	if err != nil {
		t.Fatal(err)
	}
	if len(puts) != 0 {
		t.Fatalf("duplicate upload changed the existing clip: %v", puts)
	}
	if out != "Already exists: kept.md (ID: 1)\n" {
		t.Fatalf("output = %q", out)
	}

	// --quiet still prints the ID, and --json marks the duplicate.
	withStdin(t, "seeded")
	clipUploadQuiet = true
	if out, err = captureStdout(t, func() error { return runClipUpload(clipUploadCmd, nil) }); err != nil || out != "1\n" {
		t.Fatalf("quiet: out = %q, err = %v", out, err)
	}
	withStdin(t, "seeded")
	clipUploadQuiet, jsonOutput = false, true
	if out, err = captureStdout(t, func() error { return runClipUpload(clipUploadCmd, nil) }); err != nil || !strings.Contains(out, `"duplicate": true`) {
		t.Fatalf("json: out = %q, err = %v", out, err)
	}
	if len(puts) != 0 {
		t.Fatalf("duplicate upload changed the existing clip: %v", puts)
	}
}

// New clips still get the tag and expiry; a second file with the same bytes
// in one command is a duplicate of the first and is left alone.
func TestUploadCreatedClipIsTaggedAndExpired(t *testing.T) {
	defer resetUploadFlags()
	var puts []string
	dedupServer(t, &puts)
	dir := t.TempDir()
	a, b := filepath.Join(dir, "a.txt"), filepath.Join(dir, "b.txt")
	for _, f := range []string{a, b} {
		if err := os.WriteFile(f, []byte("fresh"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	clipUploadTag, clipUploadExpire = "notes", "30m"
	out, err := captureStdout(t, func() error { return runClipUpload(clipUploadCmd, []string{a, b}) })
	if err != nil {
		t.Fatal(err)
	}
	want := []string{"/api/v1/clips/2/tags/5", "/api/v1/clips/2/expiration"}
	if fmt.Sprint(puts) != fmt.Sprint(want) {
		t.Fatalf("PUTs = %v, want %v", puts, want)
	}
	if out != "Uploaded: a.txt (ID: 2)\nAlready exists: a.txt (ID: 2)\n" {
		t.Fatalf("output = %q", out)
	}
}
