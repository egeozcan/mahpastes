package main

import (
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
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
	// App not running (closed port) and a server that never answers.
	t.Setenv("MP_API_KEY", "k")
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		time.Sleep(3 * time.Second)
	}))
	defer srv.Close()
	for _, url := range []string{"http://127.0.0.1:1", srv.URL} {
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
	registerCompletions()
	for _, c := range []*cobra.Command{tagDeleteCmd, tagMergeCmd} {
		if c.ValidArgsFunction == nil {
			t.Fatalf("%s has no ValidArgsFunction", c.Name())
		}
	}
	vals, dir := roleCompletionFor(t)
	if len(vals) != 3 || dir != cobra.ShellCompDirectiveNoFileComp {
		t.Fatalf("role completion: %v %v", vals, dir)
	}
}

func roleCompletionFor(t *testing.T) ([]string, cobra.ShellCompDirective) {
	t.Helper()
	return staticCompleter(roleSuggestions)(nil, nil, "")
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
	if string(committed) != s {
		t.Error("docs/docs/reference/cli.md is stale; run `make mp-docs`")
	}
}
