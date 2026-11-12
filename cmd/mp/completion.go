package main

import (
	"fmt"
	"os"
	"sort"
	"strings"
	"time"

	"github.com/spf13/cobra"

	"go-clipboard/cmd/mp/client"
)

// completionTimeout bounds every request a TAB press can make. When the app is
// not running the connection is refused at once; the timeout only matters for
// a black-holed MP_API_URL.
const completionTimeout = 400 * time.Millisecond

type completer = func(*cobra.Command, []string, string) ([]string, cobra.ShellCompDirective)

var contentTypeSuggestions = []string{
	"text/plain",
	"text/html",
	"text/markdown",
	"application/json",
	"application/pdf",
	"image/png",
	"image/jpeg",
	"image/gif",
	"image/webp",
	"image/svg+xml",
}

var roleSuggestions = []string{
	"viewer\tread-only access",
	"editor\tcreate and change clips",
	"admin\tfull access, including keys",
}

// completeTags suggests tag names from the running app. It fails silently: a
// missing key, a stopped app or any API error yields no suggestions and never
// prints anything, so completion degrades to nothing instead of an error.
func completeTags(cmd *cobra.Command, args []string, toComplete string) ([]string, cobra.ShellCompDirective) {
	return tagCompletions(toComplete), cobra.ShellCompDirectiveNoFileComp
}

// completeTagArg completes the given positional arguments as tag names.
func completeTagArg(positions ...int) completer {
	return func(cmd *cobra.Command, args []string, toComplete string) ([]string, cobra.ShellCompDirective) {
		for _, p := range positions {
			if len(args) == p {
				return tagCompletions(toComplete), cobra.ShellCompDirectiveNoFileComp
			}
		}
		return nil, cobra.ShellCompDirectiveNoFileComp
	}
}

func staticCompleter(values []string) completer {
	return func(*cobra.Command, []string, string) ([]string, cobra.ShellCompDirective) {
		return values, cobra.ShellCompDirectiveNoFileComp
	}
}

func tagCompletions(toComplete string) []string {
	c, err := client.New()
	if err != nil {
		return nil
	}
	return fetchTagNames(c.WithTimeout(completionTimeout), toComplete)
}

// fetchTagNames lists the tag names with the given prefix (case-insensitive),
// or nil on any error.
func fetchTagNames(c *client.Client, prefix string) []string {
	var tags []struct {
		Name string `json:"name"`
	}
	if err := c.GetJSON("/api/v1/tags", &tags); err != nil {
		return nil
	}
	prefix = strings.ToLower(prefix)
	var names []string
	for _, t := range tags {
		if t.Name != "" && strings.HasPrefix(strings.ToLower(t.Name), prefix) {
			names = append(names, t.Name)
		}
	}
	sort.Strings(names)
	return names
}

// registerCompletions wires dynamic and static completion onto the command
// tree. It runs from main so every command's flags already exist.
func registerCompletions() {
	for _, p := range []struct {
		cmd  *cobra.Command
		flag string
		fn   completer
	}{
		{clipListCmd, "tag", completeTags},
		{clipDataCmd, "tag", completeTags},
		{clipUploadCmd, "tag", completeTags},
		{watchAddCmd, "auto-tag", completeTags},
		{watchUpdateCmd, "auto-tag", completeTags},
		{tagListCmd, "children-of", completeTags},
		{tagAssignCmd, "tag", completeTags},
		{tagRemoveCmd, "tag", completeTags},
		{clipListCmd, "content-type", staticCompleter(contentTypeSuggestions)},
		{clipUploadCmd, "content-type", staticCompleter(contentTypeSuggestions)},
		{apiKeyCreateCmd, "role", staticCompleter(roleSuggestions)},
	} {
		if err := p.cmd.RegisterFlagCompletionFunc(p.flag, p.fn); err != nil {
			fmt.Fprintf(os.Stderr, "mp: completion for --%s: %v\n", p.flag, err)
		}
	}
	tagUpdateCmd.ValidArgsFunction = completeTagArg(0)
	tagDeleteCmd.ValidArgsFunction = completeTagArg(0)
	tagClipsCmd.ValidArgsFunction = completeTagArg(0)
	tagMergeCmd.ValidArgsFunction = completeTagArg(0, 1)
}
