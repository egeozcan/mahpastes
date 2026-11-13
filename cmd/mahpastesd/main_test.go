package main

import (
	"bytes"
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestHandleArgs(t *testing.T) {
	cases := []struct {
		args       []string
		start      bool
		code       int
		out, errIs string
	}{
		{nil, true, 0, "", ""},
		{[]string{"--help"}, false, 0, "Usage: mahpastesd", ""},
		{[]string{"-h"}, false, 0, "MAHPASTESD_PORT", ""},
		{[]string{"--version"}, false, 0, "mahpastesd ", ""},
		{[]string{"--bogus"}, false, 2, "", `unexpected argument "--bogus"`},
		{[]string{"serve"}, false, 2, "", `unexpected argument "serve"`},
		{[]string{"--help", "extra"}, false, 2, "", "unexpected argument"},
	}
	for _, tc := range cases {
		var out, errOut bytes.Buffer
		start, code := handleArgs(tc.args, &out, &errOut)
		if start != tc.start || code != tc.code {
			t.Errorf("%v: start=%v code=%d, want %v %d", tc.args, start, code, tc.start, tc.code)
		}
		if !strings.Contains(out.String(), tc.out) || (tc.out == "" && out.Len() != 0) {
			t.Errorf("%v: stdout = %q, want %q", tc.args, out.String(), tc.out)
		}
		if !strings.Contains(errOut.String(), tc.errIs) || (tc.errIs == "" && errOut.Len() != 0) {
			t.Errorf("%v: stderr = %q, want %q", tc.args, errOut.String(), tc.errIs)
		}
	}
}

// The binary itself must exit on an argument before it opens the library:
// `mahpastesd --help` used to start a server on the data dir.
func TestArgumentsNeverTouchDataDir(t *testing.T) {
	if os.Getenv("MAHPASTESD_TEST_RUN_MAIN") == "1" {
		os.Args = append([]string{"mahpastesd"}, strings.Fields(os.Getenv("MAHPASTESD_TEST_ARGS"))...)
		main()
		return
	}
	for arg, wantCode := range map[string]int{"--help": 0, "--version": 0, "--bogus": 2} {
		dataDir := filepath.Join(t.TempDir(), "data")
		// A regression here starts a server, which never exits by itself.
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		cmd := exec.CommandContext(ctx, os.Args[0], "-test.run=^TestArgumentsNeverTouchDataDir$")
		cmd.Env = append(os.Environ(),
			"MAHPASTESD_TEST_RUN_MAIN=1", "MAHPASTESD_TEST_ARGS="+arg,
			"MAHPASTES_DATA_DIR="+dataDir, "MAHPASTESD_PORT=0", "HOME="+t.TempDir())
		err := cmd.Run()
		cancel()
		if code := cmd.ProcessState.ExitCode(); code != wantCode {
			t.Errorf("%s: exit code %d (%v), want %d", arg, code, err, wantCode)
		}
		if _, err := os.Stat(dataDir); !os.IsNotExist(err) {
			t.Errorf("%s: data dir was created (stat err = %v)", arg, err)
		}
	}
}
