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
