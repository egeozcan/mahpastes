package main

import (
	"errors"
	"os"

	"github.com/spf13/cobra"

	"go-clipboard/cmd/mp/client"
)

var rootCmd = &cobra.Command{
	Use:   "mp",
	Short: "Command-line interface for mahpastes",
	Long: `mp is a command-line interface for the mahpastes clipboard manager.

It communicates with the mahpastes REST API to manage clips, tags,
and other resources from your terminal.

Getting started:
  1. Open mahpastes and enable the API in Settings > API
  2. Create an API key in Settings > API
  3. Export your key:  export MP_API_KEY=<your-key>
  4. (Optional) Set a custom URL:  export MP_API_URL=http://host:port

By default, mp connects to http://localhost:44557.

Environment variables:
  MP_API_KEY   API key (required)
  MP_API_URL   API base URL (default http://localhost:44557)
  MAHPASTES_API_KEY, MAHPASTES_API_URL
               Fallbacks used when the MP_ variables are unset
               (the same names the Claude Code plugin configures).`,
	Version:       versionString(),
	SilenceUsage:  true,
	SilenceErrors: true,
}

func init() {
	rootCmd.PersistentFlags().BoolVar(&jsonOutput, "json", false, "Output results as JSON")
	rootCmd.AddCommand(clipCmd)
	rootCmd.AddCommand(tagCmd)
	rootCmd.AddCommand(dedupCmd)
	rootCmd.AddCommand(watchCmd)
	rootCmd.AddCommand(pluginCmd)
	rootCmd.AddCommand(serveCmd)
	rootCmd.AddCommand(apiCmd)
	rootCmd.AddCommand(backupCmd)
	rootCmd.AddCommand(clipboardCmd)
	rootCmd.AddCommand(maintenanceCmd)
	rootCmd.AddCommand(linkCmd)
}

func main() {
	if err := registerCompletions(); err != nil {
		errorf("%s", err)
	}
	if err := rootCmd.Execute(); err != nil {
		exitCode := client.ExitError
		errorf("%s", err)
		// errors.As keeps the API error's exit code when a command wrapped it
		// (e.g. "fetch tags: ...").
		var apiErr *client.APIError
		if errors.As(err, &apiErr) {
			exitCode = apiErr.ExitCode()
		}
		os.Exit(exitCode)
	}
}

// versionString renders the resolved version, with the commit when known.
func versionString() string {
	if client.Commit != "" {
		return client.Version + " (" + client.Commit + ")"
	}
	return client.Version
}
