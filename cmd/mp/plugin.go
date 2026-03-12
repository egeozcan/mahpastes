package main

import (
	"fmt"
	"strings"

	"github.com/spf13/cobra"

	"go-clipboard/cmd/mp/client"
)

// ── plugin (parent) ─────────────────────────────────────────────────────

var pluginCmd = &cobra.Command{
	Use:   "plugin",
	Short: "Manage plugins",
	Long: `Install, enable, disable, and remove Lua plugins.

Plugins extend mahpastes with custom actions, event handlers, and
scheduled tasks. Each plugin runs in a sandboxed Lua environment.`,
}

// ── plugin list ─────────────────────────────────────────────────────────

var pluginListCmd = &cobra.Command{
	Use:   "list",
	Short: "List installed plugins",
	Long:  `Display all installed plugins with their status and version.`,
	Example: `  # List all plugins
  mp plugin list

  # List as JSON
  mp plugin list --json`,
	RunE: runPluginList,
}

func runPluginList(cmd *cobra.Command, args []string) error {
	c, err := client.New()
	if err != nil {
		return err
	}

	var plugins []struct {
		ID      string `json:"id"`
		Name    string `json:"name"`
		Version string `json:"version"`
		Enabled bool   `json:"enabled"`
		Status  string `json:"status"`
	}

	if err := c.GetJSON("/api/v1/plugins", &plugins); err != nil {
		return err
	}

	if jsonOutput {
		printJSON(plugins)
		return nil
	}

	if len(plugins) == 0 {
		fmt.Println("No plugins installed.")
		return nil
	}

	headers := []string{"ID", "NAME", "VERSION", "ENABLED", "STATUS"}
	rows := make([][]string, len(plugins))
	for i, p := range plugins {
		enabled := "no"
		if p.Enabled {
			enabled = "yes"
		}
		version := p.Version
		if version == "" {
			version = "-"
		}
		status := p.Status
		if status == "" {
			status = "-"
		}
		rows[i] = []string{
			p.ID,
			p.Name,
			version,
			enabled,
			status,
		}
	}

	printTable(headers, rows)
	return nil
}

// ── plugin install ──────────────────────────────────────────────────────

var pluginInstallCmd = &cobra.Command{
	Use:   "install <source>",
	Short: "Install a plugin from a URL or file path",
	Long: `Install a new plugin from a URL or local file path.

The source can be:
  - A URL to a .lua file or plugin repository
  - A local file path to a .lua plugin file`,
	Example: `  # Install from URL
  mp plugin install https://example.com/my-plugin.lua

  # Install from local file
  mp plugin install ./plugins/my-plugin.lua`,
	Args: cobra.ExactArgs(1),
	RunE: runPluginInstall,
}

func runPluginInstall(cmd *cobra.Command, args []string) error {
	c, err := client.New()
	if err != nil {
		return err
	}

	body := map[string]string{"source": args[0]}

	var result struct {
		ID   string `json:"id"`
		Name string `json:"name"`
	}

	if err := c.PostJSON("/api/v1/plugins", body, &result); err != nil {
		return err
	}

	if jsonOutput {
		printJSON(result)
		return nil
	}

	fmt.Printf("Installed: %s (ID: %s)\n", result.Name, result.ID)
	return nil
}

// ── plugin remove ───────────────────────────────────────────────────────

var pluginRemoveCmd = &cobra.Command{
	Use:   "remove <id>",
	Short: "Remove an installed plugin",
	Long: `Uninstall a plugin and remove its files.

This does not delete any clips or data created by the plugin.`,
	Example: `  # Remove a plugin
  mp plugin remove my-plugin`,
	Args: cobra.ExactArgs(1),
	RunE: runPluginRemove,
}

func runPluginRemove(cmd *cobra.Command, args []string) error {
	c, err := client.New()
	if err != nil {
		return err
	}

	id := args[0]

	if err := c.Delete(fmt.Sprintf("/api/v1/plugins/%s", id)); err != nil {
		return err
	}

	if jsonOutput {
		printJSON(map[string]interface{}{"id": id, "removed": true})
		return nil
	}

	fmt.Printf("Removed plugin %s\n", id)
	return nil
}

// ── plugin enable ───────────────────────────────────────────────────────

var pluginEnableCmd = &cobra.Command{
	Use:   "enable <id>",
	Short: "Enable a disabled plugin",
	Long:  `Enable a plugin so it can handle events and run actions.`,
	Example: `  mp plugin enable my-plugin`,
	Args:    cobra.ExactArgs(1),
	RunE:    runPluginEnable,
}

func runPluginEnable(cmd *cobra.Command, args []string) error {
	c, err := client.New()
	if err != nil {
		return err
	}

	id := args[0]

	if err := c.PutJSON(fmt.Sprintf("/api/v1/plugins/%s/enable", id), nil, nil); err != nil {
		return err
	}

	if jsonOutput {
		printJSON(map[string]interface{}{"id": id, "enabled": true})
		return nil
	}

	fmt.Printf("Enabled plugin %s\n", id)
	return nil
}

// ── plugin disable ──────────────────────────────────────────────────────

var pluginDisableCmd = &cobra.Command{
	Use:   "disable <id>",
	Short: "Disable an enabled plugin",
	Long:  `Disable a plugin. It remains installed but will not handle events or run actions.`,
	Example: `  mp plugin disable my-plugin`,
	Args:    cobra.ExactArgs(1),
	RunE:    runPluginDisable,
}

func runPluginDisable(cmd *cobra.Command, args []string) error {
	c, err := client.New()
	if err != nil {
		return err
	}

	id := args[0]

	if err := c.PutJSON(fmt.Sprintf("/api/v1/plugins/%s/disable", id), nil, nil); err != nil {
		return err
	}

	if jsonOutput {
		printJSON(map[string]interface{}{"id": id, "disabled": true})
		return nil
	}

	fmt.Printf("Disabled plugin %s\n", id)
	return nil
}

// ── plugin run ──────────────────────────────────────────────────────────

var pluginRunCmd = &cobra.Command{
	Use:   "run <plugin-id> <action-id>",
	Short: "Execute a plugin action",
	Long: `Run a specific action defined by a plugin.

Use --clip to pass clip IDs to the action and --option to set
action option values in key=value format.`,
	Example: `  # Run an action
  mp plugin run my-plugin process-image --clip 42

  # Run with multiple clips and options
  mp plugin run my-plugin transform --clip 1 --clip 2 --option quality=high --option format=png

  # Run and show result as JSON
  mp plugin run my-plugin analyze --clip 42 --json`,
	Args: cobra.ExactArgs(2),
	RunE: runPluginRun,
}

var (
	pluginRunClips   []int64
	pluginRunOptions []string
)

func init() {
	pluginRunCmd.Flags().Int64SliceVar(&pluginRunClips, "clip", nil, "Clip ID(s) to pass to the action (repeatable)")
	pluginRunCmd.Flags().StringArrayVar(&pluginRunOptions, "option", nil, "Action option in key=value format (repeatable)")
}

func runPluginRun(cmd *cobra.Command, args []string) error {
	c, err := client.New()
	if err != nil {
		return err
	}

	pluginID := args[0]
	actionID := args[1]

	body := map[string]interface{}{}

	if len(pluginRunClips) > 0 {
		body["clip_ids"] = pluginRunClips
	}

	if len(pluginRunOptions) > 0 {
		opts := map[string]interface{}{}
		for _, opt := range pluginRunOptions {
			parts := strings.SplitN(opt, "=", 2)
			if len(parts) != 2 {
				return fmt.Errorf("invalid option format (expected key=value): %s", opt)
			}
			opts[parts[0]] = parts[1]
		}
		body["options"] = opts
	}

	var result map[string]interface{}

	if err := c.PostJSON(fmt.Sprintf("/api/v1/plugins/%s/actions/%s", pluginID, actionID), body, &result); err != nil {
		return err
	}

	if jsonOutput {
		printJSON(result)
		return nil
	}

	// Print human-readable result
	if success, ok := result["success"].(bool); ok && success {
		fmt.Println("Action completed successfully.")
	} else if errMsg, ok := result["error"].(string); ok && errMsg != "" {
		fmt.Printf("Action failed: %s\n", errMsg)
	}

	if clipID, ok := result["result_clip_id"]; ok && clipID != nil {
		fmt.Printf("Result clip ID: %v\n", clipID)
	}

	return nil
}

// ── plugin storage (parent) ─────────────────────────────────────────────

var pluginStorageCmd = &cobra.Command{
	Use:   "storage",
	Short: "Manage plugin key-value storage",
	Long: `View and modify a plugin's sandboxed key-value storage.

Each plugin has isolated storage for configuration and state.`,
}

// ── plugin storage list ─────────────────────────────────────────────────

var pluginStorageListCmd = &cobra.Command{
	Use:   "list <plugin-id>",
	Short: "List all storage keys for a plugin",
	Long:  `Display all key-value pairs stored by a plugin.`,
	Example: `  # List storage for a plugin
  mp plugin storage list my-plugin

  # List as JSON
  mp plugin storage list my-plugin --json`,
	Args: cobra.ExactArgs(1),
	RunE: runPluginStorageList,
}

func runPluginStorageList(cmd *cobra.Command, args []string) error {
	c, err := client.New()
	if err != nil {
		return err
	}

	pluginID := args[0]

	var storage map[string]string
	if err := c.GetJSON(fmt.Sprintf("/api/v1/plugins/%s/storage", pluginID), &storage); err != nil {
		return err
	}

	if jsonOutput {
		printJSON(storage)
		return nil
	}

	if len(storage) == 0 {
		fmt.Println("No storage entries.")
		return nil
	}

	pairs := make([][2]string, 0, len(storage))
	for k, v := range storage {
		pairs = append(pairs, [2]string{k, v})
	}

	printKeyValue(pairs)
	return nil
}

// ── plugin storage get ──────────────────────────────────────────────────

var pluginStorageGetCmd = &cobra.Command{
	Use:   "get <plugin-id> <key>",
	Short: "Get a storage value",
	Long:  `Get the value of a specific key from a plugin's storage.`,
	Example: `  mp plugin storage get my-plugin api_key
  mp plugin storage get my-plugin api_key --json`,
	Args: cobra.ExactArgs(2),
	RunE: runPluginStorageGet,
}

func runPluginStorageGet(cmd *cobra.Command, args []string) error {
	c, err := client.New()
	if err != nil {
		return err
	}

	pluginID := args[0]
	key := args[1]

	var result struct {
		Value string `json:"value"`
	}
	if err := c.GetJSON(fmt.Sprintf("/api/v1/plugins/%s/storage/%s", pluginID, key), &result); err != nil {
		return err
	}

	if jsonOutput {
		printJSON(map[string]string{key: result.Value})
		return nil
	}

	fmt.Println(result.Value)
	return nil
}

// ── plugin storage set ──────────────────────────────────────────────────

var pluginStorageSetCmd = &cobra.Command{
	Use:   "set <plugin-id> <key> <value>",
	Short: "Set a storage value",
	Long:  `Set a key-value pair in a plugin's storage.`,
	Example: `  mp plugin storage set my-plugin api_key sk-12345
  mp plugin storage set my-plugin threshold "0.8"`,
	Args: cobra.ExactArgs(3),
	RunE: runPluginStorageSet,
}

func runPluginStorageSet(cmd *cobra.Command, args []string) error {
	c, err := client.New()
	if err != nil {
		return err
	}

	pluginID := args[0]
	key := args[1]
	value := args[2]

	body := map[string]string{"value": value}
	if err := c.PutJSON(fmt.Sprintf("/api/v1/plugins/%s/storage/%s", pluginID, key), body, nil); err != nil {
		return err
	}

	if jsonOutput {
		printJSON(map[string]string{key: value})
		return nil
	}

	fmt.Printf("Set %s for plugin %s\n", key, pluginID)
	return nil
}

// ── plugin update ───────────────────────────────────────────────────────

var pluginUpdateCmd = &cobra.Command{
	Use:   "update [id]",
	Short: "Update a plugin or check for updates",
	Long: `Update a specific plugin to its latest version, or check all plugins
for available updates.

Use --check to see which plugins have updates available without
actually updating them.`,
	Example: `  # Check all plugins for updates
  mp plugin update --check

  # Update a specific plugin
  mp plugin update my-plugin`,
	RunE: runPluginUpdate,
}

var pluginUpdateCheck bool

func init() {
	pluginUpdateCmd.Flags().BoolVar(&pluginUpdateCheck, "check", false, "Check for available updates without installing")
}

func runPluginUpdate(cmd *cobra.Command, args []string) error {
	c, err := client.New()
	if err != nil {
		return err
	}

	if pluginUpdateCheck {
		var updates []struct {
			ID             string `json:"id"`
			Name           string `json:"name"`
			CurrentVersion string `json:"current_version"`
			LatestVersion  string `json:"latest_version"`
		}

		if err := c.PostJSON("/api/v1/plugins/check-updates", nil, &updates); err != nil {
			return err
		}

		if jsonOutput {
			printJSON(updates)
			return nil
		}

		if len(updates) == 0 {
			fmt.Println("All plugins are up to date.")
			return nil
		}

		headers := []string{"ID", "NAME", "CURRENT", "LATEST"}
		rows := make([][]string, len(updates))
		for i, u := range updates {
			rows[i] = []string{
				u.ID,
				u.Name,
				u.CurrentVersion,
				u.LatestVersion,
			}
		}

		printTable(headers, rows)
		return nil
	}

	// Update a specific plugin
	if len(args) == 0 {
		return fmt.Errorf("plugin ID is required (or use --check to check for updates)")
	}

	pluginID := args[0]

	var result struct {
		ID      string `json:"id"`
		Name    string `json:"name"`
		Version string `json:"version"`
	}

	if err := c.PostJSON(fmt.Sprintf("/api/v1/plugins/%s/update", pluginID), nil, &result); err != nil {
		return err
	}

	if jsonOutput {
		printJSON(result)
		return nil
	}

	version := result.Version
	if version == "" {
		version = "latest"
	}
	fmt.Printf("Updated plugin %s to %s\n", pluginID, version)
	return nil
}

// ── registration ────────────────────────────────────────────────────────

func init() {
	// Wire storage subcommands
	pluginStorageCmd.AddCommand(pluginStorageListCmd)
	pluginStorageCmd.AddCommand(pluginStorageGetCmd)
	pluginStorageCmd.AddCommand(pluginStorageSetCmd)

	// Wire plugin subcommands
	pluginCmd.AddCommand(pluginListCmd)
	pluginCmd.AddCommand(pluginInstallCmd)
	pluginCmd.AddCommand(pluginRemoveCmd)
	pluginCmd.AddCommand(pluginEnableCmd)
	pluginCmd.AddCommand(pluginDisableCmd)
	pluginCmd.AddCommand(pluginRunCmd)
	pluginCmd.AddCommand(pluginStorageCmd)
	pluginCmd.AddCommand(pluginUpdateCmd)
}
