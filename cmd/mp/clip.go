package main

import (
	"bufio"
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

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

  # Search by filename or text content
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
	clipListSort        string
	clipListDir         string
	clipListQuiet       bool
)

func init() {
	clipListCmd.Flags().StringVar(&clipListTag, "tag", "", "Filter by tag name or ID")
	clipListCmd.Flags().BoolVar(&clipListArchived, "archived", false, "List archived clips only")
	clipListCmd.Flags().IntVar(&clipListLimit, "limit", 50, "Maximum number of results (1-200)")
	clipListCmd.Flags().IntVar(&clipListOffset, "offset", 0, "Pagination offset")
	clipListCmd.Flags().StringVar(&clipListSearch, "search", "", "Filter by filename or text content substring")
	clipListCmd.Flags().StringVar(&clipListContentType, "content-type", "", "Filter by MIME type")
	clipListCmd.Flags().StringVar(&clipListSort, "sort", "", "Sort field: created_at, name, size or type")
	clipListCmd.Flags().StringVar(&clipListDir, "dir", "", "Sort direction: asc or desc")
	clipListCmd.Flags().BoolVarP(&clipListQuiet, "quiet", "q", false, "Print only clip IDs, one per line")
}

// setListOrder validates --sort/--dir and adds them to a listing query. The
// server silently falls back to created_at/desc for values it does not know,
// so a typo would otherwise list in the wrong order with exit 0. An ordered
// listing takes the server's general listing path, where a plain `search`
// only post-filters the first page of 500-byte previews; search_content asks
// for the database search instead (filename, type and text content, paged).
func setListOrder(params url.Values, sort, dir string) error {
	switch sort {
	case "", "created_at", "name", "filename", "size", "type":
	default:
		return fmt.Errorf("invalid --sort %q: use created_at, name, size or type", sort)
	}
	switch dir {
	case "", "asc", "desc":
	default:
		return fmt.Errorf("invalid --dir %q: use asc or desc", dir)
	}
	if sort != "" {
		params.Set("sort", sort)
	}
	if dir != "" {
		params.Set("dir", dir)
	}
	if params.Get("search") != "" {
		params.Set("search_content", "true")
	}
	return nil
}

func runClipList(cmd *cobra.Command, args []string) error {
	if err := setListOrder(url.Values{}, clipListSort, clipListDir); err != nil {
		return err
	}
	c, err := client.New()
	if err != nil {
		return err
	}

	params := url.Values{}
	params.Set("limit", strconv.Itoa(clipListLimit))
	params.Set("offset", strconv.Itoa(clipListOffset))

	if clipListTag != "" {
		tagID, err := c.ResolveTagID(clipListTag)
		if err != nil {
			return err
		}
		params.Set("tag", strconv.FormatInt(tagID, 10))
	}
	if clipListArchived {
		params.Set("archived", "true")
	}
	if clipListSearch != "" {
		params.Set("search", clipListSearch)
	}
	if clipListContentType != "" {
		params.Set("content_type", clipListContentType)
	}
	if clipListSort != "" || clipListDir != "" {
		if err := setListOrder(params, clipListSort, clipListDir); err != nil {
			return err
		}
	}

	path := "/api/v1/clips?" + params.Encode()

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

	if clipListQuiet {
		for _, clip := range result.Clips {
			fmt.Println(clip.ID)
		}
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
	Use:   "data [id]",
	Short: "Output raw clip content to stdout",
	Long: `Download and write the raw binary content of a clip to stdout.
Useful for piping clip data to other commands or saving to a file.

Redirected or piped output is always written byte for byte. On a terminal,
binary data, text that is not valid UTF-8 and text carrying control characters
(escape sequences other than colours, a carriage return without a line feed,
bidirectional overrides) are refused unless --force is given.`,
	Example: `  # Save clip to a file
  mp clip data 42 > image.png

  # Pipe clip content to another command
  mp clip data 42 | wc -c

  # View text clip content
  mp clip data 42

  # Print the newest clip (optionally within a tag), like pbpaste
  mp clip data --latest
  mp clip data --latest --tag notes`,
	Args: cobra.MaximumNArgs(1),
	RunE: runClipData,
}

var (
	clipDataLatest bool
	clipDataTag    string
	clipDataForce  bool
)

func init() {
	clipDataCmd.Flags().BoolVar(&clipDataLatest, "latest", false, "Use the newest non-archived clip instead of an ID")
	clipDataCmd.Flags().StringVar(&clipDataTag, "tag", "", "With --latest, restrict to a tag (name or ID)")
	clipDataCmd.Flags().BoolVar(&clipDataForce, "force", false, "Write binary data and control characters even when stdout is a terminal")
}

// latestClipID returns the ID of the newest non-archived clip, optionally
// within a tag: what the gallery shows first, so archiving a clip takes it out
// of `mp clip data --latest` as it does out of the main view.
func latestClipID(c *client.Client, tag string) (int64, error) {
	params := url.Values{}
	params.Set("limit", "1")
	params.Set("archived", "false")
	if tag != "" {
		tagID, err := c.ResolveTagID(tag)
		if err != nil {
			return 0, err
		}
		params.Set("tag", strconv.FormatInt(tagID, 10))
	}
	var result struct {
		Clips []struct {
			ID int64 `json:"id"`
		} `json:"clips"`
	}
	if err := c.GetJSON("/api/v1/clips?"+params.Encode(), &result); err != nil {
		return 0, err
	}
	if len(result.Clips) == 0 {
		return 0, &client.APIError{StatusCode: http.StatusNotFound, Message: "no clips found"}
	}
	return result.Clips[0].ID, nil
}

func runClipData(cmd *cobra.Command, args []string) error {
	var id int64
	switch {
	case clipDataLatest && len(args) > 0:
		return fmt.Errorf("--latest cannot be combined with a clip ID")
	case clipDataLatest:
	case len(args) == 0:
		return fmt.Errorf("a clip ID or --latest is required")
	case clipDataTag != "":
		return fmt.Errorf("--tag only applies with --latest")
	default:
		var err error
		id, err = strconv.ParseInt(args[0], 10, 64)
		if err != nil {
			return fmt.Errorf("invalid clip ID: %s", args[0])
		}
	}

	c, err := client.New()
	if err != nil {
		return err
	}
	if clipDataLatest {
		id, err = latestClipID(c, clipDataTag)
		if err != nil {
			return err
		}
	}

	body, _, err := c.GetRaw(fmt.Sprintf("/api/v1/clips/%d/data", id))
	if err != nil {
		return err
	}
	defer body.Close()

	r, err := guardTerminal(body, stdoutIsTerminal() && !clipDataForce)
	if err != nil {
		return err
	}
	return copyToStdout(r)
}

// errTerminalControl is the refusal for text that carries terminal control
// characters or is not UTF-8.
var errTerminalControl = errors.New("refusing to write control characters or text that is not UTF-8 to a terminal (redirect the output or use --force)")

// guardTerminal refuses content that should not land on a terminal (check is
// true): binary data, judged from the first 512 bytes before anything is
// written, and anything terminalSafeReader refuses anywhere in the stream.
// Clips also come from other people (followed shares, other API keys,
// plugins, watch folders), and an escape sequence in one can retitle the
// window, rewrite what is on screen or, in terminals that honour OSC 52, set
// the clipboard. Colour (SGR) sequences are let through, so saved terminal
// output still prints. The returned reader replays the bytes peeked at; it
// fails with errTerminalControl at the first refused byte, after the text
// before it.
func guardTerminal(body io.Reader, check bool) (io.Reader, error) {
	if !check {
		return body, nil
	}
	br := bufio.NewReaderSize(body, 512)
	head, err := br.Peek(512)
	if err != nil && err != io.EOF && err != bufio.ErrBufferFull {
		return nil, err
	}
	if looksBinary(head) {
		return nil, fmt.Errorf("refusing to write binary data to a terminal (redirect the output or use --force)")
	}
	// Refuse before printing anything when the head already shows it.
	var probe terminalSafeReader
	if !probe.feed(head) {
		return nil, errTerminalControl
	}
	return &terminalSafeReader{src: br}, nil
}

// maxSGRSequence bounds a colour sequence held back while it is checked;
// a 24-bit foreground plus background is under 40 bytes.
const maxSGRSequence = 64

// terminalSafeReader passes UTF-8 text through and stops at the first byte
// that could do more on a terminal than print a character or pick a colour:
//
//   - any C0 control except tab, newline, form feed and a carriage return
//     that is part of CR LF (a lone one lets the text after it overprint the
//     line), and DEL;
//   - a C1 control (U+0080-U+009F), which terminals read as CSI/OSC/DCS
//     introducers;
//   - any byte that is not part of a valid UTF-8 sequence: a terminal in
//     8-bit mode reads a bare 0x80-0x9F as the same C1 controls, and what
//     another terminal makes of malformed input is its own business;
//   - the bidirectional embedding, override and isolate controls
//     (U+202A-U+202E, U+2066-U+2069), which show text in an order other than
//     the one it is stored in;
//   - any escape sequence other than SGR ("ESC [ <digits ; :> m"), and SGR 8
//     (conceal), which prints text nobody can see.
//
// It does not judge what the visible characters say (look-alike letters,
// zero-width characters, colours chosen to match the background). A sequence
// is held back until it is known to be allowed, so no part of a refused one
// is ever returned.
type terminalSafeReader struct {
	src   io.Reader
	buf   []byte
	out   []byte // checked bytes not yet returned
	pend  []byte // a sequence still undecided: escape, UTF-8 or CR
	state int
	err   error
}

const (
	termText = iota
	termEsc  // after ESC
	termCSI  // after ESC [
	termUTF8 // inside a multi-byte UTF-8 sequence
	termCR   // after a carriage return
)

func (t *terminalSafeReader) Read(p []byte) (int, error) {
	for len(t.out) == 0 && t.err == nil {
		if t.buf == nil {
			t.buf = make([]byte, 32*1024)
		}
		n, err := t.src.Read(t.buf)
		t.out = t.out[:0]
		if !t.feed(t.buf[:n]) {
			t.err = errTerminalControl
			break
		}
		if err == io.EOF && t.state != termText {
			// An unfinished escape sequence would swallow whatever the
			// shell prints next; a cut-off UTF-8 sequence is not UTF-8;
			// a final CR leaves the prompt on top of the last line.
			t.err = errTerminalControl
			break
		}
		if err != nil {
			t.err = err
		}
	}
	if len(t.out) > 0 {
		n := copy(p, t.out)
		t.out = t.out[n:]
		return n, nil
	}
	return 0, t.err
}

// feed appends the terminal-safe bytes of data to t.out and reports false
// at the first byte that must not reach a terminal. Sequences may straddle
// calls.
func (t *terminalSafeReader) feed(data []byte) bool {
	for _, b := range data {
		switch t.state {
		case termEsc:
			if b != '[' {
				return false
			}
			t.pend = append(t.pend, b)
			t.state = termCSI
		case termCSI:
			switch {
			case b == 'm':
				if sgrConceals(t.pend[2:]) {
					return false
				}
				t.out = append(append(t.out, t.pend...), b)
				t.pend, t.state = t.pend[:0], termText
			case (b >= '0' && b <= '9' || b == ';' || b == ':') && len(t.pend) < maxSGRSequence:
				t.pend = append(t.pend, b)
			default:
				return false
			}
		case termUTF8:
			t.pend = append(t.pend, b)
			// FullRune is also true for a sequence that can no longer
			// become valid, which then decodes as a one-byte error.
			if !utf8.FullRune(t.pend) {
				continue
			}
			r, size := utf8.DecodeRune(t.pend)
			if r == utf8.RuneError && size <= 1 || size != len(t.pend) || !terminalSafeRune(r) {
				return false
			}
			t.out = append(t.out, t.pend...)
			t.pend, t.state = t.pend[:0], termText
		case termCR:
			if b != '\n' {
				return false
			}
			t.out = append(t.out, '\r', '\n')
			t.pend, t.state = t.pend[:0], termText
		default:
			switch {
			case b == 0x1b:
				t.pend, t.state = append(t.pend[:0], b), termEsc
			case b == '\r':
				t.pend, t.state = append(t.pend[:0], b), termCR
			case b >= utf8.RuneSelf:
				// A lone byte here is never valid: continuation bytes
				// and 0xF8-0xFF are refused when the next byte arrives,
				// or at the end of the stream.
				if !utf8.RuneStart(b) {
					return false
				}
				t.pend, t.state = append(t.pend[:0], b), termUTF8
			case b == '\t' || b == '\n' || b == '\f':
				t.out = append(t.out, b)
			case b < 0x20 || b == 0x7f:
				return false
			default:
				t.out = append(t.out, b)
			}
		}
	}
	return true
}

// terminalSafeRune reports whether a decoded non-ASCII character may be
// printed: not a C1 control and not a bidirectional embedding, override or
// isolate control.
func terminalSafeRune(r rune) bool {
	switch {
	case r >= 0x80 && r <= 0x9f:
		return false
	case r >= 0x202a && r <= 0x202e, r >= 0x2066 && r <= 0x2069:
		return false
	}
	return true
}

// sgrConceals reports whether the parameters of an SGR sequence (the bytes
// between "ESC [" and "m") switch on conceal, parameter 8. An 8 that is an
// argument of an extended colour ("38;5;8", "48;2;8;8;8") is a colour.
func sgrConceals(params []byte) bool {
	fields := bytes.Split(params, []byte{';'})
	for i := 0; i < len(fields); i++ {
		code, _, hasSub := bytes.Cut(fields[i], []byte{':'})
		n := sgrNumber(code)
		if n == 8 {
			return true
		}
		if (n == 38 || n == 48 || n == 58) && !hasSub && i+1 < len(fields) {
			// The colon form carries its arguments in sub; this one
			// takes them from the fields that follow.
			switch sgrNumber(fields[i+1]) {
			case 5:
				i += 2
			case 2:
				i += 4
			}
		}
	}
	return false
}

// sgrNumber reads an SGR parameter: digits, where empty means 0. Anything
// longer than a real parameter is -1.
func sgrNumber(digits []byte) int {
	digits = bytes.TrimLeft(digits, "0")
	if len(digits) > 4 {
		return -1
	}
	n := 0
	for _, d := range digits {
		if d < '0' || d > '9' {
			return -1
		}
		n = n*10 + int(d-'0')
	}
	return n
}

// --- clip upload ---

var clipUploadCmd = &cobra.Command{
	Use:   "upload [files...]",
	Short: "Upload files as clips",
	Long: `Upload one or more files to mahpastes as new clips.
If no file arguments are given (or the argument is "-"), reads from stdin. The
clip is named paste-<timestamp>.<ext> with the extension sniffed from the
content unless --filename is given. With --quiet, only the clip IDs are
printed, one per line.
Optionally tag the uploaded clips and set an expiration duration.

Uploads are deduplicated by content. When identical content is already in the
library nothing is created: the existing clip is reported as "Already exists"
(its ID is still printed with --quiet, and --json marks it "duplicate": true),
it keeps its own filename, and --tag and --expire are not applied to it, so an
upload never retags or puts an expiry on a clip it did not create.`,
	Example: `  # Upload a single file
  mp clip upload photo.png

  # Upload multiple files
  mp clip upload *.png

  # Upload with a tag
  mp clip upload document.pdf --tag research

  # Upload from stdin
  echo "hello" | mp clip upload --filename hello.txt

  # Pipe filter: capture the clip's ID
  echo "hello" | mp clip upload -q
  some-command | mp clip upload - --tag logs -q

  # Upload with expiration
  mp clip upload temp.txt --expire 30m`,
	RunE: runClipUpload,
}

var (
	clipUploadTag         string
	clipUploadExpire      string
	clipUploadFilename    string
	clipUploadContentType string
	clipUploadQuiet       bool
)

func init() {
	clipUploadCmd.Flags().StringVar(&clipUploadTag, "tag", "", "Tag to apply to newly created clips (name or ID)")
	clipUploadCmd.Flags().StringVar(&clipUploadExpire, "expire", "", "Expiration duration for newly created clips (e.g. 30m, 2h, 7d)")
	clipUploadCmd.Flags().StringVar(&clipUploadFilename, "filename", "", "Filename for stdin input")
	clipUploadCmd.Flags().StringVar(&clipUploadContentType, "content-type", "", "Content type for stdin input")
	clipUploadCmd.Flags().BoolVarP(&clipUploadQuiet, "quiet", "q", false, "Print only the clip IDs, one per line")
}

func runClipUpload(cmd *cobra.Command, args []string) error {
	// Check the input before touching the API, so a usage mistake never
	// creates or tags anything. A bare `mp clip upload` on a terminal is a
	// mistake rather than a request to type a clip, so it fails instead of
	// waiting; an explicit "-" reads the terminal until EOF, like `cat -`.
	fromStdin := len(args) == 0 || (len(args) == 1 && args[0] == "-")
	if !fromStdin {
		for _, a := range args {
			if a == "-" {
				return fmt.Errorf(`"-" (stdin) cannot be combined with other files`)
			}
		}
	} else if len(args) == 0 && stdinIsTerminal() {
		return fmt.Errorf("no input: pipe data to stdin, pass file arguments, or use - to type the clip")
	}

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
		// Duplicate: the server answered 200, not 201 — identical content
		// already existed and this is that clip, not a new one.
		Duplicate bool `json:"duplicate"`
	}

	var uploaded []uploadedClip
	failed := 0 // files that failed to upload, tag or expire

	if fromStdin {
		name, in, err := stdinFilename(clipUploadFilename, clipUploadContentType, os.Stdin, time.Now())
		if err != nil {
			return err
		}

		extra := map[string]string{}
		if clipUploadContentType != "" {
			extra["content_type"] = clipUploadContentType
		}

		var clip uploadedClip
		status, err := c.UploadFileStatus("/api/v1/clips", name, in, extra, &clip)
		if err != nil {
			return err
		}
		clip.Duplicate = status == http.StatusOK
		uploaded = append(uploaded, clip)
	} else {
		// Upload each file
		for _, filePath := range args {
			f, err := os.Open(filePath)
			if err != nil {
				errorf("skipping %s: %s", filePath, err)
				failed++
				continue
			}

			filename := filepath.Base(filePath)
			var clip uploadedClip
			status, uploadErr := c.UploadFileStatus("/api/v1/clips", filename, f, nil, &clip)
			f.Close()
			if uploadErr != nil {
				errorf("failed to upload %s: %s", filePath, uploadErr)
				failed++
				continue
			}
			clip.Duplicate = status == http.StatusOK
			uploaded = append(uploaded, clip)
		}
	}

	// Apply tag and expiration to the clips this command created. A duplicate
	// is someone's existing clip (or one created and handled above): tagging
	// it would move it out of its folder in that tree, and an expiry would
	// have the reaper delete a clip that was meant to be kept.
	var skipped []string
	if tagID > 0 {
		skipped = append(skipped, "--tag")
	}
	if expireMinutes > 0 {
		skipped = append(skipped, "--expire")
	}
	created := map[int64]bool{}
	for _, clip := range uploaded {
		if !clip.Duplicate {
			created[clip.ID] = true
		}
	}
	for _, clip := range uploaded {
		if clip.Duplicate {
			// A copy of a file uploaded in this same run is already handled.
			if len(skipped) > 0 && !created[clip.ID] {
				fmt.Fprintf(os.Stderr, "Warning: clip %d already existed with identical content; %s not applied\n", clip.ID, strings.Join(skipped, " and "))
			}
			continue
		}
		ok := true
		if tagID > 0 {
			if err := c.PutJSON(fmt.Sprintf("/api/v1/clips/%d/tags/%d", clip.ID, tagID), nil, nil); err != nil {
				errorf("failed to tag clip %d: %s", clip.ID, err)
				ok = false
			}
		}
		if expireMinutes > 0 {
			if err := c.PutJSON(fmt.Sprintf("/api/v1/clips/%d/expiration", clip.ID), map[string]int{"minutes": expireMinutes}, nil); err != nil {
				errorf("failed to set expiration on clip %d: %s", clip.ID, err)
				ok = false
			}
		}
		if !ok {
			failed++
		}
	}

	// Report what was uploaded in every mode, then fail the command if
	// anything went wrong, so `ids=$(mp clip upload -q a b)` scripts see a
	// non-zero exit for a partial upload.
	switch {
	case jsonOutput:
		printJSON(uploaded)
	case clipUploadQuiet:
		for _, clip := range uploaded {
			fmt.Println(clip.ID)
		}
	default:
		for _, clip := range uploaded {
			if clip.Duplicate {
				fmt.Printf("Already exists: %s (ID: %d)\n", clip.Filename, clip.ID)
				continue
			}
			fmt.Printf("Uploaded: %s (ID: %d)\n", clip.Filename, clip.ID)
		}
	}

	if len(uploaded) == 0 {
		return fmt.Errorf("no files were uploaded")
	}
	if failed > 0 {
		total := len(args)
		if fromStdin {
			total = 1
		}
		return fmt.Errorf("%d of %d uploads had errors", failed, total)
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
// It goes through the client so auth, User-Agent, exit codes and API error
// messages match every other request; the bulk ZIP is assembled while the
// response is written, so it uses the long-running client.
func postRaw(c *client.Client, path string, body io.Reader) (io.ReadCloser, error) {
	req, err := http.NewRequest("POST", c.BaseURL+path, body)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := c.Long().Do(req)
	if err != nil {
		return nil, err
	}
	return resp.Body, nil
}

// --- clip metadata ---

var clipMetadataCmd = &cobra.Command{
	Use:   "metadata",
	Short: "Manage clip metadata key-value pairs",
	Long:  `View, set, and delete metadata key-value pairs attached to clips.`,
}

var clipMetadataListCmd = &cobra.Command{
	Use:     "list <id>",
	Short:   "List all metadata for a clip",
	Long:    `Display all metadata key-value pairs for a clip.`,
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
	Use:     "get <id> <key>",
	Short:   "Get a metadata value",
	Long:    `Get the value of a specific metadata key for a clip.`,
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

	if err := c.PutJSON(fmt.Sprintf("/api/v1/clips/%d/metadata/%s", id, url.PathEscape(key)), map[string]string{"value": value}, nil); err != nil {
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
	Use:     "delete <id> <key>",
	Short:   "Delete a metadata key",
	Long:    `Remove a metadata key-value pair from a clip.`,
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

	if err := c.Delete(fmt.Sprintf("/api/v1/clips/%d/metadata/%s", id, url.PathEscape(key))); err != nil {
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
