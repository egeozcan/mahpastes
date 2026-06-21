package main

import (
	"fmt"
	"os"
	"strconv"

	"github.com/spf13/cobra"

	"go-clipboard/cmd/mp/client"
)

// ShareLinkInfo mirrors the server's share-link DTO.
type ShareLinkInfo struct {
	ID            int64   `json:"id"`
	ClipID        int64   `json:"clip_id"`
	TokenPrefix   string  `json:"token_prefix"`
	Name          string  `json:"name"`
	IsRevoked     bool    `json:"is_revoked"`
	ExpiresAt     *string `json:"expires_at"`
	MaxDownloads  *int64  `json:"max_downloads"`
	DownloadCount int64   `json:"download_count"`
	CreatedAt     string  `json:"created_at"`
	LastUsedAt    *string `json:"last_used_at"`
}

// ShareLinkCreateResult mirrors the server's mint response (token shown once).
type ShareLinkCreateResult struct {
	Token string        `json:"token"`
	Path  string        `json:"path"`
	Info  ShareLinkInfo `json:"info"`
}

// ── link (parent) ────────────────────────────────────────────────────────

var linkCmd = &cobra.Command{
	Use:   "link",
	Short: "Manage revocable public share links",
	Long: `Create, list, and revoke secure public share links for individual clips.

Each link is an unguessable token served at /s/<token>. The clip is downloaded
as an attachment by anyone holding the link, no API key required. Links can be
revoked at any time (taking effect immediately) and can optionally expire or cap
the number of downloads.`,
}

// ── link create ───────────────────────────────────────────────────────────

var (
	linkCreateName         string
	linkCreateExpiresIn    int64
	linkCreateMaxDownloads int64
)

var linkCreateCmd = &cobra.Command{
	Use:   "create <clip-id>",
	Short: "Create a public share link for a clip",
	Example: `  # A link that never expires
  mp link create 42

  # A named link that expires in one hour
  mp link create 42 --name "design draft" --expires-in 3600

  # A one-time download link
  mp link create 42 --max-downloads 1`,
	Args: cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		clipID, err := strconv.ParseInt(args[0], 10, 64)
		if err != nil {
			return fmt.Errorf("invalid clip id: %s", args[0])
		}
		c, err := client.New()
		if err != nil {
			return err
		}
		var result ShareLinkCreateResult
		if err := c.PostJSON("/api/v1/links", map[string]interface{}{
			"clip_id":            clipID,
			"name":               linkCreateName,
			"expires_in_seconds": linkCreateExpiresIn,
			"max_downloads":      linkCreateMaxDownloads,
		}, &result); err != nil {
			return err
		}
		if jsonOutput {
			printJSON(result)
			return nil
		}
		printKeyValue([][2]string{
			{"id", strconv.FormatInt(result.Info.ID, 10)},
			{"clip", strconv.FormatInt(result.Info.ClipID, 10)},
			{"name", result.Info.Name},
			{"url", shareURL(c, result.Path)},
		})
		fmt.Fprintln(os.Stderr, "(save this link - the token cannot be retrieved again)")
		return nil
	},
}

// ── link list ─────────────────────────────────────────────────────────────

var linkListCmd = &cobra.Command{
	Use:     "list",
	Short:   "List share links",
	Example: `  mp link list`,
	RunE: func(cmd *cobra.Command, args []string) error {
		c, err := client.New()
		if err != nil {
			return err
		}
		var links []ShareLinkInfo
		if err := c.GetJSON("/api/v1/links", &links); err != nil {
			return err
		}
		if jsonOutput {
			printJSON(links)
			return nil
		}
		rows := make([][]string, 0, len(links))
		for _, l := range links {
			status := "active"
			if l.IsRevoked {
				status = "revoked"
			}
			downloads := strconv.FormatInt(l.DownloadCount, 10)
			if l.MaxDownloads != nil {
				downloads += "/" + strconv.FormatInt(*l.MaxDownloads, 10)
			}
			expires := "never"
			if l.ExpiresAt != nil {
				expires = *l.ExpiresAt
			}
			rows = append(rows, []string{
				strconv.FormatInt(l.ID, 10),
				strconv.FormatInt(l.ClipID, 10),
				l.Name,
				l.TokenPrefix,
				status,
				downloads,
				expires,
			})
		}
		printTable([]string{"ID", "CLIP", "NAME", "PREFIX", "STATUS", "DOWNLOADS", "EXPIRES"}, rows)
		return nil
	},
}

// ── link revoke ─────────────────────────────────────────────────────────────

var linkRevokeCmd = &cobra.Command{
	Use:     "revoke <id>",
	Short:   "Revoke a share link (takes effect immediately)",
	Example: `  mp link revoke 3`,
	Args:    cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		id, err := strconv.ParseInt(args[0], 10, 64)
		if err != nil {
			return fmt.Errorf("invalid link id: %s", args[0])
		}
		c, err := client.New()
		if err != nil {
			return err
		}
		if err := c.Delete(fmt.Sprintf("/api/v1/links/%d", id)); err != nil {
			return err
		}
		if jsonOutput {
			printJSON(map[string]interface{}{"revoked": id})
			return nil
		}
		fmt.Printf("Revoked share link %d\n", id)
		return nil
	},
}

// shareURL turns the server-returned "/s/<token>" path into a full URL using the
// configured API base. The public host may differ (e.g. behind a reverse proxy),
// so this is best-effort convenience — the path alone is authoritative.
func shareURL(c *client.Client, path string) string {
	return c.BaseURL + path
}

func init() {
	linkCreateCmd.Flags().StringVar(&linkCreateName, "name", "", "Optional label for the link")
	linkCreateCmd.Flags().Int64Var(&linkCreateExpiresIn, "expires-in", 0, "Seconds until the link expires (0 = never)")
	linkCreateCmd.Flags().Int64Var(&linkCreateMaxDownloads, "max-downloads", 0, "Maximum number of downloads (0 = unlimited)")

	linkCmd.AddCommand(linkCreateCmd)
	linkCmd.AddCommand(linkListCmd)
	linkCmd.AddCommand(linkRevokeCmd)
}
