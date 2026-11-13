package main

import (
	"bytes"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/spf13/cobra"
	"github.com/spf13/cobra/doc"
	"github.com/spf13/pflag"
)

// gendocsCmd emits the man pages and the Markdown CLI reference from the
// command tree, so they cannot drift from the code. It is hidden: it is a
// build-time tool (make mp-docs), not part of the user-facing CLI.
var gendocsCmd = &cobra.Command{
	Use:    "gendocs",
	Short:  "Generate man pages and the Markdown CLI reference",
	Hidden: true,
	Args:   cobra.NoArgs,
	RunE: func(cmd *cobra.Command, args []string) error {
		manDir, _ := cmd.Flags().GetString("man")
		mdFile, _ := cmd.Flags().GetString("markdown")
		if manDir == "" && mdFile == "" {
			return fmt.Errorf("nothing to do: pass --man <dir> and/or --markdown <file>")
		}
		if manDir != "" {
			if err := os.MkdirAll(manDir, 0o755); err != nil {
				return err
			}
			rootCmd.DisableAutoGenTag = true
			// No Title: cobra then titles each page after its command
			// (MP-CLIP-LIST(1)) instead of calling every page MP(1). Source
			// is what scripts/install-mp.sh checks before removing a page.
			header := &doc.GenManHeader{Section: "1", Source: "mahpastes"}
			restore := escapeTreeForMan(rootCmd)
			err := doc.GenManTree(rootCmd, header, manDir)
			restore()
			if err != nil {
				return err
			}
		}
		if mdFile != "" {
			md, err := renderMarkdownReference(rootCmd)
			if err != nil {
				return err
			}
			if err := os.MkdirAll(filepath.Dir(mdFile), 0o755); err != nil {
				return err
			}
			return os.WriteFile(mdFile, md, 0o644)
		}
		return nil
	},
}

func init() {
	gendocsCmd.Flags().String("man", "", "Directory to write man pages (mp.1, mp-clip-list.1, ...) into")
	gendocsCmd.Flags().String("markdown", "", "File to write the single-page Markdown CLI reference to")
	rootCmd.AddCommand(gendocsCmd)
}

// manEscaper backslash-escapes what md2man would otherwise read as markup.
// Cobra renders a page as Markdown first, where <id> is an HTML tag (dropped)
// and the underscores of MP_API_KEY ... MP_API_URL are emphasis.
var manEscaper = strings.NewReplacer(`\`, `\\`, "<", `\<`, ">", `\>`, "_", `\_`, "*", `\*`)

// escapeManProse escapes s line by line, leaving Markdown code blocks alone
// (fenced, or indented four columns after a blank line): md2man copies those
// through verbatim, so an escape there would show up as a backslash.
func escapeManProse(s string) string {
	lines := strings.Split(s, "\n")
	fenced, prevCode, prevBlank := false, false, true
	for i, line := range lines {
		trimmed := strings.TrimSpace(line)
		indented := strings.HasPrefix(line, "    ") || strings.HasPrefix(line, "\t")
		switch {
		case strings.HasPrefix(trimmed, "```"):
			fenced = !fenced
			prevCode, prevBlank = false, false
		case fenced:
		case trimmed == "":
			prevBlank = true
		case indented && (prevBlank || prevCode):
			prevCode, prevBlank = true, false
		default:
			lines[i] = manEscaper.Replace(line)
			prevCode, prevBlank = false, false
		}
	}
	return strings.Join(lines, "\n")
}

// escapeTreeForMan escapes the text cobra feeds to md2man unquoted (Use
// arguments, Short, Long, flag usage) on root and every descendant, and
// returns a function that puts the originals back: --help, completions and
// the Markdown reference read the same strings and must not see the escapes.
func escapeTreeForMan(root *cobra.Command) (restore func()) {
	var undo []func()
	set := func(p *string) {
		orig := *p
		*p = manEscaper.Replace(orig)
		undo = append(undo, func() { *p = orig })
	}
	seenFlags := map[*pflag.Flag]bool{}
	var walk func(c *cobra.Command)
	walk = func(c *cobra.Command) {
		// The first word of Use is the command name, which names the page.
		if name, args, ok := strings.Cut(c.Use, " "); ok {
			orig := c.Use
			c.Use = name + " " + manEscaper.Replace(args)
			undo = append(undo, func() { c.Use = orig })
		}
		set(&c.Short)
		origLong := c.Long
		c.Long = escapeManProse(origLong)
		undo = append(undo, func() { c.Long = origLong })
		for _, fs := range []*pflag.FlagSet{c.Flags(), c.PersistentFlags()} {
			fs.VisitAll(func(f *pflag.Flag) {
				if !seenFlags[f] {
					seenFlags[f] = true
					set(&f.Usage)
				}
			})
		}
		for _, sub := range c.Commands() {
			walk(sub)
		}
	}
	walk(root)
	return func() {
		for _, u := range undo {
			u()
		}
	}
}

// renderMarkdownReference renders every visible command into one Docusaurus
// page, depth first, with links between commands turned into page anchors.
func renderMarkdownReference(root *cobra.Command) ([]byte, error) {
	root.DisableAutoGenTag = true
	// Cobra adds these lazily on Execute; add them now so the output is the
	// same whether or not the command ran through Execute.
	root.InitDefaultCompletionCmd()
	root.InitDefaultHelpCmd()
	var buf bytes.Buffer
	buf.WriteString("---\nsidebar_position: 3\ntitle: CLI Reference\n---\n\n")
	buf.WriteString("# CLI Reference\n\n")
	buf.WriteString("Every `mp` command and flag, generated from the CLI itself (`make mp-docs`; do not edit by hand). For a guided tour see the [CLI Workflow tutorial](../tutorials/cli-workflow.md); the same content is installed as man pages (`man mp`, `man mp-clip-list`).\n\n")

	anchor := func(name string) string {
		return "#" + strings.TrimSuffix(strings.ReplaceAll(name, "_", "-"), ".md")
	}
	var walk func(c *cobra.Command) error
	walk = func(c *cobra.Command) error {
		if !c.IsAvailableCommand() && c != root || c.IsAdditionalHelpTopicCommand() {
			return nil
		}
		var sub bytes.Buffer
		if err := doc.GenMarkdownCustom(c, &sub, anchor); err != nil {
			return err
		}
		buf.WriteString(escapeMDX(sub.String()))
		buf.WriteString("\n---\n\n")
		for _, child := range c.Commands() {
			if err := walk(child); err != nil {
				return err
			}
		}
		return nil
	}
	if err := walk(root); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}

// bareURL matches scheme://... in prose. Docusaurus turns a bare URL into a
// link and its link handling throws on ones that are not real URLs
// (http://host:port), failing the whole build; a code span is never linked.
var bareURL = regexp.MustCompile(`[A-Za-z][A-Za-z0-9+.-]*://[^\s` + "`" + `<>()\[\]]*[^\s` + "`" + `<>()\[\].,;:!?'"]`)

// escapeMDX makes help text safe for Docusaurus' MDX parser: outside code
// fences and inline code, "<" and "{" would otherwise start JSX, and bare
// URLs are put in code spans so they are not auto-linked.
func escapeMDX(s string) string {
	lines := strings.Split(s, "\n")
	inFence := false
	for i, line := range lines {
		if strings.HasPrefix(strings.TrimSpace(line), "```") {
			inFence = !inFence
			continue
		}
		if inFence {
			continue
		}
		parts := strings.Split(line, "`")
		for j := 0; j < len(parts); j += 2 {
			parts[j] = bareURL.ReplaceAllString(parts[j], "`$0`")
			parts[j] = strings.NewReplacer("<", "&lt;", "{", "&#123;").Replace(parts[j])
		}
		lines[i] = strings.Join(parts, "`")
	}
	return strings.Join(lines, "\n")
}
