package main

import (
	"bytes"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/spf13/cobra"

	"go-clipboard/cmd/mp/client"
)

func TestFetchTagNames(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(`[{"id":1,"name":"work/b"},{"id":2,"name":"Work/a"},{"id":3,"name":"home"}]`))
	}))
	defer srv.Close()
	c := &client.Client{BaseURL: srv.URL, APIKey: "k", HTTPClient: srv.Client()}
	got := fetchTagNames(c, "wo")
	if len(got) != 2 || got[0] != "Work/a" || got[1] != "work/b" {
		t.Fatalf("got %v", got)
	}
}

func TestCompletionFailsFastAndSilently(t *testing.T) {
	// Missing key.
	t.Setenv("MP_API_KEY", "")
	if got := tagCompletions(""); got != nil {
		t.Fatalf("no key: got %v", got)
	}
	t.Setenv("MP_API_KEY", "k")

	// A server that never answers, and one that sends headers and then stalls
	// the body. Both are released when the test ends so Close does not wait.
	release := make(chan struct{})
	noAnswer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		select {
		case <-release:
		case <-r.Context().Done():
		}
	}))
	defer noAnswer.Close()
	stalledBody := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		w.Write([]byte(`[{"id":1,"name":"wo`))
		w.(http.Flusher).Flush()
		select {
		case <-release:
		case <-r.Context().Done():
		}
	}))
	defer stalledBody.Close()
	defer close(release)

	// App not running (closed port), no answer, stalled body.
	for _, url := range []string{"http://127.0.0.1:1", noAnswer.URL, stalledBody.URL} {
		t.Setenv("MP_API_URL", url)
		start := time.Now()
		if got := tagCompletions(""); got != nil {
			t.Fatalf("%s: got %v", url, got)
		}
		if d := time.Since(start); d > 2*time.Second {
			t.Fatalf("%s: completion took %v", url, d)
		}
	}
}

func TestRegisterCompletions(t *testing.T) {
	// Idempotent, and every listed flag exists: a renamed flag must fail here
	// rather than print an error on every mp run.
	for i := 0; i < 2; i++ {
		if err := registerCompletions(); err != nil {
			t.Fatal(err)
		}
	}
	for _, p := range flagCompletions() {
		if _, ok := p.cmd.GetFlagCompletionFunc(p.flag); !ok {
			t.Errorf("%s --%s has no completion", p.cmd.CommandPath(), p.flag)
		}
	}
	for _, c := range []*cobra.Command{tagUpdateCmd, tagDeleteCmd, tagClipsCmd, tagMergeCmd, serveStartCmd, serveStopCmd, tagAssignCmd, tagRemoveCmd} {
		if c.ValidArgsFunction == nil {
			t.Errorf("%s has no ValidArgsFunction", c.CommandPath())
		}
	}
	vals, dir := staticCompleter(roleSuggestions)(nil, nil, "")
	if len(vals) != 3 || dir != cobra.ShellCompDirectiveNoFileComp {
		t.Fatalf("role completion: %v %v", vals, dir)
	}
}

// TestCompleteThroughCobra drives the hidden __complete command the shell
// scripts call, against a fake API.
func TestCompleteThroughCobra(t *testing.T) {
	if err := registerCompletions(); err != nil {
		t.Fatal(err)
	}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/v1/tags" || r.Header.Get("Authorization") == "" && r.Header.Get("X-API-Key") == "" {
			http.Error(w, "unexpected", http.StatusBadRequest)
			return
		}
		w.Write([]byte(`[{"id":1,"name":"work/b"},{"id":2,"name":"work/a"},{"id":3,"name":"home"}]`))
	}))
	defer srv.Close()
	t.Setenv("MP_API_URL", srv.URL)
	t.Setenv("MP_API_KEY", "k")

	complete := func(args ...string) string {
		t.Helper()
		var out bytes.Buffer
		rootCmd.SetOut(&out)
		rootCmd.SetErr(&out)
		rootCmd.SetArgs(append([]string{cobra.ShellCompRequestCmd}, args...))
		defer func() {
			rootCmd.SetOut(nil)
			rootCmd.SetErr(nil)
			rootCmd.SetArgs(nil)
		}()
		if err := rootCmd.Execute(); err != nil {
			t.Fatalf("%v: %v", args, err)
		}
		return out.String()
	}

	for _, tc := range []struct {
		args []string
		want []string
		not  []string
	}{
		{[]string{"clip", "list", "--tag", "wo"}, []string{"work/a\n", "work/b\n"}, []string{"home"}},
		{[]string{"tag", "delete", ""}, []string{"home\n", "work/a\n"}, nil},
		{[]string{"tag", "merge", "home", "w"}, []string{"work/a\n"}, nil},
		{[]string{"serve", "start", ""}, []string{"home\n"}, nil},
		{[]string{"clip", "upload", "--content-type", ""}, []string{"text/plain\n"}, nil},
		{[]string{"tag", "delete", "home", ""}, []string{":4\n"}, []string{"work/a"}},
	} {
		got := complete(tc.args...)
		for _, w := range tc.want {
			if !strings.Contains(got, w) {
				t.Errorf("%v: missing %q in %q", tc.args, w, got)
			}
		}
		for _, n := range tc.not {
			if strings.Contains(got, n) {
				t.Errorf("%v: unexpected %q in %q", tc.args, n, got)
			}
		}
	}
}

func TestManPages(t *testing.T) {
	dir := t.TempDir()
	rootCmd.SetArgs([]string{"gendocs", "--man", dir})
	defer rootCmd.SetArgs(nil)
	if err := rootCmd.Execute(); err != nil {
		t.Fatal(err)
	}
	page, err := os.ReadFile(filepath.Join(dir, "mp-clip-list.1"))
	if err != nil {
		t.Fatal(err)
	}
	head := strings.SplitN(string(page), "\n", 3)
	if len(head) < 2 || !strings.HasPrefix(head[1], `.TH "MP-CLIP-LIST" "1"`) || !strings.Contains(head[1], `"mahpastes"`) {
		t.Errorf("unexpected man header %q", head)
	}
	if _, err := os.Stat(filepath.Join(dir, "mp-gendocs.1")); err == nil {
		t.Error("hidden gendocs command got a man page")
	}
}

func TestMarkdownReference(t *testing.T) {
	md, err := renderMarkdownReference(rootCmd)
	if err != nil {
		t.Fatal(err)
	}
	s := string(md)
	for _, want := range []string{"## mp clip list", "## mp tag merge", "(#mp-clip-list)"} {
		if !strings.Contains(s, want) {
			t.Errorf("missing %q", want)
		}
	}
	if strings.Contains(s, "gendocs") {
		t.Error("hidden command leaked into the reference")
	}
	// Drift guard: the committed reference must match the command tree.
	committed, err := os.ReadFile(filepath.Join("..", "..", "docs", "docs", "reference", "cli.md"))
	if err != nil {
		t.Fatal(err)
	}
	// A Windows checkout with core.autocrlf has CRLF line endings.
	if strings.ReplaceAll(string(committed), "\r\n", "\n") != s {
		t.Error("docs/docs/reference/cli.md is stale; run `make mp-docs`")
	}
}

// Cobra renders man pages through Markdown, which drops <placeholder> as an
// HTML tag and reads the underscores in MP_API_KEY ... MP_API_URL as emphasis.
func TestManPagesKeepPlaceholdersAndUnderscores(t *testing.T) {
	before, err := renderMarkdownReference(rootCmd)
	if err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	rootCmd.SetArgs([]string{"gendocs", "--man", dir})
	defer rootCmd.SetArgs(nil)
	if err := rootCmd.Execute(); err != nil {
		t.Fatal(err)
	}
	read := func(name string) string {
		t.Helper()
		b, err := os.ReadFile(filepath.Join(dir, name))
		if err != nil {
			t.Fatal(err)
		}
		return string(b)
	}
	for name, wants := range map[string][]string{
		"mp-clip-open.1":   {"mp clip open <id> [flags]"},
		"mp-clip-rename.1": {"mp clip rename <id> <filename> [flags]"},
		"mp-tag-merge.1":   {"mp tag merge <source-tag> <dest-tag> [flags]"},
		"mp.1":             {"MP_API_KEY=<your-key>", "MP_API_KEY_FILE=<path>", "MAHPASTES_API_KEY, MAHPASTES_API_URL", "the MP_ variables"},
	} {
		page := read(name)
		for _, want := range wants {
			if !strings.Contains(page, want) {
				t.Errorf("%s: missing %q", name, want)
			}
		}
	}

	// Every page: the full usage line survives, and no escape leaks through
	// as a visible backslash (code blocks are copied verbatim).
	leaked := regexp.MustCompile(`\\\\[<>_*]`)
	var walk func(c *cobra.Command)
	walk = func(c *cobra.Command) {
		if c != rootCmd && (!c.IsAvailableCommand() || c.IsAdditionalHelpTopicCommand()) {
			return
		}
		name := strings.ReplaceAll(c.CommandPath(), " ", "-") + ".1"
		page := read(name)
		if !strings.Contains(page, `\fB`+c.UseLine()+`\fP`) {
			t.Errorf("%s: SYNOPSIS is not %q", name, c.UseLine())
		}
		if m := leaked.FindString(page); m != "" {
			t.Errorf("%s: escape %q leaked into the page", name, m)
		}
		for _, sub := range c.Commands() {
			walk(sub)
		}
	}
	walk(rootCmd)

	// The escapes are undone: the Markdown reference (and --help) read the
	// same strings.
	after, err := renderMarkdownReference(rootCmd)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(before, after) {
		t.Error("generating man pages changed the Markdown reference")
	}
}
