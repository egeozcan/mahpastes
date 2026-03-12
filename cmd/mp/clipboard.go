package main

import (
	"fmt"
	"strconv"

	"github.com/spf13/cobra"

	"go-clipboard/cmd/mp/client"
)

// ── clipboard (parent) ──────────────────────────────────────────────────

var clipboardCmd = &cobra.Command{
	Use:   "clipboard",
	Short: "Copy clip content to the system clipboard",
	Long: `Copy clip contents or file references to the macOS system clipboard.

These commands instruct the mahpastes desktop app to place clip data on
the system clipboard. The desktop app must be running.`,
}

// ── clipboard copy ──────────────────────────────────────────────────────

var clipboardCopyCmd = &cobra.Command{
	Use:   "copy <id>",
	Short: "Copy clip contents to clipboard",
	Long: `Copy the raw contents of a clip to the system clipboard.

For text clips, this copies the text. For images, the image data is placed
on the clipboard. The mahpastes desktop app performs the actual clipboard
operation.`,
	Example: `  # Copy clip contents to clipboard
  mp clipboard copy 42

  # Copy and confirm with JSON
  mp clipboard copy 42 --json`,
	Args: cobra.ExactArgs(1),
	RunE: runClipboardCopy,
}

func runClipboardCopy(cmd *cobra.Command, args []string) error {
	c, err := client.New()
	if err != nil {
		return err
	}

	id, err := strconv.ParseInt(args[0], 10, 64)
	if err != nil {
		return fmt.Errorf("invalid clip ID: %s", args[0])
	}

	body := map[string]int64{"clip_id": id}
	if err := c.PostJSON("/api/v1/clipboard/copy", body, nil); err != nil {
		return err
	}

	if jsonOutput {
		printJSON(map[string]interface{}{"clip_id": id, "copied": true})
		return nil
	}

	fmt.Printf("Copied clip %d contents to clipboard\n", id)
	return nil
}

// ── clipboard copy-file ─────────────────────────────────────────────────

var clipboardCopyFileCmd = &cobra.Command{
	Use:   "copy-file <id>",
	Short: "Copy clip as file to clipboard",
	Long: `Copy a clip as a file reference to the system clipboard.

This places a file reference on the clipboard so you can paste the clip
as a file into Finder or other applications. The mahpastes desktop app
performs the actual clipboard operation.`,
	Example: `  # Copy clip as file reference
  mp clipboard copy-file 42

  # Copy and confirm with JSON
  mp clipboard copy-file 42 --json`,
	Args: cobra.ExactArgs(1),
	RunE: runClipboardCopyFile,
}

func runClipboardCopyFile(cmd *cobra.Command, args []string) error {
	c, err := client.New()
	if err != nil {
		return err
	}

	id, err := strconv.ParseInt(args[0], 10, 64)
	if err != nil {
		return fmt.Errorf("invalid clip ID: %s", args[0])
	}

	body := map[string]int64{"clip_id": id}
	if err := c.PostJSON("/api/v1/clipboard/copy-file", body, nil); err != nil {
		return err
	}

	if jsonOutput {
		printJSON(map[string]interface{}{"clip_id": id, "copied_as_file": true})
		return nil
	}

	fmt.Printf("Copied clip %d as file to clipboard\n", id)
	return nil
}

// ── init ────────────────────────────────────────────────────────────────

func init() {
	clipboardCmd.AddCommand(clipboardCopyCmd)
	clipboardCmd.AddCommand(clipboardCopyFileCmd)
}
