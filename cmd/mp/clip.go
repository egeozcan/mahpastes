package main

import (
	"bufio"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/spf13/cobra"

	"go-clipboard/cmd/mp/client"
)

// clipCmd is the parent command for all clip subcommands.
var clipCmd = &cobra.Command{
	Use:   "clip",
	Short: "Manage clips",
	Long:  `Create, view, modify, and delete clips stored in mahpastes.`,
}

// --- clip list ---

var clipListCmd = &cobra.Command{
	Use:   "list",
	Short: "List clips with optional filtering",
	Long: `List clips stored in mahpastes with optional tag, archive, and content type filters.
Results are paginated. Use --limit and --offset for pagination.`,
	Example: `  # List recent clips
  mp clip list

  # List archived clips tagged "screenshots"
  mp clip list --tag screenshots --archived

  # Search by filename
  mp clip list --search "report"

  # Get all clip IDs as JSON for scripting
  mp clip list --json | jq -r '.clips[].id'

  # List with pagination
  mp clip list --limit 10 --offset 20`,
	RunE: runClipList,
}

var (
	clipListTag         string
	clipListArchived    bool
	clipListLimit       int
	clipListOffset      int
	clipListSearch      string
	clipListContentType string
)

func init() {
	clipListCmd.Flags().StringVar(&clipListTag, "tag", "", "Filter by tag name or ID")
	clipListCmd.Flags().BoolVar(&clipListArchived, "archived", false, "Include archived clips")
	clipListCmd.Flags().IntVar(&clipListLimit, "limit", 50, "Maximum number of results (1-200)")
	clipListCmd.Flags().IntVar(&clipListOffset, "offset", 0, "Pagination offset")
	clipListCmd.Flags().StringVar(&clipListSearch, "search", "", "Filter by filename substring")
	clipListCmd.Flags().StringVar(&clipListContentType, "content-type", "", "Filter by MIME type")
}

func runClipList(cmd *cobra.Command, args []string) error {
	c, err := client.New()
	if err != nil {
		return err
	}

	params := []string{
		fmt.Sprintf("limit=%d", clipListLimit),
		fmt.Sprintf("offset=%d", clipListOffset),
	}

	if clipListTag != "" {
		tagID, err := c.ResolveTagID(clipListTag)
		if err != nil {
			return err
		}
		params = append(params, fmt.Sprintf("tag=%d", tagID))
	}

	if clipListArchived {
		params = append(params, "archived=true")
	}

	if clipListSearch != "" {
		params = append(params, "search="+clipListSearch)
	}

	if clipListContentType != "" {
		params = append(params, "content_type="+clipListContentType)
	}

	path := "/api/v1/clips?" + strings.Join(params, "&")

	var result struct {
		Clips []struct {
			ID          int64  `json:"id"`
			Filename    string `json:"filename"`
			ContentType string `json:"content_type"`
			Size        int64  `json:"size"`
			IsArchived  bool   `json:"is_archived"`
			CreatedAt   string `json:"created_at"`
			Tags        []struct {
				ID   int64  `json:"id"`
				Name string `json:"name"`
			} `json:"tags"`
		} `json:"clips"`
		Total  int `json:"total"`
		Limit  int `json:"limit"`
		Offset int `json:"offset"`
	}

	if err := c.GetJSON(path, &result); err != nil {
		return err
	}

	if jsonOutput {
		printJSON(result)
		return nil
	}

	if len(result.Clips) == 0 {
		fmt.Println("No clips found.")
		return nil
	}

	headers := []string{"ID", "FILENAME", "TYPE", "SIZE", "CREATED", "TAGS"}
	rows := make([][]string, len(result.Clips))

	for i, clip := range result.Clips {
		tagNames := make([]string, len(clip.Tags))
		for j, t := range clip.Tags {
			tagNames[j] = t.Name
		}

		rows[i] = []string{
			strconv.FormatInt(clip.ID, 10),
			clip.Filename,
			clip.ContentType,
			formatSize(clip.Size),
			formatTime(clip.CreatedAt),
			strings.Join(tagNames, ", "),
		}
	}

	printTable(headers, rows)
	fmt.Printf("\nShowing %d of %d clips\n", len(result.Clips), result.Total)

	return nil
}

// --- clip get ---

var clipGetCmd = &cobra.Command{
	Use:   "get <id>",
	Short: "Show clip metadata",
	Long:  `Display detailed metadata for a single clip by its ID.`,
	Example: `  # Show clip details
  mp clip get 42

  # Get clip info as JSON
  mp clip get 42 --json`,
	Args: cobra.ExactArgs(1),
	RunE: runClipGet,
}

func runClipGet(cmd *cobra.Command, args []string) error {
	c, err := client.New()
	if err != nil {
		return err
	}

	id, err := strconv.ParseInt(args[0], 10, 64)
	if err != nil {
		return fmt.Errorf("invalid clip ID: %s", args[0])
	}

	var clip struct {
		ID          int64  `json:"id"`
		Filename    string `json:"filename"`
		ContentType string `json:"content_type"`
		Size        int64  `json:"size"`
		IsArchived  bool   `json:"is_archived"`
		CreatedAt   string `json:"created_at"`
		Tags        []struct {
			ID   int64  `json:"id"`
			Name string `json:"name"`
		} `json:"tags"`
	}

	if err := c.GetJSON(fmt.Sprintf("/api/v1/clips/%d", id), &clip); err != nil {
		return err
	}

	if jsonOutput {
		printJSON(clip)
		return nil
	}

	tagNames := make([]string, len(clip.Tags))
	for i, t := range clip.Tags {
		tagNames[i] = t.Name
	}

	pairs := [][2]string{
		{"ID", strconv.FormatInt(clip.ID, 10)},
		{"Filename", clip.Filename},
		{"Content-Type", clip.ContentType},
		{"Size", formatSize(clip.Size)},
		{"Archived", strconv.FormatBool(clip.IsArchived)},
		{"Created", clip.CreatedAt},
		{"Tags", strings.Join(tagNames, ", ")},
	}

	printKeyValue(pairs)
	return nil
}

// --- clip data ---

var clipDataCmd = &cobra.Command{
	Use:   "data <id>",
	Short: "Output raw clip content to stdout",
	Long: `Download and write the raw binary content of a clip to stdout.
Useful for piping clip data to other commands or saving to a file.`,
	Example: `  # Save clip to a file
  mp clip data 42 > image.png

  # Pipe clip content to another command
  mp clip data 42 | wc -c

  # View text clip content
  mp clip data 42`,
	Args: cobra.ExactArgs(1),
	RunE: runClipData,
}

func runClipData(cmd *cobra.Command, args []string) error {
	c, err := client.New()
	if err != nil {
		return err
	}

	id, err := strconv.ParseInt(args[0], 10, 64)
	if err != nil {
		return fmt.Errorf("invalid clip ID: %s", args[0])
	}

	body, _, err := c.GetRaw(fmt.Sprintf("/api/v1/clips/%d/data", id))
	if err != nil {
		return err
	}
	defer body.Close()

	return copyToStdout(body)
}

// --- clip upload ---

var clipUploadCmd = &cobra.Command{
	Use:   "upload [files...]",
	Short: "Upload files as clips",
	Long: `Upload one or more files to mahpastes as new clips.
If no file arguments are given, reads from stdin (requires --filename).
Optionally tag the uploaded clips and set an expiration duration.`,
	Example: `  # Upload a single file
  mp clip upload photo.png

  # Upload multiple files
  mp clip upload *.png

  # Upload with a tag
  mp clip upload document.pdf --tag research

  # Upload from stdin
  echo "hello" | mp clip upload --filename hello.txt

  # Upload with expiration
  mp clip upload temp.txt --expire 30m`,
	RunE: runClipUpload,
}

var (
	clipUploadTag         string
	clipUploadExpire      string
	clipUploadFilename    string
	clipUploadContentType string
)

func init() {
	clipUploadCmd.Flags().StringVar(&clipUploadTag, "tag", "", "Tag to apply to uploaded clips (name or ID)")
	clipUploadCmd.Flags().StringVar(&clipUploadExpire, "expire", "", "Expiration duration (e.g. 30m, 2h, 7d)")
	clipUploadCmd.Flags().StringVar(&clipUploadFilename, "filename", "", "Filename for stdin input")
	clipUploadCmd.Flags().StringVar(&clipUploadContentType, "content-type", "", "Content type for stdin input")
}

func runClipUpload(cmd *cobra.Command, args []string) error {
	c, err := client.New()
	if err != nil {
		return err
	}

	// Resolve tag ID if provided
	var tagID int64
	if clipUploadTag != "" {
		tagID, err = c.ResolveTagID(clipUploadTag)
		if err != nil {
			return err
		}
	}

	// Parse expiration if provided
	var expireMinutes int
	if clipUploadExpire != "" {
		expireMinutes, err = parseDuration(clipUploadExpire)
		if err != nil {
			return fmt.Errorf("invalid expiration duration: %w", err)
		}
	}

	type uploadedClip struct {
		ID          int64  `json:"id"`
		Filename    string `json:"filename"`
		ContentType string `json:"content_type"`
		Size        int64  `json:"size"`
	}

	var uploaded []uploadedClip

	if len(args) == 0 {
		// Read from stdin
		if clipUploadFilename == "" {
			return fmt.Errorf("--filename is required when reading from stdin")
		}

		extra := map[string]string{}
		if clipUploadContentType != "" {
			extra["content_type"] = clipUploadContentType
		}

		var clip uploadedClip
		if err := c.UploadFile("/api/v1/clips", clipUploadFilename, os.Stdin, extra, &clip); err != nil {
			return err
		}
		uploaded = append(uploaded, clip)
	} else {
		// Upload each file
		for _, filePath := range args {
			f, err := os.Open(filePath)
			if err != nil {
				errorf("skipping %s: %s", filePath, err)
				continue
			}

			filename := filepath.Base(filePath)
			var clip uploadedClip
			uploadErr := c.UploadFile("/api/v1/clips", filename, f, nil, &clip)
			f.Close()
			if uploadErr != nil {
				errorf("failed to upload %s: %s", filePath, uploadErr)
				continue
			}
			uploaded = append(uploaded, clip)
		}
	}

	// Apply tag and expiration to uploaded clips
	for _, clip := range uploaded {
		if tagID > 0 {
			_ = c.PutJSON(fmt.Sprintf("/api/v1/clips/%d/tags/%d", clip.ID, tagID), nil, nil)
		}
		if expireMinutes > 0 {
			_ = c.PutJSON(fmt.Sprintf("/api/v1/clips/%d/expiration", clip.ID), map[string]int{"minutes": expireMinutes}, nil)
		}
	}

	if jsonOutput {
		printJSON(uploaded)
		return nil
	}

	for _, clip := range uploaded {
		fmt.Printf("Uploaded: %s (ID: %d)\n", clip.Filename, clip.ID)
	}

	if len(uploaded) == 0 {
		return fmt.Errorf("no files were uploaded")
	}

	return nil
}

// --- clip delete ---

var clipDeleteCmd = &cobra.Command{
	Use:   "delete <ids...>",
	Short: "Delete one or more clips",
	Long: `Delete clips by their IDs. Supports single and bulk deletion.
Use --stdin to read IDs from stdin (one per line), useful for piping.`,
	Example: `  # Delete a single clip
  mp clip delete 42

  # Delete multiple clips
  mp clip delete 1 2 3

  # Delete clips from a piped list
  mp clip list --json | jq -r '.clips[].id' | mp clip delete --stdin`,
	Args: cobra.ArbitraryArgs,
	RunE: runClipDelete,
}

var clipDeleteStdin bool

func init() {
	clipDeleteCmd.Flags().BoolVar(&clipDeleteStdin, "stdin", false, "Read clip IDs from stdin (one per line)")
}

func runClipDelete(cmd *cobra.Command, args []string) error {
	ids, err := collectIDs(args, clipDeleteStdin)
	if err != nil {
		return err
	}

	c, err := client.New()
	if err != nil {
		return err
	}

	if len(ids) == 1 {
		if err := c.Delete(fmt.Sprintf("/api/v1/clips/%d", ids[0])); err != nil {
			return err
		}
	} else {
		if err := c.PostJSON("/api/v1/clips/bulk/delete", map[string][]int64{"ids": ids}, nil); err != nil {
			return err
		}
	}

	if jsonOutput {
		printJSON(map[string]int{"deleted": len(ids)})
		return nil
	}

	fmt.Printf("Deleted %d clip(s)\n", len(ids))
	return nil
}

// --- clip rename ---

var clipRenameCmd = &cobra.Command{
	Use:   "rename <id> <filename>",
	Short: "Rename a clip",
	Long:  `Change the filename of a clip.`,
	Example: `  # Rename a clip
  mp clip rename 42 new-name.png`,
	Args: cobra.ExactArgs(2),
	RunE: runClipRename,
}

func runClipRename(cmd *cobra.Command, args []string) error {
	c, err := client.New()
	if err != nil {
		return err
	}

	id, err := strconv.ParseInt(args[0], 10, 64)
	if err != nil {
		return fmt.Errorf("invalid clip ID: %s", args[0])
	}

	filename := args[1]

	if err := c.PatchJSON(fmt.Sprintf("/api/v1/clips/%d", id), map[string]string{"filename": filename}, nil); err != nil {
		return err
	}

	if jsonOutput {
		printJSON(map[string]interface{}{"id": id, "filename": filename})
		return nil
	}

	fmt.Printf("Renamed clip %d to %s\n", id, filename)
	return nil
}

// --- clip archive ---

var clipArchiveCmd = &cobra.Command{
	Use:   "archive <ids...>",
	Short: "Archive one or more clips",
	Long: `Archive clips by their IDs. Archived clips are hidden from the default list view.
Use --stdin to read IDs from stdin (one per line).`,
	Example: `  # Archive a single clip
  mp clip archive 42

  # Archive multiple clips
  mp clip archive 1 2 3

  # Archive from a piped list
  mp clip list --json | jq -r '.clips[].id' | mp clip archive --stdin`,
	Args: cobra.ArbitraryArgs,
	RunE: runClipArchive,
}

var clipArchiveStdin bool

func init() {
	clipArchiveCmd.Flags().BoolVar(&clipArchiveStdin, "stdin", false, "Read clip IDs from stdin (one per line)")
}

func runClipArchive(cmd *cobra.Command, args []string) error {
	ids, err := collectIDs(args, clipArchiveStdin)
	if err != nil {
		return err
	}

	c, err := client.New()
	if err != nil {
		return err
	}

	if len(ids) == 1 {
		if err := c.PutJSON(fmt.Sprintf("/api/v1/clips/%d/archive", ids[0]), nil, nil); err != nil {
			return err
		}
	} else {
		if err := c.PostJSON("/api/v1/clips/bulk/archive", map[string][]int64{"ids": ids}, nil); err != nil {
			return err
		}
	}

	if jsonOutput {
		printJSON(map[string]int{"archived": len(ids)})
		return nil
	}

	fmt.Printf("Archived %d clip(s)\n", len(ids))
	return nil
}

// --- clip unarchive ---

var clipUnarchiveCmd = &cobra.Command{
	Use:   "unarchive <ids...>",
	Short: "Unarchive one or more clips",
	Long: `Restore archived clips back to the active list.
Use --stdin to read IDs from stdin (one per line).`,
	Example: `  # Unarchive a single clip
  mp clip unarchive 42

  # Unarchive multiple clips
  mp clip unarchive 1 2 3`,
	Args: cobra.ArbitraryArgs,
	RunE: runClipUnarchive,
}

var clipUnarchiveStdin bool

func init() {
	clipUnarchiveCmd.Flags().BoolVar(&clipUnarchiveStdin, "stdin", false, "Read clip IDs from stdin (one per line)")
}

func runClipUnarchive(cmd *cobra.Command, args []string) error {
	ids, err := collectIDs(args, clipUnarchiveStdin)
	if err != nil {
		return err
	}

	c, err := client.New()
	if err != nil {
		return err
	}

	if len(ids) == 1 {
		// Unarchive is DELETE /api/v1/clips/{id}/archive
		if err := c.Delete(fmt.Sprintf("/api/v1/clips/%d/archive", ids[0])); err != nil {
			return err
		}
	} else {
		if err := c.PostJSON("/api/v1/clips/bulk/unarchive", map[string][]int64{"ids": ids}, nil); err != nil {
			return err
		}
	}

	if jsonOutput {
		printJSON(map[string]int{"unarchived": len(ids)})
		return nil
	}

	fmt.Printf("Unarchived %d clip(s)\n", len(ids))
	return nil
}

// --- clip expire ---

var clipExpireCmd = &cobra.Command{
	Use:   "expire <ids...>",
	Short: "Set or cancel clip expiration",
	Long: `Set an expiration timer on clips so they are automatically deleted after a duration.
Use --cancel to remove an existing expiration. Supports bulk operations via --stdin.`,
	Example: `  # Set 30-minute expiration
  mp clip expire 42 --duration 30m

  # Set 7-day expiration on multiple clips
  mp clip expire 1 2 3 --duration 7d

  # Cancel expiration
  mp clip expire 42 --cancel`,
	Args: cobra.ArbitraryArgs,
	RunE: runClipExpire,
}

var (
	clipExpireDuration string
	clipExpireCancel   bool
	clipExpireStdin    bool
)

func init() {
	clipExpireCmd.Flags().StringVar(&clipExpireDuration, "duration", "", "Expiration duration (e.g. 30m, 2h, 7d, 1h30m)")
	clipExpireCmd.Flags().BoolVar(&clipExpireCancel, "cancel", false, "Cancel existing expiration")
	clipExpireCmd.Flags().BoolVar(&clipExpireStdin, "stdin", false, "Read clip IDs from stdin (one per line)")
}

func runClipExpire(cmd *cobra.Command, args []string) error {
	if !clipExpireCancel && clipExpireDuration == "" {
		return fmt.Errorf("either --duration or --cancel is required")
	}

	if clipExpireCancel && clipExpireDuration != "" {
		return fmt.Errorf("--duration and --cancel are mutually exclusive")
	}

	ids, err := collectIDs(args, clipExpireStdin)
	if err != nil {
		return err
	}

	c, err := client.New()
	if err != nil {
		return err
	}

	if clipExpireCancel {
		if len(ids) == 1 {
			if err := c.Delete(fmt.Sprintf("/api/v1/clips/%d/expiration", ids[0])); err != nil {
				return err
			}
		} else {
			if err := c.PostJSON("/api/v1/clips/bulk/cancel-expire", map[string][]int64{"ids": ids}, nil); err != nil {
				return err
			}
		}

		if jsonOutput {
			printJSON(map[string]int{"updated": len(ids)})
			return nil
		}
		fmt.Printf("Cancelled expiration for %d clip(s)\n", len(ids))
		return nil
	}

	minutes, err := parseDuration(clipExpireDuration)
	if err != nil {
		return fmt.Errorf("invalid duration: %w", err)
	}

	if len(ids) == 1 {
		if err := c.PutJSON(fmt.Sprintf("/api/v1/clips/%d/expiration", ids[0]), map[string]int{"minutes": minutes}, nil); err != nil {
			return err
		}
	} else {
		body := map[string]interface{}{
			"ids":     ids,
			"minutes": minutes,
		}
		if err := c.PostJSON("/api/v1/clips/bulk/expire", body, nil); err != nil {
			return err
		}
	}

	if jsonOutput {
		printJSON(map[string]int{"updated": len(ids)})
		return nil
	}

	fmt.Printf("Set expiration (%s) for %d clip(s)\n", clipExpireDuration, len(ids))
	return nil
}

// --- clip download ---

var clipDownloadCmd = &cobra.Command{
	Use:   "download <ids...>",
	Short: "Download clips to files",
	Long: `Download one or more clips to the local filesystem.
A single clip is saved with its original filename. Multiple clips are downloaded as a ZIP archive.`,
	Example: `  # Download a single clip
  mp clip download 42

  # Download to a specific path
  mp clip download 42 -o ~/Downloads/photo.png

  # Download multiple clips as ZIP
  mp clip download 1 2 3 -o clips.zip`,
	Args: cobra.MinimumNArgs(1),
	RunE: runClipDownload,
}

var clipDownloadOutput string

func init() {
	clipDownloadCmd.Flags().StringVarP(&clipDownloadOutput, "output", "o", "", "Output file path")
}

func runClipDownload(cmd *cobra.Command, args []string) error {
	c, err := client.New()
	if err != nil {
		return err
	}

	ids := make([]int64, 0, len(args))
	for _, arg := range args {
		id, err := strconv.ParseInt(arg, 10, 64)
		if err != nil {
			return fmt.Errorf("invalid clip ID: %s", arg)
		}
		ids = append(ids, id)
	}

	if len(ids) == 1 {
		return downloadSingleClip(c, ids[0], clipDownloadOutput)
	}

	return downloadBulkClips(c, ids, clipDownloadOutput)
}

func downloadSingleClip(c *client.Client, id int64, outputPath string) error {
	// If no output path, get the filename from clip metadata first
	if outputPath == "" {
		var clip struct {
			Filename string `json:"filename"`
		}
		if err := c.GetJSON(fmt.Sprintf("/api/v1/clips/%d", id), &clip); err != nil {
			return err
		}
		outputPath = clip.Filename
		if outputPath == "" {
			outputPath = fmt.Sprintf("clip_%d", id)
		}
	}

	body, _, err := c.GetRaw(fmt.Sprintf("/api/v1/clips/%d/data", id))
	if err != nil {
		return err
	}
	defer body.Close()

	f, err := os.Create(outputPath)
	if err != nil {
		return fmt.Errorf("create output file: %w", err)
	}
	defer f.Close()

	n, err := io.Copy(f, body)
	if err != nil {
		return fmt.Errorf("write output file: %w", err)
	}

	if jsonOutput {
		printJSON(map[string]interface{}{
			"id":   id,
			"path": outputPath,
			"size": n,
		})
		return nil
	}

	fmt.Printf("Downloaded clip %d to %s (%s)\n", id, outputPath, formatSize(n))
	return nil
}

func downloadBulkClips(c *client.Client, ids []int64, outputPath string) error {
	if outputPath == "" {
		outputPath = "clips.zip"
	}

	// Bulk download returns a ZIP file. We need to POST JSON and get raw bytes.
	// Build the request using the client's exported fields.
	var buf strings.Builder
	if err := json.NewEncoder(&buf).Encode(map[string][]int64{"ids": ids}); err != nil {
		return fmt.Errorf("encode body: %w", err)
	}

	body, err := postRaw(c, "/api/v1/clips/bulk/download", strings.NewReader(buf.String()))
	if err != nil {
		return err
	}
	defer body.Close()

	f, err := os.Create(outputPath)
	if err != nil {
		return fmt.Errorf("create output file: %w", err)
	}
	defer f.Close()

	n, err := io.Copy(f, body)
	if err != nil {
		return fmt.Errorf("write output file: %w", err)
	}

	if jsonOutput {
		printJSON(map[string]interface{}{
			"path":  outputPath,
			"size":  n,
			"count": len(ids),
		})
		return nil
	}

	fmt.Printf("Downloaded %d clips to %s (%s)\n", len(ids), outputPath, formatSize(n))
	return nil
}

// postRaw performs a POST with a JSON body and returns the raw response body.
// This is needed for endpoints that return binary data (e.g. ZIP downloads).
func postRaw(c *client.Client, path string, body io.Reader) (io.ReadCloser, error) {
	req, err := http.NewRequest("POST", c.BaseURL+path, body)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+c.APIKey)
	req.Header.Set("User-Agent", "mp-cli/1.0")

	resp, err := c.HTTPClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("request failed: %w", err)
	}

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		resp.Body.Close()
		return nil, fmt.Errorf("API error: %s", resp.Status)
	}

	return resp.Body, nil
}

// --- clip open ---

var clipOpenCmd = &cobra.Command{
	Use:   "open <id>",
	Short: "Open a clip (desktop app only)",
	Long:  `Open a clip in the desktop app. This command requires the mahpastes desktop application.`,
	Example: `  mp clip open 42`,
	Args:    cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		return fmt.Errorf("open requires the mahpastes desktop app")
	},
}

// --- clip metadata ---

var clipMetadataCmd = &cobra.Command{
	Use:   "metadata",
	Short: "Manage clip metadata key-value pairs",
	Long:  `View, set, and delete metadata key-value pairs attached to clips.`,
}

var clipMetadataListCmd = &cobra.Command{
	Use:   "list <id>",
	Short: "List all metadata for a clip",
	Long:  `Display all metadata key-value pairs for a clip.`,
	Example: `  mp clip metadata list 42`,
	Args:    cobra.ExactArgs(1),
	RunE:    runClipMetadataList,
}

func runClipMetadataList(cmd *cobra.Command, args []string) error {
	c, err := client.New()
	if err != nil {
		return err
	}

	id, err := strconv.ParseInt(args[0], 10, 64)
	if err != nil {
		return fmt.Errorf("invalid clip ID: %s", args[0])
	}

	var metadata map[string]string
	if err := c.GetJSON(fmt.Sprintf("/api/v1/clips/%d/metadata", id), &metadata); err != nil {
		return err
	}

	if jsonOutput {
		printJSON(metadata)
		return nil
	}

	if len(metadata) == 0 {
		fmt.Println("No metadata.")
		return nil
	}

	pairs := make([][2]string, 0, len(metadata))
	for k, v := range metadata {
		pairs = append(pairs, [2]string{k, v})
	}

	printKeyValue(pairs)
	return nil
}

var clipMetadataGetCmd = &cobra.Command{
	Use:   "get <id> <key>",
	Short: "Get a metadata value",
	Long:  `Get the value of a specific metadata key for a clip.`,
	Example: `  mp clip metadata get 42 author`,
	Args:    cobra.ExactArgs(2),
	RunE:    runClipMetadataGet,
}

func runClipMetadataGet(cmd *cobra.Command, args []string) error {
	c, err := client.New()
	if err != nil {
		return err
	}

	id, err := strconv.ParseInt(args[0], 10, 64)
	if err != nil {
		return fmt.Errorf("invalid clip ID: %s", args[0])
	}

	key := args[1]

	var metadata map[string]string
	if err := c.GetJSON(fmt.Sprintf("/api/v1/clips/%d/metadata", id), &metadata); err != nil {
		return err
	}

	value, ok := metadata[key]
	if !ok {
		return fmt.Errorf("metadata key not found: %s", key)
	}

	if jsonOutput {
		printJSON(map[string]string{key: value})
		return nil
	}

	fmt.Println(value)
	return nil
}

var clipMetadataSetCmd = &cobra.Command{
	Use:   "set <id> <key> <value>",
	Short: "Set a metadata value",
	Long:  `Set a metadata key-value pair on a clip.`,
	Example: `  mp clip metadata set 42 author "John Doe"
  mp clip metadata set 42 source https://example.com`,
	Args: cobra.ExactArgs(3),
	RunE: runClipMetadataSet,
}

func runClipMetadataSet(cmd *cobra.Command, args []string) error {
	c, err := client.New()
	if err != nil {
		return err
	}

	id, err := strconv.ParseInt(args[0], 10, 64)
	if err != nil {
		return fmt.Errorf("invalid clip ID: %s", args[0])
	}

	key := args[1]
	value := args[2]

	if err := c.PutJSON(fmt.Sprintf("/api/v1/clips/%d/metadata/%s", id, key), map[string]string{"value": value}, nil); err != nil {
		return err
	}

	if jsonOutput {
		printJSON(map[string]string{key: value})
		return nil
	}

	fmt.Printf("Set %s = %s on clip %d\n", key, value, id)
	return nil
}

var clipMetadataDeleteCmd = &cobra.Command{
	Use:   "delete <id> <key>",
	Short: "Delete a metadata key",
	Long:  `Remove a metadata key-value pair from a clip.`,
	Example: `  mp clip metadata delete 42 author`,
	Args:    cobra.ExactArgs(2),
	RunE:    runClipMetadataDelete,
}

func runClipMetadataDelete(cmd *cobra.Command, args []string) error {
	c, err := client.New()
	if err != nil {
		return err
	}

	id, err := strconv.ParseInt(args[0], 10, 64)
	if err != nil {
		return fmt.Errorf("invalid clip ID: %s", args[0])
	}

	key := args[1]

	if err := c.Delete(fmt.Sprintf("/api/v1/clips/%d/metadata/%s", id, key)); err != nil {
		return err
	}

	if jsonOutput {
		printJSON(map[string]string{"deleted": key})
		return nil
	}

	fmt.Printf("Deleted metadata key %q from clip %d\n", key, id)
	return nil
}

// --- registration ---

func init() {
	// Wire subcommands under clipCmd
	clipCmd.AddCommand(clipListCmd)
	clipCmd.AddCommand(clipGetCmd)
	clipCmd.AddCommand(clipDataCmd)
	clipCmd.AddCommand(clipUploadCmd)
	clipCmd.AddCommand(clipDeleteCmd)
	clipCmd.AddCommand(clipRenameCmd)
	clipCmd.AddCommand(clipArchiveCmd)
	clipCmd.AddCommand(clipUnarchiveCmd)
	clipCmd.AddCommand(clipExpireCmd)
	clipCmd.AddCommand(clipDownloadCmd)
	clipCmd.AddCommand(clipOpenCmd)
	clipCmd.AddCommand(clipMetadataCmd)

	// Wire metadata subcommands
	clipMetadataCmd.AddCommand(clipMetadataListCmd)
	clipMetadataCmd.AddCommand(clipMetadataGetCmd)
	clipMetadataCmd.AddCommand(clipMetadataSetCmd)
	clipMetadataCmd.AddCommand(clipMetadataDeleteCmd)
}

// --- helpers ---

// parseDuration parses a human-friendly duration string into minutes.
// Supports: "30m", "2h", "7d", "1h30m", "1d12h".
func parseDuration(s string) (int, error) {
	s = strings.TrimSpace(strings.ToLower(s))
	if s == "" {
		return 0, fmt.Errorf("empty duration")
	}

	totalMinutes := 0

	// Handle 'd' suffix (days) since Go's time.ParseDuration doesn't support it
	if idx := strings.Index(s, "d"); idx >= 0 {
		days, err := strconv.Atoi(s[:idx])
		if err != nil {
			return 0, fmt.Errorf("invalid days value in %q", s)
		}
		totalMinutes += days * 24 * 60
		s = s[idx+1:]
	}

	if s != "" {
		d, err := time.ParseDuration(s)
		if err != nil {
			return 0, fmt.Errorf("invalid duration %q: %w", s, err)
		}
		totalMinutes += int(d.Minutes())
	}

	if totalMinutes <= 0 {
		return 0, fmt.Errorf("duration must be positive")
	}

	return totalMinutes, nil
}

// readIDsFromStdin reads numeric IDs from stdin, one per line.
func readIDsFromStdin() ([]int64, error) {
	scanner := bufio.NewScanner(os.Stdin)
	var ids []int64
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" {
			continue
		}
		id, err := strconv.ParseInt(line, 10, 64)
		if err != nil {
			return nil, fmt.Errorf("invalid ID: %s", line)
		}
		ids = append(ids, id)
	}
	return ids, scanner.Err()
}

// collectIDs gathers IDs from command args and/or stdin.
func collectIDs(args []string, fromStdin bool) ([]int64, error) {
	var ids []int64

	for _, arg := range args {
		id, err := strconv.ParseInt(arg, 10, 64)
		if err != nil {
			return nil, fmt.Errorf("invalid clip ID: %s", arg)
		}
		ids = append(ids, id)
	}

	if fromStdin {
		stdinIDs, err := readIDsFromStdin()
		if err != nil {
			return nil, err
		}
		ids = append(ids, stdinIDs...)
	}

	if len(ids) == 0 {
		return nil, fmt.Errorf("at least one clip ID is required")
	}

	return ids, nil
}

// formatTime formats an ISO timestamp to a shorter display format.
func formatTime(iso string) string {
	t, err := time.Parse(time.RFC3339, iso)
	if err != nil {
		// Try other common formats
		t, err = time.Parse("2006-01-02T15:04:05Z", iso)
		if err != nil {
			return iso
		}
	}
	return t.Format("2006-01-02 15:04")
}
