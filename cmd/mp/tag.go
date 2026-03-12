package main

import (
	"bufio"
	"fmt"
	"os"
	"strconv"
	"strings"

	"github.com/spf13/cobra"

	"go-clipboard/cmd/mp/client"
)

// ── tag (parent) ────────────────────────────────────────────────────────

var tagCmd = &cobra.Command{
	Use:   "tag",
	Short: "Manage tags",
	Long: `Create, list, update, and delete tags.

Tags organize clips into categories and can form hierarchical trees
using "/" as a separator (e.g., "work/client1/projectA").`,
}

// ── tag list ────────────────────────────────────────────────────────────

var tagListCmd = &cobra.Command{
	Use:   "list",
	Short: "List all tags",
	Long: `List all tags with their ID, name, color, and clip count.

Use --children-of to list only the direct children of a specific tag.`,
	Example: `  mp tag list
  mp tag list --json
  mp tag list --children-of work
  mp tag list --children-of 5`,
	RunE: runTagList,
}

func runTagList(cmd *cobra.Command, args []string) error {
	c, err := client.New()
	if err != nil {
		return err
	}

	childrenOf, _ := cmd.Flags().GetString("children-of")

	var tags []struct {
		ID    int64  `json:"id"`
		Name  string `json:"name"`
		Color string `json:"color"`
		Count int    `json:"count"`
	}

	if childrenOf != "" {
		parentID, err := c.ResolveTagID(childrenOf)
		if err != nil {
			return err
		}
		if err := c.GetJSON(fmt.Sprintf("/api/v1/tags/%d/children", parentID), &tags); err != nil {
			return err
		}
	} else {
		if err := c.GetJSON("/api/v1/tags", &tags); err != nil {
			return err
		}
	}

	if jsonOutput {
		printJSON(tags)
		return nil
	}

	if len(tags) == 0 {
		fmt.Println("No tags found.")
		return nil
	}

	headers := []string{"ID", "NAME", "COLOR", "CLIPS"}
	rows := make([][]string, len(tags))
	for i, t := range tags {
		color := t.Color
		if color == "" {
			color = "-"
		}
		rows[i] = []string{
			strconv.FormatInt(t.ID, 10),
			t.Name,
			color,
			strconv.Itoa(t.Count),
		}
	}
	printTable(headers, rows)
	return nil
}

// ── tag create ──────────────────────────────────────────────────────────

var tagCreateCmd = &cobra.Command{
	Use:   "create <name>",
	Short: "Create a new tag",
	Long: `Create a new tag with the given name.

Hierarchical tags use "/" as a separator (e.g., "work/client1").
Parent tags are created automatically if they don't exist.`,
	Example: `  mp tag create photos
  mp tag create work/client1
  mp tag create "design assets" --color "#ff0000"`,
	Args: cobra.ExactArgs(1),
	RunE: runTagCreate,
}

func runTagCreate(cmd *cobra.Command, args []string) error {
	c, err := client.New()
	if err != nil {
		return err
	}

	name := args[0]
	color, _ := cmd.Flags().GetString("color")

	body := map[string]string{"name": name}
	if color != "" {
		body["color"] = color
	}

	var created struct {
		ID    int64  `json:"id"`
		Name  string `json:"name"`
		Color string `json:"color"`
		Count int    `json:"count"`
	}

	if err := c.PostJSON("/api/v1/tags", body, &created); err != nil {
		return err
	}

	if jsonOutput {
		printJSON(created)
		return nil
	}

	fmt.Printf("Created tag: %s (ID: %d)\n", created.Name, created.ID)
	return nil
}

// ── tag update ──────────────────────────────────────────────────────────

var tagUpdateCmd = &cobra.Command{
	Use:   "update <tag>",
	Short: "Update a tag's name or color",
	Long: `Update an existing tag's name, color, or both.

The tag argument can be a tag name or numeric ID.
At least one of --name or --color must be provided.`,
	Example: `  mp tag update photos --name "my photos"
  mp tag update 5 --color "#00ff00"
  mp tag update work --name "projects" --color "#0000ff"`,
	Args: cobra.ExactArgs(1),
	RunE: runTagUpdate,
}

func runTagUpdate(cmd *cobra.Command, args []string) error {
	c, err := client.New()
	if err != nil {
		return err
	}

	newName, _ := cmd.Flags().GetString("name")
	newColor, _ := cmd.Flags().GetString("color")

	if newName == "" && newColor == "" {
		return fmt.Errorf("at least one of --name or --color is required")
	}

	tagID, err := c.ResolveTagID(args[0])
	if err != nil {
		return err
	}

	body := map[string]string{}
	if newName != "" {
		body["name"] = newName
	}
	if newColor != "" {
		body["color"] = newColor
	}

	if err := c.PutJSON(fmt.Sprintf("/api/v1/tags/%d", tagID), body, nil); err != nil {
		return err
	}

	if jsonOutput {
		printJSON(map[string]interface{}{"id": tagID, "updated": true})
		return nil
	}

	fmt.Printf("Updated tag %d\n", tagID)
	return nil
}

// ── tag delete ──────────────────────────────────────────────────────────

var tagDeleteCmd = &cobra.Command{
	Use:   "delete <tag>",
	Short: "Delete a tag",
	Long: `Delete a tag by name or ID.

This removes the tag from all clips but does not delete the clips themselves.`,
	Example: `  mp tag delete photos
  mp tag delete 5`,
	Args: cobra.ExactArgs(1),
	RunE: runTagDelete,
}

func runTagDelete(cmd *cobra.Command, args []string) error {
	c, err := client.New()
	if err != nil {
		return err
	}

	tagID, err := c.ResolveTagID(args[0])
	if err != nil {
		return err
	}

	if err := c.Delete(fmt.Sprintf("/api/v1/tags/%d", tagID)); err != nil {
		return err
	}

	if jsonOutput {
		printJSON(map[string]interface{}{"id": tagID, "deleted": true})
		return nil
	}

	fmt.Printf("Deleted tag: %s\n", args[0])
	return nil
}

// ── tag assign ──────────────────────────────────────────────────────────

var tagAssignCmd = &cobra.Command{
	Use:   "assign <clip-id> [clip-id...]",
	Short: "Assign a tag to one or more clips",
	Long: `Add a tag to one or more clips.

Clip IDs can be passed as arguments or read from stdin with --stdin.
Tree exclusivity is enforced: adding a tag removes other tags from
the same root tree on each clip.`,
	Example: `  mp tag assign 1 --tag photos
  mp tag assign 1 2 3 --tag work/client1
  echo "1\n2\n3" | mp tag assign --tag photos --stdin`,
	RunE: runTagAssign,
}

func runTagAssign(cmd *cobra.Command, args []string) error {
	c, err := client.New()
	if err != nil {
		return err
	}

	tagNameOrID, _ := cmd.Flags().GetString("tag")
	if tagNameOrID == "" {
		return fmt.Errorf("--tag is required")
	}

	useStdin, _ := cmd.Flags().GetBool("stdin")
	clipIDs, err := collectClipIDs(args, useStdin)
	if err != nil {
		return err
	}

	if len(clipIDs) == 0 {
		return fmt.Errorf("at least one clip ID is required")
	}

	tagID, err := c.ResolveTagID(tagNameOrID)
	if err != nil {
		return err
	}

	if len(clipIDs) == 1 {
		// Single assign
		if err := c.PutJSON(fmt.Sprintf("/api/v1/clips/%d/tags/%d", clipIDs[0], tagID), nil, nil); err != nil {
			return err
		}
	} else {
		// Bulk assign
		body := map[string]interface{}{
			"ids":    clipIDs,
			"tag_id": tagID,
		}
		if err := c.PostJSON("/api/v1/clips/bulk/tag", body, nil); err != nil {
			return err
		}
	}

	if jsonOutput {
		printJSON(map[string]interface{}{
			"tagged":  len(clipIDs),
			"tag_id":  tagID,
			"clip_ids": clipIDs,
		})
		return nil
	}

	fmt.Printf("Tagged %d clip(s) with %s\n", len(clipIDs), tagNameOrID)
	return nil
}

// ── tag remove ──────────────────────────────────────────────────────────

var tagRemoveCmd = &cobra.Command{
	Use:   "remove <clip-id> [clip-id...]",
	Short: "Remove a tag from one or more clips",
	Long: `Remove a tag from one or more clips.

Clip IDs can be passed as arguments or read from stdin with --stdin.`,
	Example: `  mp tag remove 1 --tag photos
  mp tag remove 1 2 3 --tag work
  echo "1\n2\n3" | mp tag remove --tag photos --stdin`,
	RunE: runTagRemove,
}

func runTagRemove(cmd *cobra.Command, args []string) error {
	c, err := client.New()
	if err != nil {
		return err
	}

	tagNameOrID, _ := cmd.Flags().GetString("tag")
	if tagNameOrID == "" {
		return fmt.Errorf("--tag is required")
	}

	useStdin, _ := cmd.Flags().GetBool("stdin")
	clipIDs, err := collectClipIDs(args, useStdin)
	if err != nil {
		return err
	}

	if len(clipIDs) == 0 {
		return fmt.Errorf("at least one clip ID is required")
	}

	tagID, err := c.ResolveTagID(tagNameOrID)
	if err != nil {
		return err
	}

	if len(clipIDs) == 1 {
		// Single remove
		if err := c.Delete(fmt.Sprintf("/api/v1/clips/%d/tags/%d", clipIDs[0], tagID)); err != nil {
			return err
		}
	} else {
		// Bulk remove
		body := map[string]interface{}{
			"ids":    clipIDs,
			"tag_id": tagID,
		}
		if err := c.PostJSON("/api/v1/clips/bulk/untag", body, nil); err != nil {
			return err
		}
	}

	if jsonOutput {
		printJSON(map[string]interface{}{
			"untagged": len(clipIDs),
			"tag_id":   tagID,
			"clip_ids": clipIDs,
		})
		return nil
	}

	fmt.Printf("Removed %s from %d clip(s)\n", tagNameOrID, len(clipIDs))
	return nil
}

// ── tag clips ───────────────────────────────────────────────────────────

var tagClipsCmd = &cobra.Command{
	Use:   "clips <tag>",
	Short: "List clips with a specific tag",
	Long: `List all clips assigned to a tag.

The tag argument can be a tag name or numeric ID.
Results are paginated with --limit and --offset.`,
	Example: `  mp tag clips photos
  mp tag clips 5 --limit 20
  mp tag clips work/client1 --offset 50 --json`,
	Args: cobra.ExactArgs(1),
	RunE: runTagClips,
}

func runTagClips(cmd *cobra.Command, args []string) error {
	c, err := client.New()
	if err != nil {
		return err
	}

	tagID, err := c.ResolveTagID(args[0])
	if err != nil {
		return err
	}

	limit, _ := cmd.Flags().GetInt("limit")
	offset, _ := cmd.Flags().GetInt("offset")

	path := fmt.Sprintf("/api/v1/tags/%d/clips?limit=%d&offset=%d", tagID, limit, offset)

	var result struct {
		Clips []struct {
			ID          int64  `json:"id"`
			Filename    string `json:"filename"`
			ContentType string `json:"content_type"`
			Size        int64  `json:"size"`
			IsArchived  bool   `json:"is_archived"`
			CreatedAt   string `json:"created_at"`
			Tags        []struct {
				ID    int64  `json:"id"`
				Name  string `json:"name"`
				Color string `json:"color"`
			} `json:"tags"`
		} `json:"clips"`
		Total  int `json:"total"`
		Limit  int `json:"limit"`
		Offset int `json:"offset"`
	}

	if err := c.GetJSON(path, &result); err != nil {
		return err
	}

	if jsonOutput {
		printJSON(result)
		return nil
	}

	if len(result.Clips) == 0 {
		fmt.Println("No clips found.")
		return nil
	}

	headers := []string{"ID", "FILENAME", "TYPE", "SIZE", "ARCHIVED", "CREATED", "TAGS"}
	rows := make([][]string, len(result.Clips))
	for i, clip := range result.Clips {
		archived := ""
		if clip.IsArchived {
			archived = "yes"
		}

		tagNames := make([]string, len(clip.Tags))
		for j, t := range clip.Tags {
			tagNames[j] = t.Name
		}

		rows[i] = []string{
			strconv.FormatInt(clip.ID, 10),
			clip.Filename,
			clip.ContentType,
			formatSize(clip.Size),
			archived,
			clip.CreatedAt,
			strings.Join(tagNames, ", "),
		}
	}
	printTable(headers, rows)

	if result.Total > result.Offset+len(result.Clips) {
		fmt.Printf("\nShowing %d-%d of %d clips\n",
			result.Offset+1,
			result.Offset+len(result.Clips),
			result.Total)
	}

	return nil
}

// ── tag hidden ──────────────────────────────────────────────────────────

var tagHiddenCmd = &cobra.Command{
	Use:   "hidden",
	Short: "View or set hidden tags",
	Long: `View the current list of hidden tag IDs, or update them with --set.

Hidden tags are not shown in the main tag filter UI.`,
	Example: `  mp tag hidden
  mp tag hidden --json
  mp tag hidden --set "1,2,3"
  mp tag hidden --set ""`,
	RunE: runTagHidden,
}

func runTagHidden(cmd *cobra.Command, args []string) error {
	c, err := client.New()
	if err != nil {
		return err
	}

	setVal, _ := cmd.Flags().GetString("set")
	hasSet := cmd.Flags().Changed("set")

	if hasSet {
		// Update hidden tags
		var ids []int64
		if setVal != "" {
			parts := strings.Split(setVal, ",")
			for _, p := range parts {
				p = strings.TrimSpace(p)
				if p == "" {
					continue
				}
				id, err := strconv.ParseInt(p, 10, 64)
				if err != nil {
					return fmt.Errorf("invalid tag ID: %s", p)
				}
				ids = append(ids, id)
			}
		} else {
			ids = []int64{}
		}

		body := map[string][]int64{"ids": ids}
		if err := c.PutJSON("/api/v1/tags/hidden", body, nil); err != nil {
			return err
		}

		if jsonOutput {
			printJSON(map[string][]int64{"ids": ids})
			return nil
		}

		if len(ids) == 0 {
			fmt.Println("Cleared all hidden tags.")
		} else {
			strs := make([]string, len(ids))
			for i, id := range ids {
				strs[i] = strconv.FormatInt(id, 10)
			}
			fmt.Printf("Set hidden tags: %s\n", strings.Join(strs, ", "))
		}
		return nil
	}

	// Get hidden tags
	var result struct {
		IDs []int64 `json:"ids"`
	}
	if err := c.GetJSON("/api/v1/tags/hidden", &result); err != nil {
		return err
	}

	if jsonOutput {
		printJSON(result)
		return nil
	}

	if len(result.IDs) == 0 {
		fmt.Println("No hidden tags.")
		return nil
	}

	strs := make([]string, len(result.IDs))
	for i, id := range result.IDs {
		strs[i] = strconv.FormatInt(id, 10)
	}
	fmt.Printf("Hidden tag IDs: %s\n", strings.Join(strs, ", "))
	return nil
}

// ── helpers ─────────────────────────────────────────────────────────────

// collectClipIDs gathers clip IDs from command args and optionally stdin.
func collectClipIDs(args []string, useStdin bool) ([]int64, error) {
	var ids []int64

	for _, arg := range args {
		id, err := strconv.ParseInt(arg, 10, 64)
		if err != nil {
			return nil, fmt.Errorf("invalid clip ID: %s", arg)
		}
		ids = append(ids, id)
	}

	if useStdin {
		scanner := bufio.NewScanner(os.Stdin)
		for scanner.Scan() {
			line := strings.TrimSpace(scanner.Text())
			if line == "" {
				continue
			}
			id, err := strconv.ParseInt(line, 10, 64)
			if err != nil {
				return nil, fmt.Errorf("invalid clip ID from stdin: %s", line)
			}
			ids = append(ids, id)
		}
		if err := scanner.Err(); err != nil {
			return nil, fmt.Errorf("reading stdin: %w", err)
		}
	}

	return ids, nil
}

// formatSize returns a human-readable size string.
func formatSize(bytes int64) string {
	const (
		kb = 1024
		mb = kb * 1024
		gb = mb * 1024
	)
	switch {
	case bytes >= gb:
		return fmt.Sprintf("%.1f GB", float64(bytes)/float64(gb))
	case bytes >= mb:
		return fmt.Sprintf("%.1f MB", float64(bytes)/float64(mb))
	case bytes >= kb:
		return fmt.Sprintf("%.1f KB", float64(bytes)/float64(kb))
	default:
		return fmt.Sprintf("%d B", bytes)
	}
}

// ── init ────────────────────────────────────────────────────────────────

func init() {
	// tag list
	tagListCmd.Flags().String("children-of", "", "List children of a specific tag (name or ID)")

	// tag create
	tagCreateCmd.Flags().String("color", "", "Tag color as hex (e.g., \"#ff0000\")")

	// tag update
	tagUpdateCmd.Flags().String("name", "", "New tag name")
	tagUpdateCmd.Flags().String("color", "", "New tag color as hex")

	// tag assign
	tagAssignCmd.Flags().String("tag", "", "Tag to assign (name or ID)")
	tagAssignCmd.Flags().Bool("stdin", false, "Read clip IDs from stdin (one per line)")

	// tag remove
	tagRemoveCmd.Flags().String("tag", "", "Tag to remove (name or ID)")
	tagRemoveCmd.Flags().Bool("stdin", false, "Read clip IDs from stdin (one per line)")

	// tag clips
	tagClipsCmd.Flags().Int("limit", 50, "Maximum number of clips to return")
	tagClipsCmd.Flags().Int("offset", 0, "Number of clips to skip")

	// tag hidden
	tagHiddenCmd.Flags().String("set", "", "Comma-separated tag IDs to set as hidden (empty string clears all)")

	// Register subcommands
	tagCmd.AddCommand(tagListCmd)
	tagCmd.AddCommand(tagCreateCmd)
	tagCmd.AddCommand(tagUpdateCmd)
	tagCmd.AddCommand(tagDeleteCmd)
	tagCmd.AddCommand(tagAssignCmd)
	tagCmd.AddCommand(tagRemoveCmd)
	tagCmd.AddCommand(tagClipsCmd)
	tagCmd.AddCommand(tagHiddenCmd)
}
