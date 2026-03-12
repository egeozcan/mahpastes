package main

import (
	"fmt"
	"strconv"

	"github.com/spf13/cobra"

	"go-clipboard/cmd/mp/client"
)

// ── dedup (parent) ──────────────────────────────────────────────────────

var dedupCmd = &cobra.Command{
	Use:   "dedup",
	Short: "Find and merge duplicate clips",
	Long: `Detect duplicate clips by content hash and merge or remove them.

Duplicates are clips with identical binary content. The dedup commands
let you list groups of duplicates, merge a specific group, or remove
all duplicates at once.`,
}

// ── dedup list ──────────────────────────────────────────────────────────

var dedupListCmd = &cobra.Command{
	Use:   "list",
	Short: "List groups of duplicate clips",
	Long: `Show all groups of clips that share the same content hash.

Each row represents a group of duplicates with the same content.
The Count column shows how many copies exist.`,
	Example: `  # List all duplicate groups
  mp dedup list

  # List duplicates as JSON for scripting
  mp dedup list --json`,
	RunE: runDedupList,
}

func runDedupList(cmd *cobra.Command, args []string) error {
	c, err := client.New()
	if err != nil {
		return err
	}

	var groups []struct {
		ContentHash string `json:"content_hash"`
		Filename    string `json:"filename"`
		ContentType string `json:"content_type"`
		Count       int    `json:"count"`
		OldestID    int64  `json:"oldest_id"`
	}

	if err := c.GetJSON("/api/v1/dedup", &groups); err != nil {
		return err
	}

	if jsonOutput {
		printJSON(groups)
		return nil
	}

	if len(groups) == 0 {
		fmt.Println("No duplicates found.")
		return nil
	}

	headers := []string{"HASH", "FILENAME", "TYPE", "COUNT", "OLDEST_ID"}
	rows := make([][]string, len(groups))
	for i, g := range groups {
		hash := g.ContentHash
		if len(hash) > 12 {
			hash = hash[:12]
		}
		rows[i] = []string{
			hash,
			g.Filename,
			g.ContentType,
			strconv.Itoa(g.Count),
			strconv.FormatInt(g.OldestID, 10),
		}
	}

	printTable(headers, rows)
	return nil
}

// ── dedup merge ─────────────────────────────────────────────────────────

var dedupMergeCmd = &cobra.Command{
	Use:   "merge <clip-id>",
	Short: "Merge duplicates for a specific clip",
	Long: `Merge all duplicates of a clip into the specified clip ID.

The given clip is kept and all other clips with the same content hash
are removed. Tags from removed clips are merged onto the kept clip.`,
	Example: `  # Merge duplicates of clip 42
  mp dedup merge 42

  # Merge and show result as JSON
  mp dedup merge 42 --json`,
	Args: cobra.ExactArgs(1),
	RunE: runDedupMerge,
}

func runDedupMerge(cmd *cobra.Command, args []string) error {
	c, err := client.New()
	if err != nil {
		return err
	}

	id, err := strconv.ParseInt(args[0], 10, 64)
	if err != nil {
		return fmt.Errorf("invalid clip ID: %s", args[0])
	}

	if err := c.PostJSON(fmt.Sprintf("/api/v1/dedup/%d/merge", id), nil, nil); err != nil {
		return err
	}

	if jsonOutput {
		printJSON(map[string]interface{}{"clip_id": id, "merged": true})
		return nil
	}

	fmt.Printf("Merged duplicates for clip %d\n", id)
	return nil
}

// ── dedup all ───────────────────────────────────────────────────────────

var dedupAllCmd = &cobra.Command{
	Use:   "all",
	Short: "Remove all duplicate clips",
	Long: `Remove all duplicate clips across the entire library.

For each group of duplicates, the oldest clip is kept and the rest are
removed. Tags from removed clips are merged onto the kept clip.`,
	Example: `  # Remove all duplicates
  mp dedup all

  # Remove all duplicates and show count as JSON
  mp dedup all --json`,
	RunE: runDedupAll,
}

func runDedupAll(cmd *cobra.Command, args []string) error {
	c, err := client.New()
	if err != nil {
		return err
	}

	var result struct {
		Removed int `json:"removed"`
	}

	if err := c.PostJSON("/api/v1/dedup/all", nil, &result); err != nil {
		return err
	}

	if jsonOutput {
		printJSON(result)
		return nil
	}

	fmt.Printf("Removed %d duplicate(s)\n", result.Removed)
	return nil
}

// ── registration ────────────────────────────────────────────────────────

func init() {
	dedupCmd.AddCommand(dedupListCmd)
	dedupCmd.AddCommand(dedupMergeCmd)
	dedupCmd.AddCommand(dedupAllCmd)
}
