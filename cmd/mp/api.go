package main

import (
	"fmt"
	"os"
	"strconv"

	"github.com/spf13/cobra"

	"go-clipboard/cmd/mp/client"
)

type APIKeyInfo struct {
	ID            int64  `json:"id"`
	Name          string `json:"name"`
	KeyPrefix     string `json:"key_prefix"`
	Role          string `json:"role"`
	ScopedTagID   *int64 `json:"scoped_tag_id"`
	ScopedTagName string `json:"scoped_tag_name"`
	IsRevoked     bool   `json:"is_revoked"`
}

type APIKeyCreateResult struct {
	Key  string     `json:"key"`
	Info APIKeyInfo `json:"info"`
}

// ── api (parent) ────────────────────────────────────────────────────────

var apiCmd = &cobra.Command{
	Use:   "api",
	Short: "API connectivity and key management",
	Long:  `Check API connectivity and manage API keys.`,
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
	Long:  `Create, list, and revoke API keys through the REST API.`,
}

// ── api key create ──────────────────────────────────────────────────────

var apiKeyCreateCmd = &cobra.Command{
	Use:     "create <name>",
	Short:   "Create an API key",
	Example: `  mp api key create my-key`,
	Args:    cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		role, _ := cmd.Flags().GetString("role")
		scopedTagID, _ := cmd.Flags().GetInt64("scoped-tag")
		c, err := client.New()
		if err != nil {
			return err
		}
		var result APIKeyCreateResult
		if err := c.PostJSON("/api/v1/keys", map[string]interface{}{
			"name":          args[0],
			"role":          role,
			"scoped_tag_id": scopedTagID,
		}, &result); err != nil {
			return err
		}
		if jsonOutput {
			printJSON(result)
			return nil
		}
		printKeyValue([][2]string{
			{"id", strconv.FormatInt(result.Info.ID, 10)},
			{"name", result.Info.Name},
			{"role", result.Info.Role},
			{"key", result.Key},
		})
		fmt.Fprintln(os.Stderr, "(save this key - it cannot be retrieved again)")
		return nil
	},
}

// ── api key list ────────────────────────────────────────────────────────

var apiKeyListCmd = &cobra.Command{
	Use:     "list",
	Short:   "List API keys",
	Example: `  mp api key list`,
	RunE: func(cmd *cobra.Command, args []string) error {
		c, err := client.New()
		if err != nil {
			return err
		}
		var keys []APIKeyInfo
		if err := c.GetJSON("/api/v1/keys", &keys); err != nil {
			return err
		}
		if jsonOutput {
			printJSON(keys)
			return nil
		}
		rows := make([][]string, 0, len(keys))
		for _, k := range keys {
			status := "active"
			if k.IsRevoked {
				status = "revoked"
			}
			scope := ""
			if k.ScopedTagID != nil {
				scope = k.ScopedTagName
			}
			rows = append(rows, []string{
				strconv.FormatInt(k.ID, 10),
				k.Name,
				k.Role,
				k.KeyPrefix,
				scope,
				status,
			})
		}
		printTable([]string{"ID", "NAME", "ROLE", "PREFIX", "SCOPE", "STATUS"}, rows)
		return nil
	},
}

// ── api key revoke ──────────────────────────────────────────────────────

var apiKeyRevokeCmd = &cobra.Command{
	Use:     "revoke <id>",
	Short:   "Revoke an API key",
	Example: `  mp api key revoke 3`,
	Args:    cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		id, err := strconv.ParseInt(args[0], 10, 64)
		if err != nil {
			return fmt.Errorf("invalid key id: %s", args[0])
		}
		c, err := client.New()
		if err != nil {
			return err
		}
		if err := c.Delete(fmt.Sprintf("/api/v1/keys/%d", id)); err != nil {
			return err
		}
		if jsonOutput {
			printJSON(map[string]interface{}{"revoked": id})
			return nil
		}
		fmt.Printf("Revoked API key %d\n", id)
		return nil
	},
}

// ── init ────────────────────────────────────────────────────────────────

func init() {
	apiKeyCreateCmd.Flags().String("role", "viewer", "Role for the new key: viewer, editor, or admin")
	apiKeyCreateCmd.Flags().Int64("scoped-tag", 0, "Restrict the key to a tag ID")

	apiKeyCmd.AddCommand(apiKeyCreateCmd)
	apiKeyCmd.AddCommand(apiKeyListCmd)
	apiKeyCmd.AddCommand(apiKeyRevokeCmd)

	apiCmd.AddCommand(apiStatusCmd)
	apiCmd.AddCommand(apiKeyCmd)
}
