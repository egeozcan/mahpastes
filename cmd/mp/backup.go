package main

import (
	"fmt"
	"io"
	"os"

	"github.com/spf13/cobra"

	"go-clipboard/cmd/mp/client"
)

// ── backup (parent) ─────────────────────────────────────────────────────

var backupCmd = &cobra.Command{
	Use:   "backup",
	Short: "Backup and restore mahpastes data",
	Long: `Create and restore full backups of your mahpastes data.

Backups are ZIP archives containing the database and all clip files.`,
}

// ── backup create ───────────────────────────────────────────────────────

var backupCreateCmd = &cobra.Command{
	Use:   "create <path>",
	Short: "Download a backup to a file",
	Long: `Create a backup of all mahpastes data and save it to the specified path.

The backup is a ZIP archive containing the database and all clip files.
Use "-" as the path to write the backup to stdout (for piping).`,
	Example: `  # Save backup to a file
  mp backup create ~/backups/mahpastes.zip

  # Pipe backup to another command
  mp backup create - | aws s3 cp - s3://bucket/backup.zip

  # Save with JSON output showing size
  mp backup create backup.zip --json`,
	Args: cobra.ExactArgs(1),
	RunE: runBackupCreate,
}

func runBackupCreate(cmd *cobra.Command, args []string) error {
	c, err := client.New()
	if err != nil {
		return err
	}

	outputPath := args[0]

	body, _, err := c.GetRaw("/api/v1/backup")
	if err != nil {
		return err
	}
	defer body.Close()

	if outputPath == "-" {
		// Write to stdout
		n, err := io.Copy(os.Stdout, body)
		if err != nil {
			return fmt.Errorf("write to stdout: %w", err)
		}
		// Only print status to stderr when piping
		if jsonOutput {
			// JSON to stderr so it doesn't mix with binary data
			fmt.Fprintf(os.Stderr, `{"bytes":%d}`+"\n", n)
		}
		return nil
	}

	f, err := os.Create(outputPath)
	if err != nil {
		return fmt.Errorf("create output file: %w", err)
	}
	defer f.Close()

	n, err := io.Copy(f, body)
	if err != nil {
		return fmt.Errorf("write backup file: %w", err)
	}

	if jsonOutput {
		printJSON(map[string]interface{}{
			"path":  outputPath,
			"bytes": n,
		})
		return nil
	}

	fmt.Printf("Backup saved to %s (%s)\n", outputPath, formatSize(n))
	return nil
}

// ── backup restore ──────────────────────────────────────────────────────

var backupRestoreCmd = &cobra.Command{
	Use:   "restore <path>",
	Short: "Restore from a backup file",
	Long: `Restore mahpastes data from a backup ZIP archive.

This replaces all current data with the contents of the backup.
Use "-" as the path to read the backup from stdin (for piping).`,
	Example: `  # Restore from a file
  mp backup restore ~/backups/mahpastes.zip

  # Restore from stdin
  cat backup.zip | mp backup restore -

  # Restore from S3
  aws s3 cp s3://bucket/backup.zip - | mp backup restore -`,
	Args: cobra.ExactArgs(1),
	RunE: runBackupRestore,
}

func runBackupRestore(cmd *cobra.Command, args []string) error {
	c, err := client.New()
	if err != nil {
		return err
	}

	inputPath := args[0]

	var reader io.Reader
	var filename string

	if inputPath == "-" {
		reader = os.Stdin
		filename = "backup.zip"
	} else {
		f, err := os.Open(inputPath)
		if err != nil {
			return fmt.Errorf("open backup file: %w", err)
		}
		defer f.Close()
		reader = f
		filename = inputPath
	}

	if err := c.UploadFile("/api/v1/backup/restore", filename, reader, nil, nil); err != nil {
		return err
	}

	if jsonOutput {
		printJSON(map[string]interface{}{
			"restored": true,
			"path":     inputPath,
		})
		return nil
	}

	fmt.Println("Backup restored successfully.")
	return nil
}

// ── init ────────────────────────────────────────────────────────────────

func init() {
	backupCmd.AddCommand(backupCreateCmd)
	backupCmd.AddCommand(backupRestoreCmd)
}
