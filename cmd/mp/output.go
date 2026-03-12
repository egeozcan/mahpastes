package main

import (
	"encoding/json"
	"fmt"
	"io"
	"os"
	"text/tabwriter"
)

// jsonOutput is set by the --json persistent flag.
var jsonOutput bool

// printJSON pretty-prints v as indented JSON to stdout.
func printJSON(v interface{}) {
	enc := json.NewEncoder(os.Stdout)
	enc.SetIndent("", "  ")
	_ = enc.Encode(v)
}

// printTable prints a formatted table with headers and rows to stdout.
func printTable(headers []string, rows [][]string) {
	w := tabwriter.NewWriter(os.Stdout, 0, 0, 2, ' ', 0)
	for i, h := range headers {
		if i > 0 {
			fmt.Fprint(w, "\t")
		}
		fmt.Fprint(w, h)
	}
	fmt.Fprintln(w)

	for _, row := range rows {
		for i, col := range row {
			if i > 0 {
				fmt.Fprint(w, "\t")
			}
			fmt.Fprint(w, col)
		}
		fmt.Fprintln(w)
	}
	w.Flush()
}

// printKeyValue prints aligned key-value pairs to stdout.
// pairs is a slice of [2]string{key, value}.
func printKeyValue(pairs [][2]string) {
	maxKey := 0
	for _, p := range pairs {
		if len(p[0]) > maxKey {
			maxKey = len(p[0])
		}
	}
	for _, p := range pairs {
		fmt.Printf("%-*s  %s\n", maxKey, p[0]+":", p[1])
	}
}

// errorf prints a formatted error message to stderr.
func errorf(format string, args ...interface{}) {
	fmt.Fprintf(os.Stderr, "Error: "+format+"\n", args...)
}

// copyToStdout pipes the contents of r to stdout.
func copyToStdout(r io.Reader) error {
	_, err := io.Copy(os.Stdout, r)
	return err
}
