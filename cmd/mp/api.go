package main

import (
	"fmt"

	"github.com/spf13/cobra"

	"go-clipboard/cmd/mp/client"
)

// ── api (parent) ────────────────────────────────────────────────────────

var apiCmd = &cobra.Command{
	Use:   "api",
	Short: "API connectivity and key management",
	Long: `Check API connectivity and manage API keys.

The API server must be started from the mahpastes desktop app (Settings > API).
API keys are also created and managed from the desktop app.`,
}

// ── api status ──────────────────────────────────────────────────────────

var apiStatusCmd = &cobra.Command{
	Use:   "status",
	Short: "Check API connectivity",
	Long: `Verify that the CLI can connect to the mahpastes API.

Makes a test request to confirm the API is reachable and the key is valid.`,
	Example: `  # Check connection
  mp api status

  # Check connection as JSON
  mp api status --json`,
	RunE: runAPIStatus,
}

func runAPIStatus(cmd *cobra.Command, args []string) error {
	c, err := client.New()
	if err != nil {
		return err
	}

	// Verify connectivity by making a lightweight API call.
	var result struct {
		Clips []interface{} `json:"clips"`
		Total int           `json:"total"`
	}
	if err := c.GetJSON("/api/v1/clips?limit=1", &result); err != nil {
		return err
	}

	if jsonOutput {
		printJSON(map[string]interface{}{
			"connected": true,
			"url":       c.BaseURL,
		})
		return nil
	}

	fmt.Printf("Connected to mahpastes API at %s\n", c.BaseURL)
	return nil
}

// ── api key (parent) ────────────────────────────────────────────────────

var apiKeyCmd = &cobra.Command{
	Use:   "key",
	Short: "Manage API keys",
	Long: `API keys are managed in the mahpastes desktop app under Settings > API.

Use the desktop app to create, list, and revoke API keys.`,
}

// ── api key create ──────────────────────────────────────────────────────

var apiKeyCreateCmd = &cobra.Command{
	Use:   "create <name>",
	Short: "Create an API key (desktop app only)",
	Long: `API keys must be created from the mahpastes desktop application.

Open Settings > API in the desktop app to create a new API key.`,
	Example: `  mp api key create my-key`,
	Args:    cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		fmt.Println("API keys are managed in the mahpastes desktop app under Settings > API.")
		fmt.Println("")
		fmt.Println("To create a key:")
		fmt.Println("  1. Open mahpastes")
		fmt.Println("  2. Go to Settings > API")
		fmt.Println("  3. Click \"Create Key\"")
		return nil
	},
}

// ── api key list ────────────────────────────────────────────────────────

var apiKeyListCmd = &cobra.Command{
	Use:   "list",
	Short: "List API keys (desktop app only)",
	Long: `API keys must be viewed from the mahpastes desktop application.

Open Settings > API in the desktop app to see all API keys.`,
	Example: `  mp api key list`,
	RunE: func(cmd *cobra.Command, args []string) error {
		fmt.Println("API keys are managed in the mahpastes desktop app under Settings > API.")
		fmt.Println("")
		fmt.Println("To view your keys:")
		fmt.Println("  1. Open mahpastes")
		fmt.Println("  2. Go to Settings > API")
		return nil
	},
}

// ── api key revoke ──────────────────────────────────────────────────────

var apiKeyRevokeCmd = &cobra.Command{
	Use:   "revoke <id>",
	Short: "Revoke an API key (desktop app only)",
	Long: `API keys must be revoked from the mahpastes desktop application.

Open Settings > API in the desktop app to revoke an API key.`,
	Example: `  mp api key revoke 3`,
	Args:    cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		fmt.Println("API keys are managed in the mahpastes desktop app under Settings > API.")
		fmt.Println("")
		fmt.Println("To revoke a key:")
		fmt.Println("  1. Open mahpastes")
		fmt.Println("  2. Go to Settings > API")
		fmt.Println("  3. Click the revoke button next to the key")
		return nil
	},
}

// ── init ────────────────────────────────────────────────────────────────

func init() {
	apiKeyCmd.AddCommand(apiKeyCreateCmd)
	apiKeyCmd.AddCommand(apiKeyListCmd)
	apiKeyCmd.AddCommand(apiKeyRevokeCmd)

	apiCmd.AddCommand(apiStatusCmd)
	apiCmd.AddCommand(apiKeyCmd)
}
