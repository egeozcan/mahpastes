package main

import (
	"fmt"
	"strconv"
	"strings"

	"github.com/spf13/cobra"

	"go-clipboard/cmd/mp/client"
)

// ── watch (parent) ──────────────────────────────────────────────────────

var watchCmd = &cobra.Command{
	Use:   "watch",
	Short: "Manage watch folders",
	Long: `Add, update, pause, and remove watch folders.

Watch folders monitor a directory on disk and automatically import new
files as clips. Filters control which file types are imported.`,
}

// ── watch list ──────────────────────────────────────────────────────────

var watchListCmd = &cobra.Command{
	Use:   "list",
	Short: "List all watch folders",
	Long:  `Display all configured watch folders with their settings.`,
	Example: `  # List watch folders
  mp watch list

  # List as JSON
  mp watch list --json`,
	RunE: runWatchList,
}

func runWatchList(cmd *cobra.Command, args []string) error {
	c, err := client.New()
	if err != nil {
		return err
	}

	var folders []struct {
		ID        int64  `json:"id"`
		Path      string `json:"path"`
		Filter    string `json:"filter"`
		Paused    bool   `json:"paused"`
		AutoTag   string `json:"auto_tag"`
		AutoTagID int64  `json:"auto_tag_id"`
	}

	if err := c.GetJSON("/api/v1/watch", &folders); err != nil {
		return err
	}

	if jsonOutput {
		printJSON(folders)
		return nil
	}

	if len(folders) == 0 {
		fmt.Println("No watch folders configured.")
		return nil
	}

	headers := []string{"ID", "PATH", "FILTER", "PAUSED", "AUTO_TAG"}
	rows := make([][]string, len(folders))
	for i, f := range folders {
		paused := "no"
		if f.Paused {
			paused = "yes"
		}
		autoTag := f.AutoTag
		if autoTag == "" && f.AutoTagID > 0 {
			autoTag = strconv.FormatInt(f.AutoTagID, 10)
		}
		if autoTag == "" {
			autoTag = "-"
		}
		rows[i] = []string{
			strconv.FormatInt(f.ID, 10),
			f.Path,
			f.Filter,
			paused,
			autoTag,
		}
	}

	printTable(headers, rows)
	return nil
}

// ── watch add ───────────────────────────────────────────────────────────

var watchAddCmd = &cobra.Command{
	Use:   "add <path>",
	Short: "Add a new watch folder",
	Long: `Start watching a directory for new files to import as clips.

Use flags to configure file filters, auto-tagging, and other options.`,
	Example: `  # Watch a folder for all files
  mp watch add ~/Screenshots

  # Watch for images only
  mp watch add ~/Photos --filter presets --presets images

  # Watch with auto-tagging
  mp watch add ~/Downloads --auto-tag downloads

  # Watch with a custom regex filter
  mp watch add ~/Exports --filter custom --regex "\\.(png|jpg)$"

  # Watch and process existing files
  mp watch add ~/Documents --process-existing`,
	Args: cobra.ExactArgs(1),
	RunE: runWatchAdd,
}

var (
	watchAddFilter          string
	watchAddPresets         string
	watchAddRegex           string
	watchAddAutoTag         string
	watchAddAutoArchive     bool
	watchAddProcessExisting bool
)

func init() {
	watchAddCmd.Flags().StringVar(&watchAddFilter, "filter", "all", "File filter mode: all, presets, or custom")
	watchAddCmd.Flags().StringVar(&watchAddPresets, "presets", "", "Comma-separated preset names (images,videos,documents)")
	watchAddCmd.Flags().StringVar(&watchAddRegex, "regex", "", "Custom regex filter pattern")
	watchAddCmd.Flags().StringVar(&watchAddAutoTag, "auto-tag", "", "Tag name or ID to auto-assign to imported clips")
	watchAddCmd.Flags().BoolVar(&watchAddAutoArchive, "auto-archive", false, "Automatically archive imported clips")
	watchAddCmd.Flags().BoolVar(&watchAddProcessExisting, "process-existing", false, "Import existing files when adding the folder")
}

func runWatchAdd(cmd *cobra.Command, args []string) error {
	c, err := client.New()
	if err != nil {
		return err
	}

	body := map[string]interface{}{
		"path":   args[0],
		"filter": watchAddFilter,
	}

	if watchAddPresets != "" {
		body["presets"] = strings.Split(watchAddPresets, ",")
	}
	if watchAddRegex != "" {
		body["regex"] = watchAddRegex
	}
	if watchAddAutoTag != "" {
		body["auto_tag"] = watchAddAutoTag
	}
	if watchAddAutoArchive {
		body["auto_archive"] = true
	}
	if watchAddProcessExisting {
		body["process_existing"] = true
	}

	var result struct {
		ID   int64  `json:"id"`
		Path string `json:"path"`
	}

	if err := c.PostJSON("/api/v1/watch", body, &result); err != nil {
		return err
	}

	if jsonOutput {
		printJSON(result)
		return nil
	}

	fmt.Printf("Watching: %s (ID: %d)\n", result.Path, result.ID)
	return nil
}

// ── watch update ────────────────────────────────────────────────────────

var watchUpdateCmd = &cobra.Command{
	Use:   "update <id>",
	Short: "Update a watch folder's settings",
	Long: `Modify the configuration of an existing watch folder.

Only the flags you provide will be updated; other settings remain unchanged.`,
	Example: `  # Change filter mode
  mp watch update 1 --filter presets --presets images,videos

  # Set auto-tag
  mp watch update 1 --auto-tag screenshots

  # Change regex pattern
  mp watch update 1 --filter custom --regex "\\.(png|jpg)$"`,
	Args: cobra.ExactArgs(1),
	RunE: runWatchUpdate,
}

var (
	watchUpdateFilter      string
	watchUpdatePresets     string
	watchUpdateRegex       string
	watchUpdateAutoTag     string
	watchUpdateAutoArchive bool
)

func init() {
	watchUpdateCmd.Flags().StringVar(&watchUpdateFilter, "filter", "", "File filter mode: all, presets, or custom")
	watchUpdateCmd.Flags().StringVar(&watchUpdatePresets, "presets", "", "Comma-separated preset names (images,videos,documents)")
	watchUpdateCmd.Flags().StringVar(&watchUpdateRegex, "regex", "", "Custom regex filter pattern")
	watchUpdateCmd.Flags().StringVar(&watchUpdateAutoTag, "auto-tag", "", "Tag name or ID to auto-assign")
	watchUpdateCmd.Flags().BoolVar(&watchUpdateAutoArchive, "auto-archive", false, "Automatically archive imported clips")
}

func runWatchUpdate(cmd *cobra.Command, args []string) error {
	c, err := client.New()
	if err != nil {
		return err
	}

	id, err := strconv.ParseInt(args[0], 10, 64)
	if err != nil {
		return fmt.Errorf("invalid watch folder ID: %s", args[0])
	}

	body := map[string]interface{}{}

	if cmd.Flags().Changed("filter") {
		body["filter"] = watchUpdateFilter
	}
	if cmd.Flags().Changed("presets") {
		body["presets"] = strings.Split(watchUpdatePresets, ",")
	}
	if cmd.Flags().Changed("regex") {
		body["regex"] = watchUpdateRegex
	}
	if cmd.Flags().Changed("auto-tag") {
		body["auto_tag"] = watchUpdateAutoTag
	}
	if cmd.Flags().Changed("auto-archive") {
		body["auto_archive"] = watchUpdateAutoArchive
	}

	if len(body) == 0 {
		return fmt.Errorf("at least one flag is required")
	}

	if err := c.PutJSON(fmt.Sprintf("/api/v1/watch/%d", id), body, nil); err != nil {
		return err
	}

	if jsonOutput {
		printJSON(map[string]interface{}{"id": id, "updated": true})
		return nil
	}

	fmt.Printf("Updated watch folder %d\n", id)
	return nil
}

// ── watch remove ────────────────────────────────────────────────────────

var watchRemoveCmd = &cobra.Command{
	Use:   "remove <id>",
	Short: "Remove a watch folder",
	Long: `Stop watching a folder and remove its configuration.

This does not delete any clips that were previously imported from the folder.`,
	Example: `  # Remove watch folder 1
  mp watch remove 1`,
	Args: cobra.ExactArgs(1),
	RunE: runWatchRemove,
}

func runWatchRemove(cmd *cobra.Command, args []string) error {
	c, err := client.New()
	if err != nil {
		return err
	}

	id, err := strconv.ParseInt(args[0], 10, 64)
	if err != nil {
		return fmt.Errorf("invalid watch folder ID: %s", args[0])
	}

	if err := c.Delete(fmt.Sprintf("/api/v1/watch/%d", id)); err != nil {
		return err
	}

	if jsonOutput {
		printJSON(map[string]interface{}{"id": id, "removed": true})
		return nil
	}

	fmt.Printf("Removed watch folder %d\n", id)
	return nil
}

// ── watch pause ─────────────────────────────────────────────────────────

var watchPauseCmd = &cobra.Command{
	Use:   "pause [id]",
	Short: "Pause a watch folder or all folders",
	Long: `Pause file watching for a specific folder or globally.

When paused, new files in the folder are not imported. Use --global to
pause all watch folders at once.`,
	Example: `  # Pause a specific folder
  mp watch pause 1

  # Pause all watch folders
  mp watch pause --global`,
	RunE: runWatchPause,
}

var watchPauseGlobal bool

func init() {
	watchPauseCmd.Flags().BoolVar(&watchPauseGlobal, "global", false, "Pause all watch folders")
}

func runWatchPause(cmd *cobra.Command, args []string) error {
	c, err := client.New()
	if err != nil {
		return err
	}

	if watchPauseGlobal {
		if err := c.PutJSON("/api/v1/watch/global-pause", nil, nil); err != nil {
			return err
		}

		if jsonOutput {
			printJSON(map[string]interface{}{"global_paused": true})
			return nil
		}

		fmt.Println("Paused all watch folders")
		return nil
	}

	if len(args) == 0 {
		return fmt.Errorf("watch folder ID is required (or use --global)")
	}

	id, err := strconv.ParseInt(args[0], 10, 64)
	if err != nil {
		return fmt.Errorf("invalid watch folder ID: %s", args[0])
	}

	if err := c.PutJSON(fmt.Sprintf("/api/v1/watch/%d/pause", id), nil, nil); err != nil {
		return err
	}

	if jsonOutput {
		printJSON(map[string]interface{}{"id": id, "paused": true})
		return nil
	}

	fmt.Printf("Paused watch folder %d\n", id)
	return nil
}

// ── watch resume ────────────────────────────────────────────────────────

var watchResumeCmd = &cobra.Command{
	Use:   "resume [id]",
	Short: "Resume a watch folder or all folders",
	Long: `Resume file watching for a paused folder or globally.

Use --global to resume all watch folders at once.`,
	Example: `  # Resume a specific folder
  mp watch resume 1

  # Resume all watch folders
  mp watch resume --global`,
	RunE: runWatchResume,
}

var watchResumeGlobal bool

func init() {
	watchResumeCmd.Flags().BoolVar(&watchResumeGlobal, "global", false, "Resume all watch folders")
}

func runWatchResume(cmd *cobra.Command, args []string) error {
	c, err := client.New()
	if err != nil {
		return err
	}

	if watchResumeGlobal {
		if err := c.Delete("/api/v1/watch/global-pause"); err != nil {
			return err
		}

		if jsonOutput {
			printJSON(map[string]interface{}{"global_paused": false})
			return nil
		}

		fmt.Println("Resumed all watch folders")
		return nil
	}

	if len(args) == 0 {
		return fmt.Errorf("watch folder ID is required (or use --global)")
	}

	id, err := strconv.ParseInt(args[0], 10, 64)
	if err != nil {
		return fmt.Errorf("invalid watch folder ID: %s", args[0])
	}

	if err := c.Delete(fmt.Sprintf("/api/v1/watch/%d/pause", id)); err != nil {
		return err
	}

	if jsonOutput {
		printJSON(map[string]interface{}{"id": id, "resumed": true})
		return nil
	}

	fmt.Printf("Resumed watch folder %d\n", id)
	return nil
}

// ── watch status ────────────────────────────────────────────────────────

var watchStatusCmd = &cobra.Command{
	Use:   "status",
	Short: "Show watch folder system status",
	Long: `Display the overall status of the watch folder system.

Shows whether watching is globally paused, how many folders are active,
and the total count of configured folders.`,
	Example: `  # Show watch status
  mp watch status

  # Show status as JSON
  mp watch status --json`,
	RunE: runWatchStatus,
}

func runWatchStatus(cmd *cobra.Command, args []string) error {
	c, err := client.New()
	if err != nil {
		return err
	}

	var status struct {
		GlobalPaused bool     `json:"global_paused"`
		Active       int      `json:"active"`
		Total        int      `json:"total"`
		Watching     []string `json:"watching"`
	}

	if err := c.GetJSON("/api/v1/watch/status", &status); err != nil {
		return err
	}

	if jsonOutput {
		printJSON(status)
		return nil
	}

	globalPaused := "no"
	if status.GlobalPaused {
		globalPaused = "yes"
	}

	pairs := [][2]string{
		{"Global Paused", globalPaused},
		{"Active", strconv.Itoa(status.Active)},
		{"Total", strconv.Itoa(status.Total)},
		{"Watching", strings.Join(status.Watching, ", ")},
	}
	if len(status.Watching) == 0 {
		pairs[3] = [2]string{"Watching", "-"}
	}

	printKeyValue(pairs)
	return nil
}

// ── watch process ───────────────────────────────────────────────────────

var watchProcessCmd = &cobra.Command{
	Use:   "process <id>",
	Short: "Process existing files in a watch folder",
	Long: `Trigger a scan of existing files in a watch folder.

Normally, watch folders only detect new files. This command imports any
existing files that haven't been imported yet.`,
	Example: `  # Process existing files in folder 1
  mp watch process 1`,
	Args: cobra.ExactArgs(1),
	RunE: runWatchProcess,
}

func runWatchProcess(cmd *cobra.Command, args []string) error {
	c, err := client.New()
	if err != nil {
		return err
	}

	id, err := strconv.ParseInt(args[0], 10, 64)
	if err != nil {
		return fmt.Errorf("invalid watch folder ID: %s", args[0])
	}

	if err := c.PostJSON(fmt.Sprintf("/api/v1/watch/%d/process", id), nil, nil); err != nil {
		return err
	}

	if jsonOutput {
		printJSON(map[string]interface{}{"id": id, "processing": true})
		return nil
	}

	fmt.Printf("Processing existing files in folder %d\n", id)
	return nil
}

// ── registration ────────────────────────────────────────────────────────

func init() {
	watchCmd.AddCommand(watchListCmd)
	watchCmd.AddCommand(watchAddCmd)
	watchCmd.AddCommand(watchUpdateCmd)
	watchCmd.AddCommand(watchRemoveCmd)
	watchCmd.AddCommand(watchPauseCmd)
	watchCmd.AddCommand(watchResumeCmd)
	watchCmd.AddCommand(watchStatusCmd)
	watchCmd.AddCommand(watchProcessCmd)
}
