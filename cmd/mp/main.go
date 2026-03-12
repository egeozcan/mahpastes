package main

import (
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

By default, mp connects to http://localhost:44557.`,
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
}

func main() {
	if err := rootCmd.Execute(); err != nil {
		exitCode := client.ExitError
		if apiErr, ok := err.(*client.APIError); ok {
			errorf("%s", apiErr.Message)
			exitCode = apiErr.ExitCode()
		} else {
			errorf("%s", err)
		}
		os.Exit(exitCode)
	}
}
