package main

import (
	"fmt"

	"github.com/spf13/cobra"
	"go-clipboard/cmd/mp/client"
)

var maintenanceCmd = &cobra.Command{
	Use:   "maintenance",
	Short: "Database maintenance tools",
}

var maintenanceVacuumCmd = &cobra.Command{
	Use:   "vacuum",
	Short: "Compact the database (VACUUM + ANALYZE)",
	RunE: func(cmd *cobra.Command, args []string) error {
		c, err := client.New()
		if err != nil {
			return err
		}

		var resp struct {
			Before int64 `json:"before"`
			After  int64 `json:"after"`
		}
		if err := c.PostJSON("/api/v1/maintenance/vacuum", nil, &resp); err != nil {
			return err
		}

		if jsonOutput {
			printJSON(resp)
		} else {
			reclaimed := resp.Before - resp.After
			fmt.Printf("Compact complete. Before: %d bytes, After: %d bytes, Reclaimed: %d bytes\n",
				resp.Before, resp.After, reclaimed)
		}
		return nil
	},
}

// ── maintenance stale-files ────────────────────────────────────────────────

var maintenanceStaleFilesCmd = &cobra.Command{
	Use:   "stale-files",
	Short: "List or clean stale temp/share-staging files",
}

var maintenanceStaleFilesListCmd = &cobra.Command{
	Use:   "list",
	Short: "List stale files",
	RunE: func(cmd *cobra.Command, args []string) error {
		c, err := client.New()
		if err != nil {
			return err
		}

		var files []map[string]interface{}
		if err := c.GetJSON("/api/v1/maintenance/stale-files", &files); err != nil {
			return err
		}

		if jsonOutput {
			printJSON(files)
		} else {
			for _, f := range files {
				fmt.Printf("%-20s  %8d B  %6.1fh  %s\n",
					f["source"],
					int64(f["size"].(float64)),
					f["age_hours"].(float64),
					f["name"])
			}
		}
		return nil
	},
}

var maintenanceStaleFilesCleanCmd = &cobra.Command{
	Use:   "clean",
	Short: "Remove stale files",
	RunE: func(cmd *cobra.Command, args []string) error {
		c, err := client.New()
		if err != nil {
			return err
		}

		var resp struct {
			Count int   `json:"count"`
			Bytes int64 `json:"bytes"`
		}
		if err := c.PostJSON("/api/v1/maintenance/stale-files/clean", nil, &resp); err != nil {
			return err
		}

		if jsonOutput {
			printJSON(resp)
		} else {
			fmt.Printf("Removed %d file(s), %d bytes reclaimed\n", resp.Count, resp.Bytes)
		}
		return nil
	},
}

// ── maintenance orphan-rows ────────────────────────────────────────────────

var maintenanceOrphanRowsCmd = &cobra.Command{
	Use:   "orphan-rows",
	Short: "List or clean orphan DB rows",
}

var maintenanceOrphanRowsListCmd = &cobra.Command{
	Use:   "list",
	Short: "List orphan row counts",
	RunE: func(cmd *cobra.Command, args []string) error {
		c, err := client.New()
		if err != nil {
			return err
		}

		var report map[string]int
		if err := c.GetJSON("/api/v1/maintenance/orphan-rows", &report); err != nil {
			return err
		}

		if jsonOutput {
			printJSON(report)
		} else {
			keys := []string{"plugin_storage", "plugin_permissions", "stale_follows", "stale_auto_tags", "stale_hidden_tag_ids"}
			for _, k := range keys {
				fmt.Printf("%-30s %d\n", k, report[k])
			}
		}
		return nil
	},
}

var maintenanceOrphanRowsCleanCmd = &cobra.Command{
	Use:   "clean",
	Short: "Clean orphan DB rows",
	RunE: func(cmd *cobra.Command, args []string) error {
		c, err := client.New()
		if err != nil {
			return err
		}

		var report map[string]int
		if err := c.PostJSON("/api/v1/maintenance/orphan-rows/clean", nil, &report); err != nil {
			return err
		}

		if jsonOutput {
			printJSON(report)
		} else {
			total := 0
			for _, v := range report {
				total += v
			}
			fmt.Printf("Cleaned %d orphan row(s)\n", total)
		}
		return nil
	},
}

func init() {
	maintenanceCmd.AddCommand(maintenanceVacuumCmd)
	maintenanceCmd.AddCommand(maintenanceStaleFilesCmd)
	maintenanceStaleFilesCmd.AddCommand(maintenanceStaleFilesListCmd, maintenanceStaleFilesCleanCmd)
	maintenanceCmd.AddCommand(maintenanceOrphanRowsCmd)
	maintenanceOrphanRowsCmd.AddCommand(maintenanceOrphanRowsListCmd, maintenanceOrphanRowsCleanCmd)
}
