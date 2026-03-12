package main

import (
	"fmt"
	"strconv"

	"github.com/spf13/cobra"

	"go-clipboard/cmd/mp/client"
)

// ── serve (parent) ──────────────────────────────────────────────────────

var serveCmd = &cobra.Command{
	Use:   "serve",
	Short: "Manage tag HTTP servers",
	Long: `Start, stop, and list HTTP servers that serve tag contents as web pages.

Each served tag gets its own HTTP server on a specified port. HTML clips
become web pages, and other clips are served as static assets.`,
}

// ── serve list ──────────────────────────────────────────────────────────

var serveListCmd = &cobra.Command{
	Use:   "list",
	Short: "List running tag servers",
	Long:  `Display all currently running tag HTTP servers with their port, URL, and API access mode.`,
	Example: `  # List all running servers
  mp serve list

  # Get server list as JSON
  mp serve list --json`,
	RunE: runServeList,
}

func runServeList(cmd *cobra.Command, args []string) error {
	c, err := client.New()
	if err != nil {
		return err
	}

	var result struct {
		Servers []struct {
			TagID        int64  `json:"tag_id"`
			TagName      string `json:"tag_name"`
			Port         int    `json:"port"`
			BindAll      bool   `json:"bind_all"`
			URL          string `json:"url"`
			Running      bool   `json:"running"`
			RequestCount int64  `json:"request_count"`
			ApiAccess    string `json:"api_access"`
		} `json:"servers"`
	}

	if err := c.GetJSON("/api/v1/serve", &result); err != nil {
		return err
	}

	if jsonOutput {
		printJSON(result)
		return nil
	}

	if len(result.Servers) == 0 {
		fmt.Println("No servers running.")
		return nil
	}

	headers := []string{"TAG ID", "TAG NAME", "PORT", "URL", "API ACCESS", "REQUESTS"}
	rows := make([][]string, len(result.Servers))
	for i, s := range result.Servers {
		rows[i] = []string{
			strconv.FormatInt(s.TagID, 10),
			s.TagName,
			strconv.Itoa(s.Port),
			s.URL,
			s.ApiAccess,
			strconv.FormatInt(s.RequestCount, 10),
		}
	}

	printTable(headers, rows)
	return nil
}

// ── serve start ─────────────────────────────────────────────────────────

var serveStartCmd = &cobra.Command{
	Use:   "start <tag>",
	Short: "Start serving a tag over HTTP",
	Long: `Start an HTTP server for the specified tag, making its clips accessible as web pages.

The tag argument can be a tag name or numeric ID. An available port is
auto-assigned unless --port is specified.`,
	Example: `  # Start serving with auto-assigned port
  mp serve start my-site

  # Start on a specific port
  mp serve start my-site --port 8080

  # Start with read-write JSON API access
  mp serve start my-site --port 8080 --api-access readwrite

  # Bind to all interfaces (not just localhost)
  mp serve start my-site --bind-all`,
	Args: cobra.ExactArgs(1),
	RunE: runServeStart,
}

var (
	serveStartPort      int
	serveStartBindAll   bool
	serveStartApiAccess string
)

func runServeStart(cmd *cobra.Command, args []string) error {
	c, err := client.New()
	if err != nil {
		return err
	}

	tagID, err := c.ResolveTagID(args[0])
	if err != nil {
		return err
	}

	body := map[string]interface{}{
		"tag_id":     tagID,
		"port":       serveStartPort,
		"bind_all":   serveStartBindAll,
		"api_access": serveStartApiAccess,
	}

	var info struct {
		TagID        int64  `json:"tag_id"`
		TagName      string `json:"tag_name"`
		Port         int    `json:"port"`
		BindAll      bool   `json:"bind_all"`
		URL          string `json:"url"`
		Running      bool   `json:"running"`
		RequestCount int64  `json:"request_count"`
		ApiAccess    string `json:"api_access"`
	}

	if err := c.PostJSON("/api/v1/serve", body, &info); err != nil {
		return err
	}

	if jsonOutput {
		printJSON(info)
		return nil
	}

	fmt.Printf("Serving %s at %s\n", info.TagName, info.URL)
	return nil
}

// ── serve stop ──────────────────────────────────────────────────────────

var serveStopCmd = &cobra.Command{
	Use:   "stop <tag>",
	Short: "Stop serving a tag",
	Long: `Stop the HTTP server for the specified tag.

The tag argument can be a tag name or numeric ID.`,
	Example: `  # Stop serving by tag name
  mp serve stop my-site

  # Stop serving by tag ID
  mp serve stop 5`,
	Args: cobra.ExactArgs(1),
	RunE: runServeStop,
}

func runServeStop(cmd *cobra.Command, args []string) error {
	c, err := client.New()
	if err != nil {
		return err
	}

	tagID, err := c.ResolveTagID(args[0])
	if err != nil {
		return err
	}

	if err := c.Delete(fmt.Sprintf("/api/v1/serve/%d", tagID)); err != nil {
		return err
	}

	if jsonOutput {
		printJSON(map[string]interface{}{"tag_id": tagID, "stopped": true})
		return nil
	}

	fmt.Printf("Stopped serving %s\n", args[0])
	return nil
}

// ── init ────────────────────────────────────────────────────────────────

func init() {
	serveStartCmd.Flags().IntVar(&serveStartPort, "port", 0, "Port to serve on (0 for auto-assign)")
	serveStartCmd.Flags().BoolVar(&serveStartBindAll, "bind-all", false, "Bind to all interfaces (not just localhost)")
	serveStartCmd.Flags().StringVar(&serveStartApiAccess, "api-access", "none", "JSON API access level: none, read, or readwrite")

	serveCmd.AddCommand(serveListCmd)
	serveCmd.AddCommand(serveStartCmd)
	serveCmd.AddCommand(serveStopCmd)
}
