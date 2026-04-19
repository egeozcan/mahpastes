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

func init() {
	maintenanceCmd.AddCommand(maintenanceVacuumCmd)
}
