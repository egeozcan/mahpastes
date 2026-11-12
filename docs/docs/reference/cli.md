---
sidebar_position: 3
title: CLI Reference
---

# CLI Reference

Every `mp` command and flag, generated from the CLI itself (`make mp-docs`; do not edit by hand). For a guided tour see the [CLI Workflow tutorial](../tutorials/cli-workflow.md); the same content is installed as man pages (`man mp`, `man mp-clip-list`).

## mp

Command-line interface for mahpastes

### Synopsis

mp is a command-line interface for the mahpastes clipboard manager.

It communicates with the mahpastes REST API to manage clips, tags,
and other resources from your terminal.

Getting started:
  1. Open mahpastes and enable the API in Settings > API
     (turn on "Start on launch" to keep it running across restarts)
  2. Create an API key in Settings > API
  3. Export your key:  export MP_API_KEY=&lt;your-key>
     (or keep it in a file:  export MP_API_KEY_FILE=&lt;path>)
  4. (Optional) Set a custom URL:  export MP_API_URL=http://host:port

By default, mp connects to http://localhost:44557.

Environment variables:
  MP_API_KEY       API key (this or MP_API_KEY_FILE is required)
  MP_API_KEY_FILE  File holding just the API key, used when MP_API_KEY
                   is unset
  MP_API_URL       API base URL (default http://localhost:44557)
  MP_TIMEOUT       Wait for the connection and the first response bytes
                   (default 30s; never limits a running transfer)
  MAHPASTES_API_KEY, MAHPASTES_API_URL
                   Fallbacks used when the MP_ variables are unset
                   (the same names the Claude Code plugin configures).

### Options

```
  -h, --help   help for mp
      --json   Output results as JSON
```

### SEE ALSO

* [mp api](#mp-api)	 - API connectivity and key management
* [mp backup](#mp-backup)	 - Backup and restore mahpastes data
* [mp clip](#mp-clip)	 - Manage clips
* [mp clipboard](#mp-clipboard)	 - Copy clip content to the system clipboard
* [mp completion](#mp-completion)	 - Generate the autocompletion script for the specified shell
* [mp dedup](#mp-dedup)	 - Find and merge duplicate clips
* [mp link](#mp-link)	 - Manage revocable public share links
* [mp maintenance](#mp-maintenance)	 - Database maintenance tools
* [mp plugin](#mp-plugin)	 - Manage plugins
* [mp serve](#mp-serve)	 - Manage tag HTTP servers
* [mp tag](#mp-tag)	 - Manage tags
* [mp watch](#mp-watch)	 - Manage watch folders


---

## mp api

API connectivity and key management

### Synopsis

Check API connectivity and manage API keys.

### Options

```
  -h, --help   help for api
```

### Options inherited from parent commands

```
      --json   Output results as JSON
```

### SEE ALSO

* [mp](#mp)	 - Command-line interface for mahpastes
* [mp api key](#mp-api-key)	 - Manage API keys
* [mp api status](#mp-api-status)	 - Check API connectivity


---

## mp api key

Manage API keys

### Synopsis

Create, list, and revoke API keys through the REST API.

### Options

```
  -h, --help   help for key
```

### Options inherited from parent commands

```
      --json   Output results as JSON
```

### SEE ALSO

* [mp api](#mp-api)	 - API connectivity and key management
* [mp api key create](#mp-api-key-create)	 - Create an API key
* [mp api key list](#mp-api-key-list)	 - List API keys
* [mp api key revoke](#mp-api-key-revoke)	 - Revoke an API key


---

## mp api key create

Create an API key

```
mp api key create <name> [flags]
```

### Examples

```
  mp api key create my-key
```

### Options

```
  -h, --help             help for create
      --role string      Role for the new key: viewer, editor, or admin (default "viewer")
      --scoped-tag int   Restrict the key to a tag ID
```

### Options inherited from parent commands

```
      --json   Output results as JSON
```

### SEE ALSO

* [mp api key](#mp-api-key)	 - Manage API keys


---

## mp api key list

List API keys

```
mp api key list [flags]
```

### Examples

```
  mp api key list
```

### Options

```
  -h, --help   help for list
```

### Options inherited from parent commands

```
      --json   Output results as JSON
```

### SEE ALSO

* [mp api key](#mp-api-key)	 - Manage API keys


---

## mp api key revoke

Revoke an API key

```
mp api key revoke <id> [flags]
```

### Examples

```
  mp api key revoke 3
```

### Options

```
  -h, --help   help for revoke
```

### Options inherited from parent commands

```
      --json   Output results as JSON
```

### SEE ALSO

* [mp api key](#mp-api-key)	 - Manage API keys


---

## mp api status

Check API connectivity

### Synopsis

Verify that the CLI can connect to the mahpastes API.

First asks the unauthenticated status endpoint whether the API is up, then
makes an authenticated request to confirm the key is valid. Exits 2 when the
app is not reachable and 3 when the key is missing, invalid or revoked.

Also prints the app's version, and warns when it differs from this mp's.

```
mp api status [flags]
```

### Examples

```
  # Check connection
  mp api status

  # Check connection as JSON
  mp api status --json
```

### Options

```
  -h, --help   help for status
```

### Options inherited from parent commands

```
      --json   Output results as JSON
```

### SEE ALSO

* [mp api](#mp-api)	 - API connectivity and key management


---

## mp backup

Backup and restore mahpastes data

### Synopsis

Create and restore full backups of your mahpastes data.

Backups are ZIP archives containing the database and all clip files.

### Options

```
  -h, --help   help for backup
```

### Options inherited from parent commands

```
      --json   Output results as JSON
```

### SEE ALSO

* [mp](#mp)	 - Command-line interface for mahpastes
* [mp backup create](#mp-backup-create)	 - Download a backup to a file
* [mp backup restore](#mp-backup-restore)	 - Restore from a backup file


---

## mp backup create

Download a backup to a file

### Synopsis

Create a backup of all mahpastes data and save it to the specified path.

The backup is a ZIP archive containing the database and all clip files.
Use "-" as the path to write the backup to stdout (for piping).

```
mp backup create <path> [flags]
```

### Examples

```
  # Save backup to a file
  mp backup create ~/backups/mahpastes.zip

  # Pipe backup to another command
  mp backup create - | aws s3 cp - s3://bucket/backup.zip

  # Save with JSON output showing size
  mp backup create backup.zip --json
```

### Options

```
  -h, --help   help for create
```

### Options inherited from parent commands

```
      --json   Output results as JSON
```

### SEE ALSO

* [mp backup](#mp-backup)	 - Backup and restore mahpastes data


---

## mp backup restore

Restore from a backup file

### Synopsis

Restore mahpastes data from a backup ZIP archive.

This replaces all current data with the contents of the backup.
Use "-" as the path to read the backup from stdin (for piping).

```
mp backup restore <path> [flags]
```

### Examples

```
  # Restore from a file
  mp backup restore ~/backups/mahpastes.zip

  # Restore from stdin
  cat backup.zip | mp backup restore -

  # Restore from S3
  aws s3 cp s3://bucket/backup.zip - | mp backup restore -
```

### Options

```
  -h, --help   help for restore
```

### Options inherited from parent commands

```
      --json   Output results as JSON
```

### SEE ALSO

* [mp backup](#mp-backup)	 - Backup and restore mahpastes data


---

## mp clip

Manage clips

### Synopsis

Create, view, modify, and delete clips stored in mahpastes.

### Options

```
  -h, --help   help for clip
```

### Options inherited from parent commands

```
      --json   Output results as JSON
```

### SEE ALSO

* [mp](#mp)	 - Command-line interface for mahpastes
* [mp clip archive](#mp-clip-archive)	 - Archive one or more clips
* [mp clip data](#mp-clip-data)	 - Output raw clip content to stdout
* [mp clip delete](#mp-clip-delete)	 - Delete one or more clips
* [mp clip download](#mp-clip-download)	 - Download clips to files
* [mp clip expire](#mp-clip-expire)	 - Set or cancel clip expiration
* [mp clip get](#mp-clip-get)	 - Show clip metadata
* [mp clip list](#mp-clip-list)	 - List clips with optional filtering
* [mp clip metadata](#mp-clip-metadata)	 - Manage clip metadata key-value pairs
* [mp clip open](#mp-clip-open)	 - Open a clip in its default application
* [mp clip rename](#mp-clip-rename)	 - Rename a clip
* [mp clip unarchive](#mp-clip-unarchive)	 - Unarchive one or more clips
* [mp clip upload](#mp-clip-upload)	 - Upload files as clips


---

## mp clip archive

Archive one or more clips

### Synopsis

Archive clips by their IDs. Archived clips are hidden from the default list view.
Use --stdin to read IDs from stdin (one per line).

```
mp clip archive <ids...> [flags]
```

### Examples

```
  # Archive a single clip
  mp clip archive 42

  # Archive multiple clips
  mp clip archive 1 2 3

  # Archive from a piped list
  mp clip list --json | jq -r '.clips[].id' | mp clip archive --stdin
```

### Options

```
  -h, --help    help for archive
      --stdin   Read clip IDs from stdin (one per line)
```

### Options inherited from parent commands

```
      --json   Output results as JSON
```

### SEE ALSO

* [mp clip](#mp-clip)	 - Manage clips


---

## mp clip data

Output raw clip content to stdout

### Synopsis

Download and write the raw binary content of a clip to stdout.
Useful for piping clip data to other commands or saving to a file.

```
mp clip data [id] [flags]
```

### Examples

```
  # Save clip to a file
  mp clip data 42 > image.png

  # Pipe clip content to another command
  mp clip data 42 | wc -c

  # View text clip content
  mp clip data 42

  # Print the newest clip (optionally within a tag), like pbpaste
  mp clip data --latest
  mp clip data --latest --tag notes
```

### Options

```
      --force        Write binary data even when stdout is a terminal
  -h, --help         help for data
      --latest       Use the newest non-archived clip instead of an ID
      --tag string   With --latest, restrict to a tag (name or ID)
```

### Options inherited from parent commands

```
      --json   Output results as JSON
```

### SEE ALSO

* [mp clip](#mp-clip)	 - Manage clips


---

## mp clip delete

Delete one or more clips

### Synopsis

Delete clips by their IDs. Supports single and bulk deletion.
Use --stdin to read IDs from stdin (one per line), useful for piping.

```
mp clip delete <ids...> [flags]
```

### Examples

```
  # Delete a single clip
  mp clip delete 42

  # Delete multiple clips
  mp clip delete 1 2 3

  # Delete clips from a piped list
  mp clip list --json | jq -r '.clips[].id' | mp clip delete --stdin
```

### Options

```
  -h, --help    help for delete
      --stdin   Read clip IDs from stdin (one per line)
```

### Options inherited from parent commands

```
      --json   Output results as JSON
```

### SEE ALSO

* [mp clip](#mp-clip)	 - Manage clips


---

## mp clip download

Download clips to files

### Synopsis

Download one or more clips to the local filesystem.
A single clip is saved with its original filename. Multiple clips are downloaded as a ZIP archive.

```
mp clip download <ids...> [flags]
```

### Examples

```
  # Download a single clip
  mp clip download 42

  # Download to a specific path
  mp clip download 42 -o ~/Downloads/photo.png

  # Download multiple clips as ZIP
  mp clip download 1 2 3 -o clips.zip
```

### Options

```
  -h, --help            help for download
  -o, --output string   Output file path
```

### Options inherited from parent commands

```
      --json   Output results as JSON
```

### SEE ALSO

* [mp clip](#mp-clip)	 - Manage clips


---

## mp clip expire

Set or cancel clip expiration

### Synopsis

Set an expiration timer on clips so they are automatically deleted after a duration.
Use --cancel to remove an existing expiration. Supports bulk operations via --stdin.

```
mp clip expire <ids...> [flags]
```

### Examples

```
  # Set 30-minute expiration
  mp clip expire 42 --duration 30m

  # Set 7-day expiration on multiple clips
  mp clip expire 1 2 3 --duration 7d

  # Cancel expiration
  mp clip expire 42 --cancel
```

### Options

```
      --cancel            Cancel existing expiration
      --duration string   Expiration duration (e.g. 30m, 2h, 7d, 1h30m)
  -h, --help              help for expire
      --stdin             Read clip IDs from stdin (one per line)
```

### Options inherited from parent commands

```
      --json   Output results as JSON
```

### SEE ALSO

* [mp clip](#mp-clip)	 - Manage clips


---

## mp clip get

Show clip metadata

### Synopsis

Display detailed metadata for a single clip by its ID.

```
mp clip get <id> [flags]
```

### Examples

```
  # Show clip details
  mp clip get 42

  # Get clip info as JSON
  mp clip get 42 --json
```

### Options

```
  -h, --help   help for get
```

### Options inherited from parent commands

```
      --json   Output results as JSON
```

### SEE ALSO

* [mp clip](#mp-clip)	 - Manage clips


---

## mp clip list

List clips with optional filtering

### Synopsis

List clips stored in mahpastes with optional tag, archive, and content type filters.
Results are paginated. Use --limit and --offset for pagination.

```
mp clip list [flags]
```

### Examples

```
  # List recent clips
  mp clip list

  # List archived clips tagged "screenshots"
  mp clip list --tag screenshots --archived

  # Search by filename or text content
  mp clip list --search "report"

  # Get all clip IDs as JSON for scripting
  mp clip list --json | jq -r '.clips[].id'

  # List with pagination
  mp clip list --limit 10 --offset 20
```

### Options

```
      --archived              List archived clips only
      --content-type string   Filter by MIME type
      --dir string            Sort direction: asc or desc
  -h, --help                  help for list
      --limit int             Maximum number of results (1-200) (default 50)
      --offset int            Pagination offset
  -q, --quiet                 Print only clip IDs, one per line
      --search string         Filter by filename or text content substring
      --sort string           Sort field: created_at, name, size or type
      --tag string            Filter by tag name or ID
```

### Options inherited from parent commands

```
      --json   Output results as JSON
```

### SEE ALSO

* [mp clip](#mp-clip)	 - Manage clips


---

## mp clip metadata

Manage clip metadata key-value pairs

### Synopsis

View, set, and delete metadata key-value pairs attached to clips.

### Options

```
  -h, --help   help for metadata
```

### Options inherited from parent commands

```
      --json   Output results as JSON
```

### SEE ALSO

* [mp clip](#mp-clip)	 - Manage clips
* [mp clip metadata delete](#mp-clip-metadata-delete)	 - Delete a metadata key
* [mp clip metadata get](#mp-clip-metadata-get)	 - Get a metadata value
* [mp clip metadata list](#mp-clip-metadata-list)	 - List all metadata for a clip
* [mp clip metadata set](#mp-clip-metadata-set)	 - Set a metadata value


---

## mp clip metadata delete

Delete a metadata key

### Synopsis

Remove a metadata key-value pair from a clip.

```
mp clip metadata delete <id> <key> [flags]
```

### Examples

```
  mp clip metadata delete 42 author
```

### Options

```
  -h, --help   help for delete
```

### Options inherited from parent commands

```
      --json   Output results as JSON
```

### SEE ALSO

* [mp clip metadata](#mp-clip-metadata)	 - Manage clip metadata key-value pairs


---

## mp clip metadata get

Get a metadata value

### Synopsis

Get the value of a specific metadata key for a clip.

```
mp clip metadata get <id> <key> [flags]
```

### Examples

```
  mp clip metadata get 42 author
```

### Options

```
  -h, --help   help for get
```

### Options inherited from parent commands

```
      --json   Output results as JSON
```

### SEE ALSO

* [mp clip metadata](#mp-clip-metadata)	 - Manage clip metadata key-value pairs


---

## mp clip metadata list

List all metadata for a clip

### Synopsis

Display all metadata key-value pairs for a clip.

```
mp clip metadata list <id> [flags]
```

### Examples

```
  mp clip metadata list 42
```

### Options

```
  -h, --help   help for list
```

### Options inherited from parent commands

```
      --json   Output results as JSON
```

### SEE ALSO

* [mp clip metadata](#mp-clip-metadata)	 - Manage clip metadata key-value pairs


---

## mp clip metadata set

Set a metadata value

### Synopsis

Set a metadata key-value pair on a clip.

```
mp clip metadata set <id> <key> <value> [flags]
```

### Examples

```
  mp clip metadata set 42 author "John Doe"
  mp clip metadata set 42 source https://example.com
```

### Options

```
  -h, --help   help for set
```

### Options inherited from parent commands

```
      --json   Output results as JSON
```

### SEE ALSO

* [mp clip metadata](#mp-clip-metadata)	 - Manage clip metadata key-value pairs


---

## mp clip open

Open a clip in its default application

### Synopsis

Download a clip to a private folder and open it with the application the
operating system uses for that file type, or with --app.

The copy stays in mp's cache folder (mahpastes/mp-open under your user cache
directory) because the application reads it after mp has exited; delete that
folder whenever you like. Editing the copy does not change the clip.

Programs, scripts and installers (.exe, .bat, .py, .sh, .command, .pkg,
.desktop and similar) are not opened by default; use "mp clip download" for
those.

--in-app instead brings the running mahpastes window forward and shows the clip,
through its mahpastes:// link. That needs the desktop app on this computer
with the link handler registered (macOS, and Linux after scripts/linux/install.sh);
it does not use the API.

```
mp clip open <id> [flags]
```

### Examples

```
  # Open in the default application
  mp clip open 42

  # Open with a specific application
  mp clip open 42 --app "Preview"        # macOS: an application name or path
  mp clip open 42 --app gimp             # Linux/Windows: a program to run

  # Show the clip in the mahpastes window
  mp clip open 42 --in-app
```

### Options

```
      --app string   Application to open the clip with
  -h, --help         help for open
      --in-app       Show the clip in the mahpastes desktop app instead
```

### Options inherited from parent commands

```
      --json   Output results as JSON
```

### SEE ALSO

* [mp clip](#mp-clip)	 - Manage clips


---

## mp clip rename

Rename a clip

### Synopsis

Change the filename of a clip.

```
mp clip rename <id> <filename> [flags]
```

### Examples

```
  # Rename a clip
  mp clip rename 42 new-name.png
```

### Options

```
  -h, --help   help for rename
```

### Options inherited from parent commands

```
      --json   Output results as JSON
```

### SEE ALSO

* [mp clip](#mp-clip)	 - Manage clips


---

## mp clip unarchive

Unarchive one or more clips

### Synopsis

Restore archived clips back to the active list.
Use --stdin to read IDs from stdin (one per line).

```
mp clip unarchive <ids...> [flags]
```

### Examples

```
  # Unarchive a single clip
  mp clip unarchive 42

  # Unarchive multiple clips
  mp clip unarchive 1 2 3
```

### Options

```
  -h, --help    help for unarchive
      --stdin   Read clip IDs from stdin (one per line)
```

### Options inherited from parent commands

```
      --json   Output results as JSON
```

### SEE ALSO

* [mp clip](#mp-clip)	 - Manage clips


---

## mp clip upload

Upload files as clips

### Synopsis

Upload one or more files to mahpastes as new clips.
If no file arguments are given (or the argument is "-"), reads from stdin. The
clip is named paste-&lt;timestamp>.&lt;ext> with the extension sniffed from the
content unless --filename is given. With --quiet, only the new clip IDs are
printed, one per line.
Optionally tag the uploaded clips and set an expiration duration.

```
mp clip upload [files...] [flags]
```

### Examples

```
  # Upload a single file
  mp clip upload photo.png

  # Upload multiple files
  mp clip upload *.png

  # Upload with a tag
  mp clip upload document.pdf --tag research

  # Upload from stdin
  echo "hello" | mp clip upload --filename hello.txt

  # Pipe filter: capture the new clip's ID
  echo "hello" | mp clip upload -q
  some-command | mp clip upload - --tag logs -q

  # Upload with expiration
  mp clip upload temp.txt --expire 30m
```

### Options

```
      --content-type string   Content type for stdin input
      --expire string         Expiration duration (e.g. 30m, 2h, 7d)
      --filename string       Filename for stdin input
  -h, --help                  help for upload
  -q, --quiet                 Print only the new clip IDs, one per line
      --tag string            Tag to apply to uploaded clips (name or ID)
```

### Options inherited from parent commands

```
      --json   Output results as JSON
```

### SEE ALSO

* [mp clip](#mp-clip)	 - Manage clips


---

## mp clipboard

Copy clip content to the system clipboard

### Synopsis

Copy clip contents or file references to the system clipboard.

These commands instruct the mahpastes desktop app to place clip data on
the system clipboard. The desktop app must be running.

### Options

```
  -h, --help   help for clipboard
```

### Options inherited from parent commands

```
      --json   Output results as JSON
```

### SEE ALSO

* [mp](#mp)	 - Command-line interface for mahpastes
* [mp clipboard copy](#mp-clipboard-copy)	 - Copy clip contents to clipboard
* [mp clipboard copy-file](#mp-clipboard-copy-file)	 - Copy clip as file to clipboard


---

## mp clipboard copy

Copy clip contents to clipboard

### Synopsis

Copy the raw contents of a clip to the system clipboard.

For text clips, this copies the text. For images, the image data is placed
on the clipboard. The mahpastes desktop app performs the actual clipboard
operation.

```
mp clipboard copy <id> [flags]
```

### Examples

```
  # Copy clip contents to clipboard
  mp clipboard copy 42

  # Copy and confirm with JSON
  mp clipboard copy 42 --json
```

### Options

```
  -h, --help   help for copy
```

### Options inherited from parent commands

```
      --json   Output results as JSON
```

### SEE ALSO

* [mp clipboard](#mp-clipboard)	 - Copy clip content to the system clipboard


---

## mp clipboard copy-file

Copy clip as file to clipboard

### Synopsis

Copy a clip as a file reference to the system clipboard.

This places a file reference on the clipboard so you can paste the clip
as a file into Finder, Explorer, a Linux file manager or other applications. The mahpastes desktop app
performs the actual clipboard operation.

```
mp clipboard copy-file <id> [flags]
```

### Examples

```
  # Copy clip as file reference
  mp clipboard copy-file 42

  # Copy and confirm with JSON
  mp clipboard copy-file 42 --json
```

### Options

```
  -h, --help   help for copy-file
```

### Options inherited from parent commands

```
      --json   Output results as JSON
```

### SEE ALSO

* [mp clipboard](#mp-clipboard)	 - Copy clip content to the system clipboard


---

## mp completion

Generate the autocompletion script for the specified shell

### Synopsis

Generate the autocompletion script for mp for the specified shell.
See each sub-command's help for details on how to use the generated script.


### Options

```
  -h, --help   help for completion
```

### Options inherited from parent commands

```
      --json   Output results as JSON
```

### SEE ALSO

* [mp](#mp)	 - Command-line interface for mahpastes
* [mp completion bash](#mp-completion-bash)	 - Generate the autocompletion script for bash
* [mp completion fish](#mp-completion-fish)	 - Generate the autocompletion script for fish
* [mp completion powershell](#mp-completion-powershell)	 - Generate the autocompletion script for powershell
* [mp completion zsh](#mp-completion-zsh)	 - Generate the autocompletion script for zsh


---

## mp completion bash

Generate the autocompletion script for bash

### Synopsis

Generate the autocompletion script for the bash shell.

This script depends on the 'bash-completion' package.
If it is not installed already, you can install it via your OS's package manager.

To load completions in your current shell session:

	source &lt;(mp completion bash)

To load completions for every new session, execute once:

#### Linux:

	mp completion bash > /etc/bash_completion.d/mp

#### macOS:

	mp completion bash > $(brew --prefix)/etc/bash_completion.d/mp

You will need to start a new shell for this setup to take effect.


```
mp completion bash
```

### Options

```
  -h, --help              help for bash
      --no-descriptions   disable completion descriptions
```

### Options inherited from parent commands

```
      --json   Output results as JSON
```

### SEE ALSO

* [mp completion](#mp-completion)	 - Generate the autocompletion script for the specified shell


---

## mp completion fish

Generate the autocompletion script for fish

### Synopsis

Generate the autocompletion script for the fish shell.

To load completions in your current shell session:

	mp completion fish | source

To load completions for every new session, execute once:

	mp completion fish > ~/.config/fish/completions/mp.fish

You will need to start a new shell for this setup to take effect.


```
mp completion fish [flags]
```

### Options

```
  -h, --help              help for fish
      --no-descriptions   disable completion descriptions
```

### Options inherited from parent commands

```
      --json   Output results as JSON
```

### SEE ALSO

* [mp completion](#mp-completion)	 - Generate the autocompletion script for the specified shell


---

## mp completion powershell

Generate the autocompletion script for powershell

### Synopsis

Generate the autocompletion script for powershell.

To load completions in your current shell session:

	mp completion powershell | Out-String | Invoke-Expression

To load completions for every new session, add the output of the above command
to your powershell profile.


```
mp completion powershell [flags]
```

### Options

```
  -h, --help              help for powershell
      --no-descriptions   disable completion descriptions
```

### Options inherited from parent commands

```
      --json   Output results as JSON
```

### SEE ALSO

* [mp completion](#mp-completion)	 - Generate the autocompletion script for the specified shell


---

## mp completion zsh

Generate the autocompletion script for zsh

### Synopsis

Generate the autocompletion script for the zsh shell.

If shell completion is not already enabled in your environment you will need
to enable it.  You can execute the following once:

	echo "autoload -U compinit; compinit" >> ~/.zshrc

To load completions in your current shell session:

	source &lt;(mp completion zsh)

To load completions for every new session, execute once:

#### Linux:

	mp completion zsh > "$&#123;fpath[1]}/_mp"

#### macOS:

	mp completion zsh > $(brew --prefix)/share/zsh/site-functions/_mp

You will need to start a new shell for this setup to take effect.


```
mp completion zsh [flags]
```

### Options

```
  -h, --help              help for zsh
      --no-descriptions   disable completion descriptions
```

### Options inherited from parent commands

```
      --json   Output results as JSON
```

### SEE ALSO

* [mp completion](#mp-completion)	 - Generate the autocompletion script for the specified shell


---

## mp dedup

Find and merge duplicate clips

### Synopsis

Detect duplicate clips by content hash and merge or remove them.

Duplicates are clips with identical binary content. The dedup commands
let you list groups of duplicates, merge a specific group, or remove
all duplicates at once.

### Options

```
  -h, --help   help for dedup
```

### Options inherited from parent commands

```
      --json   Output results as JSON
```

### SEE ALSO

* [mp](#mp)	 - Command-line interface for mahpastes
* [mp dedup all](#mp-dedup-all)	 - Remove all duplicate clips
* [mp dedup list](#mp-dedup-list)	 - List groups of duplicate clips
* [mp dedup merge](#mp-dedup-merge)	 - Merge duplicates for a specific clip


---

## mp dedup all

Remove all duplicate clips

### Synopsis

Remove all duplicate clips across the entire library.

For each group of duplicates, the oldest clip is kept and the rest are
removed. Tags from removed clips are merged onto the kept clip.

```
mp dedup all [flags]
```

### Examples

```
  # Remove all duplicates
  mp dedup all

  # Remove all duplicates and show count as JSON
  mp dedup all --json
```

### Options

```
  -h, --help   help for all
```

### Options inherited from parent commands

```
      --json   Output results as JSON
```

### SEE ALSO

* [mp dedup](#mp-dedup)	 - Find and merge duplicate clips


---

## mp dedup list

List groups of duplicate clips

### Synopsis

Show all groups of clips that share the same content hash.

Each row represents a group of duplicates with the same content.
The Count column shows how many copies exist.

```
mp dedup list [flags]
```

### Examples

```
  # List all duplicate groups
  mp dedup list

  # List duplicates as JSON for scripting
  mp dedup list --json
```

### Options

```
  -h, --help   help for list
```

### Options inherited from parent commands

```
      --json   Output results as JSON
```

### SEE ALSO

* [mp dedup](#mp-dedup)	 - Find and merge duplicate clips


---

## mp dedup merge

Merge duplicates for a specific clip

### Synopsis

Merge all duplicates of a clip into the specified clip ID.

The given clip is kept and all other clips with the same content hash
are removed. Tags from removed clips are merged onto the kept clip.

```
mp dedup merge <clip-id> [flags]
```

### Examples

```
  # Merge duplicates of clip 42
  mp dedup merge 42

  # Merge and show result as JSON
  mp dedup merge 42 --json
```

### Options

```
  -h, --help   help for merge
```

### Options inherited from parent commands

```
      --json   Output results as JSON
```

### SEE ALSO

* [mp dedup](#mp-dedup)	 - Find and merge duplicate clips


---

## mp link

Manage revocable public share links

### Synopsis

Create, list, and revoke secure public share links for individual clips.

Each link is an unguessable token served at /s/&lt;token>. The clip is downloaded
as an attachment by anyone holding the link, no API key required. Links can be
revoked at any time (taking effect immediately) and can optionally expire or cap
the number of downloads.

### Options

```
  -h, --help   help for link
```

### Options inherited from parent commands

```
      --json   Output results as JSON
```

### SEE ALSO

* [mp](#mp)	 - Command-line interface for mahpastes
* [mp link create](#mp-link-create)	 - Create a public share link for a clip
* [mp link list](#mp-link-list)	 - List share links
* [mp link revoke](#mp-link-revoke)	 - Revoke a share link (takes effect immediately)


---

## mp link create

Create a public share link for a clip

```
mp link create <clip-id> [flags]
```

### Examples

```
  # A link that never expires
  mp link create 42

  # A named link that expires in one hour
  mp link create 42 --name "design draft" --expires-in 3600

  # A one-time download link
  mp link create 42 --max-downloads 1
```

### Options

```
      --expires-in int      Seconds until the link expires (0 = never)
  -h, --help                help for create
      --max-downloads int   Maximum number of downloads (0 = unlimited)
      --name string         Optional label for the link
```

### Options inherited from parent commands

```
      --json   Output results as JSON
```

### SEE ALSO

* [mp link](#mp-link)	 - Manage revocable public share links


---

## mp link list

List share links

```
mp link list [flags]
```

### Examples

```
  mp link list
```

### Options

```
  -h, --help   help for list
```

### Options inherited from parent commands

```
      --json   Output results as JSON
```

### SEE ALSO

* [mp link](#mp-link)	 - Manage revocable public share links


---

## mp link revoke

Revoke a share link (takes effect immediately)

```
mp link revoke <id> [flags]
```

### Examples

```
  mp link revoke 3
```

### Options

```
  -h, --help   help for revoke
```

### Options inherited from parent commands

```
      --json   Output results as JSON
```

### SEE ALSO

* [mp link](#mp-link)	 - Manage revocable public share links


---

## mp maintenance

Database maintenance tools

### Options

```
  -h, --help   help for maintenance
```

### Options inherited from parent commands

```
      --json   Output results as JSON
```

### SEE ALSO

* [mp](#mp)	 - Command-line interface for mahpastes
* [mp maintenance orphan-rows](#mp-maintenance-orphan-rows)	 - List or clean orphan DB rows
* [mp maintenance stale-files](#mp-maintenance-stale-files)	 - List or clean stale temp/share-staging files
* [mp maintenance vacuum](#mp-maintenance-vacuum)	 - Compact the database (VACUUM + ANALYZE)


---

## mp maintenance orphan-rows

List or clean orphan DB rows

### Options

```
  -h, --help   help for orphan-rows
```

### Options inherited from parent commands

```
      --json   Output results as JSON
```

### SEE ALSO

* [mp maintenance](#mp-maintenance)	 - Database maintenance tools
* [mp maintenance orphan-rows clean](#mp-maintenance-orphan-rows-clean)	 - Clean orphan DB rows
* [mp maintenance orphan-rows list](#mp-maintenance-orphan-rows-list)	 - List orphan row counts


---

## mp maintenance orphan-rows clean

Clean orphan DB rows

```
mp maintenance orphan-rows clean [flags]
```

### Options

```
  -h, --help   help for clean
```

### Options inherited from parent commands

```
      --json   Output results as JSON
```

### SEE ALSO

* [mp maintenance orphan-rows](#mp-maintenance-orphan-rows)	 - List or clean orphan DB rows


---

## mp maintenance orphan-rows list

List orphan row counts

```
mp maintenance orphan-rows list [flags]
```

### Options

```
  -h, --help   help for list
```

### Options inherited from parent commands

```
      --json   Output results as JSON
```

### SEE ALSO

* [mp maintenance orphan-rows](#mp-maintenance-orphan-rows)	 - List or clean orphan DB rows


---

## mp maintenance stale-files

List or clean stale temp/share-staging files

### Options

```
  -h, --help   help for stale-files
```

### Options inherited from parent commands

```
      --json   Output results as JSON
```

### SEE ALSO

* [mp maintenance](#mp-maintenance)	 - Database maintenance tools
* [mp maintenance stale-files clean](#mp-maintenance-stale-files-clean)	 - Remove stale files
* [mp maintenance stale-files list](#mp-maintenance-stale-files-list)	 - List stale files


---

## mp maintenance stale-files clean

Remove stale files

```
mp maintenance stale-files clean [flags]
```

### Options

```
  -h, --help   help for clean
```

### Options inherited from parent commands

```
      --json   Output results as JSON
```

### SEE ALSO

* [mp maintenance stale-files](#mp-maintenance-stale-files)	 - List or clean stale temp/share-staging files


---

## mp maintenance stale-files list

List stale files

```
mp maintenance stale-files list [flags]
```

### Options

```
  -h, --help   help for list
```

### Options inherited from parent commands

```
      --json   Output results as JSON
```

### SEE ALSO

* [mp maintenance stale-files](#mp-maintenance-stale-files)	 - List or clean stale temp/share-staging files


---

## mp maintenance vacuum

Compact the database (VACUUM + ANALYZE)

```
mp maintenance vacuum [flags]
```

### Options

```
  -h, --help   help for vacuum
```

### Options inherited from parent commands

```
      --json   Output results as JSON
```

### SEE ALSO

* [mp maintenance](#mp-maintenance)	 - Database maintenance tools


---

## mp plugin

Manage plugins

### Synopsis

Install, enable, disable, and remove Lua plugins.

Plugins extend mahpastes with custom actions, event handlers, and
scheduled tasks. Each plugin runs in a sandboxed Lua environment.

### Options

```
  -h, --help   help for plugin
```

### Options inherited from parent commands

```
      --json   Output results as JSON
```

### SEE ALSO

* [mp](#mp)	 - Command-line interface for mahpastes
* [mp plugin disable](#mp-plugin-disable)	 - Disable an enabled plugin
* [mp plugin enable](#mp-plugin-enable)	 - Enable a disabled plugin
* [mp plugin install](#mp-plugin-install)	 - Install a plugin from a URL or file path
* [mp plugin list](#mp-plugin-list)	 - List installed plugins
* [mp plugin remove](#mp-plugin-remove)	 - Remove an installed plugin
* [mp plugin run](#mp-plugin-run)	 - Execute a plugin action
* [mp plugin storage](#mp-plugin-storage)	 - Manage plugin key-value storage
* [mp plugin update](#mp-plugin-update)	 - Update a plugin or check for updates


---

## mp plugin disable

Disable an enabled plugin

### Synopsis

Disable a plugin. It remains installed but will not handle events or run actions.

```
mp plugin disable <id> [flags]
```

### Examples

```
  mp plugin disable 1
```

### Options

```
  -h, --help   help for disable
```

### Options inherited from parent commands

```
      --json   Output results as JSON
```

### SEE ALSO

* [mp plugin](#mp-plugin)	 - Manage plugins


---

## mp plugin enable

Enable a disabled plugin

### Synopsis

Enable a plugin so it can handle events and run actions.

```
mp plugin enable <id> [flags]
```

### Examples

```
  mp plugin enable 1
```

### Options

```
  -h, --help   help for enable
```

### Options inherited from parent commands

```
      --json   Output results as JSON
```

### SEE ALSO

* [mp plugin](#mp-plugin)	 - Manage plugins


---

## mp plugin install

Install a plugin from a URL or file path

### Synopsis

Install a new plugin from a URL or local file path.

The source can be:
  - A URL to a .lua file or plugin repository
  - A local file path to a .lua plugin file

```
mp plugin install <source> [flags]
```

### Examples

```
  # Install from URL
  mp plugin install https://example.com/my-plugin.lua

  # Install from local file
  mp plugin install ./plugins/my-plugin.lua
```

### Options

```
  -h, --help   help for install
```

### Options inherited from parent commands

```
      --json   Output results as JSON
```

### SEE ALSO

* [mp plugin](#mp-plugin)	 - Manage plugins


---

## mp plugin list

List installed plugins

### Synopsis

Display all installed plugins with their status and version.

```
mp plugin list [flags]
```

### Examples

```
  # List all plugins
  mp plugin list

  # List as JSON
  mp plugin list --json
```

### Options

```
  -h, --help   help for list
```

### Options inherited from parent commands

```
      --json   Output results as JSON
```

### SEE ALSO

* [mp plugin](#mp-plugin)	 - Manage plugins


---

## mp plugin remove

Remove an installed plugin

### Synopsis

Uninstall a plugin and remove its files.

This does not delete any clips or data created by the plugin.

```
mp plugin remove <id> [flags]
```

### Examples

```
  # Remove a plugin by ID
  mp plugin remove 1
```

### Options

```
  -h, --help   help for remove
```

### Options inherited from parent commands

```
      --json   Output results as JSON
```

### SEE ALSO

* [mp plugin](#mp-plugin)	 - Manage plugins


---

## mp plugin run

Execute a plugin action

### Synopsis

Run a specific action defined by a plugin.

Use --clip to pass clip IDs to the action and --option to set
action option values in key=value format.

```
mp plugin run <plugin-id> <action-id> [flags]
```

### Examples

```
  # Run an action
  mp plugin run 1 process-image --clip 42

  # Run with multiple clips and options
  mp plugin run 1 transform --clip 1 --clip 2 --option quality=high --option format=png

  # Run and show result as JSON
  mp plugin run 1 analyze --clip 42 --json
```

### Options

```
      --clip int64Slice      Clip ID(s) to pass to the action (repeatable) (default [])
  -h, --help                 help for run
      --option stringArray   Action option in key=value format (repeatable)
```

### Options inherited from parent commands

```
      --json   Output results as JSON
```

### SEE ALSO

* [mp plugin](#mp-plugin)	 - Manage plugins


---

## mp plugin storage

Manage plugin key-value storage

### Synopsis

View and modify a plugin's sandboxed key-value storage.

Each plugin has isolated storage for configuration and state.

### Options

```
  -h, --help   help for storage
```

### Options inherited from parent commands

```
      --json   Output results as JSON
```

### SEE ALSO

* [mp plugin](#mp-plugin)	 - Manage plugins
* [mp plugin storage get](#mp-plugin-storage-get)	 - Get a storage value
* [mp plugin storage list](#mp-plugin-storage-list)	 - List all storage keys for a plugin
* [mp plugin storage set](#mp-plugin-storage-set)	 - Set a storage value


---

## mp plugin storage get

Get a storage value

### Synopsis

Get the value of a specific key from a plugin's storage.

```
mp plugin storage get <plugin-id> <key> [flags]
```

### Examples

```
  mp plugin storage get 1 api_key
  mp plugin storage get 1 api_key --json
```

### Options

```
  -h, --help   help for get
```

### Options inherited from parent commands

```
      --json   Output results as JSON
```

### SEE ALSO

* [mp plugin storage](#mp-plugin-storage)	 - Manage plugin key-value storage


---

## mp plugin storage list

List all storage keys for a plugin

### Synopsis

Display all key-value pairs stored by a plugin.

```
mp plugin storage list <plugin-id> [flags]
```

### Examples

```
  # List storage for a plugin
  mp plugin storage list 1

  # List as JSON
  mp plugin storage list 1 --json
```

### Options

```
  -h, --help   help for list
```

### Options inherited from parent commands

```
      --json   Output results as JSON
```

### SEE ALSO

* [mp plugin storage](#mp-plugin-storage)	 - Manage plugin key-value storage


---

## mp plugin storage set

Set a storage value

### Synopsis

Set a key-value pair in a plugin's storage.

```
mp plugin storage set <plugin-id> <key> <value> [flags]
```

### Examples

```
  mp plugin storage set 1 api_key sk-12345
  mp plugin storage set 1 threshold "0.8"
```

### Options

```
  -h, --help   help for set
```

### Options inherited from parent commands

```
      --json   Output results as JSON
```

### SEE ALSO

* [mp plugin storage](#mp-plugin-storage)	 - Manage plugin key-value storage


---

## mp plugin update

Update a plugin or check for updates

### Synopsis

Update a specific plugin to its latest version, or check all plugins
for available updates.

Use --check to see which plugins have updates available without
actually updating them.

```
mp plugin update [id] [flags]
```

### Examples

```
  # Check all plugins for updates
  mp plugin update --check

  # Update a specific plugin
  mp plugin update 1
```

### Options

```
      --check   Check for available updates without installing
  -h, --help    help for update
```

### Options inherited from parent commands

```
      --json   Output results as JSON
```

### SEE ALSO

* [mp plugin](#mp-plugin)	 - Manage plugins


---

## mp serve

Manage tag HTTP servers

### Synopsis

Start, stop, and list HTTP servers that serve tag contents as web pages.

Each served tag gets its own HTTP server on a specified port. HTML clips
become web pages, and other clips are served as static assets.

### Options

```
  -h, --help   help for serve
```

### Options inherited from parent commands

```
      --json   Output results as JSON
```

### SEE ALSO

* [mp](#mp)	 - Command-line interface for mahpastes
* [mp serve list](#mp-serve-list)	 - List running tag servers
* [mp serve start](#mp-serve-start)	 - Start serving a tag over HTTP
* [mp serve stop](#mp-serve-stop)	 - Stop serving a tag


---

## mp serve list

List running tag servers

### Synopsis

Display all currently running tag HTTP servers with their port, URL, and API access mode.

```
mp serve list [flags]
```

### Examples

```
  # List all running servers
  mp serve list

  # Get server list as JSON
  mp serve list --json
```

### Options

```
  -h, --help   help for list
```

### Options inherited from parent commands

```
      --json   Output results as JSON
```

### SEE ALSO

* [mp serve](#mp-serve)	 - Manage tag HTTP servers


---

## mp serve start

Start serving a tag over HTTP

### Synopsis

Start an HTTP server for the specified tag, making its clips accessible as web pages.

The tag argument can be a tag name or numeric ID. An available port is
auto-assigned unless --port is specified.

```
mp serve start <tag> [flags]
```

### Examples

```
  # Start serving with auto-assigned port
  mp serve start my-site

  # Start on a specific port
  mp serve start my-site --port 8080

  # Start with read-write JSON API access
  mp serve start my-site --port 8080 --api-access readwrite

  # Bind to all interfaces (not just localhost)
  mp serve start my-site --bind-all
```

### Options

```
      --api-access string   JSON API access level: none, read, or readwrite (default "none")
      --bind-all            Bind to all interfaces (not just localhost)
  -h, --help                help for start
      --port int            Port to serve on (0 for auto-assign)
```

### Options inherited from parent commands

```
      --json   Output results as JSON
```

### SEE ALSO

* [mp serve](#mp-serve)	 - Manage tag HTTP servers


---

## mp serve stop

Stop serving a tag

### Synopsis

Stop the HTTP server for the specified tag.

The tag argument can be a tag name or numeric ID.

```
mp serve stop <tag> [flags]
```

### Examples

```
  # Stop serving by tag name
  mp serve stop my-site

  # Stop serving by tag ID
  mp serve stop 5
```

### Options

```
  -h, --help   help for stop
```

### Options inherited from parent commands

```
      --json   Output results as JSON
```

### SEE ALSO

* [mp serve](#mp-serve)	 - Manage tag HTTP servers


---

## mp tag

Manage tags

### Synopsis

Create, list, update, and delete tags.

Tags organize clips into categories and can form hierarchical trees
using "/" as a separator (e.g., "work/client1/projectA").

### Options

```
  -h, --help   help for tag
```

### Options inherited from parent commands

```
      --json   Output results as JSON
```

### SEE ALSO

* [mp](#mp)	 - Command-line interface for mahpastes
* [mp tag assign](#mp-tag-assign)	 - Assign a tag to one or more clips
* [mp tag clips](#mp-tag-clips)	 - List clips with a specific tag
* [mp tag create](#mp-tag-create)	 - Create a new tag
* [mp tag delete](#mp-tag-delete)	 - Delete a tag
* [mp tag hidden](#mp-tag-hidden)	 - View or set hidden tags
* [mp tag list](#mp-tag-list)	 - List all tags
* [mp tag merge](#mp-tag-merge)	 - Merge source tag into destination tag
* [mp tag remove](#mp-tag-remove)	 - Remove a tag from one or more clips
* [mp tag update](#mp-tag-update)	 - Update a tag's name or color


---

## mp tag assign

Assign a tag to one or more clips

### Synopsis

Add a tag to one or more clips.

Clip IDs can be passed as arguments or read from stdin with --stdin.
Tree exclusivity is enforced: adding a tag removes other tags from
the same root tree on each clip.

```
mp tag assign <clip-id> [clip-id...] [flags]
```

### Examples

```
  mp tag assign 1 --tag photos
  mp tag assign 1 2 3 --tag work/client1
  echo "1\n2\n3" | mp tag assign --tag photos --stdin
```

### Options

```
  -h, --help         help for assign
      --stdin        Read clip IDs from stdin (one per line)
      --tag string   Tag to assign (name or ID)
```

### Options inherited from parent commands

```
      --json   Output results as JSON
```

### SEE ALSO

* [mp tag](#mp-tag)	 - Manage tags


---

## mp tag clips

List clips with a specific tag

### Synopsis

List all clips assigned to a tag.

The tag argument can be a tag name or numeric ID.
Results are paginated with --limit and --offset.

```
mp tag clips <tag> [flags]
```

### Examples

```
  mp tag clips photos
  mp tag clips 5 --limit 20
  mp tag clips work/client1 --offset 50 --json
```

### Options

```
  -h, --help         help for clips
      --limit int    Maximum number of clips to return (default 50)
      --offset int   Number of clips to skip
```

### Options inherited from parent commands

```
      --json   Output results as JSON
```

### SEE ALSO

* [mp tag](#mp-tag)	 - Manage tags


---

## mp tag create

Create a new tag

### Synopsis

Create a new tag with the given name.

Hierarchical tags use "/" as a separator (e.g., "work/client1").
Parent tags are created automatically if they don't exist.

```
mp tag create <name> [flags]
```

### Examples

```
  mp tag create photos
  mp tag create work/client1
  mp tag create "design assets"
```

### Options

```
  -h, --help   help for create
```

### Options inherited from parent commands

```
      --json   Output results as JSON
```

### SEE ALSO

* [mp tag](#mp-tag)	 - Manage tags


---

## mp tag delete

Delete a tag

### Synopsis

Delete a tag by name or ID.

This removes the tag from all clips but does not delete the clips themselves.

```
mp tag delete <tag> [flags]
```

### Examples

```
  mp tag delete photos
  mp tag delete 5
```

### Options

```
  -h, --help   help for delete
```

### Options inherited from parent commands

```
      --json   Output results as JSON
```

### SEE ALSO

* [mp tag](#mp-tag)	 - Manage tags


---

## mp tag hidden

View or set hidden tags

### Synopsis

View the current list of hidden tag IDs, or update them with --set.

Hidden tags are not shown in the main tag filter UI.

```
mp tag hidden [flags]
```

### Examples

```
  mp tag hidden
  mp tag hidden --json
  mp tag hidden --set "1,2,3"
  mp tag hidden --set ""
```

### Options

```
  -h, --help         help for hidden
      --set string   Comma-separated tag IDs to set as hidden (empty string clears all)
```

### Options inherited from parent commands

```
      --json   Output results as JSON
```

### SEE ALSO

* [mp tag](#mp-tag)	 - Manage tags


---

## mp tag list

List all tags

### Synopsis

List all tags with their ID, name, color, and clip count.

Use --children-of to list only the direct children of a specific tag.

```
mp tag list [flags]
```

### Examples

```
  mp tag list
  mp tag list --json
  mp tag list --children-of work
  mp tag list --children-of 5
```

### Options

```
      --children-of string   List children of a specific tag (name or ID)
  -h, --help                 help for list
```

### Options inherited from parent commands

```
      --json   Output results as JSON
```

### SEE ALSO

* [mp tag](#mp-tag)	 - Manage tags


---

## mp tag merge

Merge source tag into destination tag

### Synopsis

Merge a source tag into a destination tag.

All clips assigned to the source tag will be assigned to the destination tag instead.
The source tag is then deleted. Tree exclusivity is enforced on each clip.

Both tags can be referenced by name or ID.

```
mp tag merge <source-tag> <dest-tag> [flags]
```

### Examples

```
  mp tag merge photos archive
  mp tag merge 5 10
  mp tag merge work/client1 work/inactive
```

### Options

```
  -h, --help   help for merge
```

### Options inherited from parent commands

```
      --json   Output results as JSON
```

### SEE ALSO

* [mp tag](#mp-tag)	 - Manage tags


---

## mp tag remove

Remove a tag from one or more clips

### Synopsis

Remove a tag from one or more clips.

Clip IDs can be passed as arguments or read from stdin with --stdin.

```
mp tag remove <clip-id> [clip-id...] [flags]
```

### Examples

```
  mp tag remove 1 --tag photos
  mp tag remove 1 2 3 --tag work
  echo "1\n2\n3" | mp tag remove --tag photos --stdin
```

### Options

```
  -h, --help         help for remove
      --stdin        Read clip IDs from stdin (one per line)
      --tag string   Tag to remove (name or ID)
```

### Options inherited from parent commands

```
      --json   Output results as JSON
```

### SEE ALSO

* [mp tag](#mp-tag)	 - Manage tags


---

## mp tag update

Update a tag's name or color

### Synopsis

Update an existing tag's name, color, or both.

The tag argument can be a tag name or numeric ID.
At least one of --name or --color must be provided.

```
mp tag update <tag> [flags]
```

### Examples

```
  mp tag update photos --name "my photos"
  mp tag update 5 --color "#00ff00"
  mp tag update work --name "projects" --color "#0000ff"
```

### Options

```
      --color string   New tag color as hex
  -h, --help           help for update
      --name string    New tag name
```

### Options inherited from parent commands

```
      --json   Output results as JSON
```

### SEE ALSO

* [mp tag](#mp-tag)	 - Manage tags


---

## mp watch

Manage watch folders

### Synopsis

Add, update, pause, and remove watch folders.

Watch folders monitor a directory on disk and automatically import new
files as clips. Filters control which file types are imported.

### Options

```
  -h, --help   help for watch
```

### Options inherited from parent commands

```
      --json   Output results as JSON
```

### SEE ALSO

* [mp](#mp)	 - Command-line interface for mahpastes
* [mp watch add](#mp-watch-add)	 - Add a new watch folder
* [mp watch list](#mp-watch-list)	 - List all watch folders
* [mp watch pause](#mp-watch-pause)	 - Pause a watch folder or all folders
* [mp watch process](#mp-watch-process)	 - Process existing files in a watch folder
* [mp watch remove](#mp-watch-remove)	 - Remove a watch folder
* [mp watch resume](#mp-watch-resume)	 - Resume a watch folder or all folders
* [mp watch status](#mp-watch-status)	 - Show watch folder system status
* [mp watch update](#mp-watch-update)	 - Update a watch folder's settings


---

## mp watch add

Add a new watch folder

### Synopsis

Start watching a directory for new files to import as clips.

Use flags to configure file filters, auto-tagging, and other options.

```
mp watch add <path> [flags]
```

### Examples

```
  # Watch a folder for all files
  mp watch add ~/Screenshots

  # Watch for images only
  mp watch add ~/Photos --filter presets --presets images

  # Watch with auto-tagging
  mp watch add ~/Downloads --auto-tag downloads

  # Watch with a custom regex filter
  mp watch add ~/Exports --filter custom --regex "\\.(png|jpg)$"

  # Watch and process existing files
  mp watch add ~/Documents --process-existing
```

### Options

```
      --auto-archive       Automatically archive imported clips
      --auto-tag string    Tag name or ID to auto-assign to imported clips
      --filter string      File filter mode: all, presets, or custom (default "all")
  -h, --help               help for add
      --presets string     Comma-separated preset names (images,videos,documents)
      --process-existing   Import existing files when adding the folder
      --regex string       Custom regex filter pattern
```

### Options inherited from parent commands

```
      --json   Output results as JSON
```

### SEE ALSO

* [mp watch](#mp-watch)	 - Manage watch folders


---

## mp watch list

List all watch folders

### Synopsis

Display all configured watch folders with their settings.

```
mp watch list [flags]
```

### Examples

```
  # List watch folders
  mp watch list

  # List as JSON
  mp watch list --json
```

### Options

```
  -h, --help   help for list
```

### Options inherited from parent commands

```
      --json   Output results as JSON
```

### SEE ALSO

* [mp watch](#mp-watch)	 - Manage watch folders


---

## mp watch pause

Pause a watch folder or all folders

### Synopsis

Pause file watching for a specific folder or globally.

When paused, new files in the folder are not imported. Use --global to
pause all watch folders at once.

```
mp watch pause [id] [flags]
```

### Examples

```
  # Pause a specific folder
  mp watch pause 1

  # Pause all watch folders
  mp watch pause --global
```

### Options

```
      --global   Pause all watch folders
  -h, --help     help for pause
```

### Options inherited from parent commands

```
      --json   Output results as JSON
```

### SEE ALSO

* [mp watch](#mp-watch)	 - Manage watch folders


---

## mp watch process

Process existing files in a watch folder

### Synopsis

Trigger a scan of existing files in a watch folder.

Normally, watch folders only detect new files. This command imports any
existing files that haven't been imported yet.

```
mp watch process <id> [flags]
```

### Examples

```
  # Process existing files in folder 1
  mp watch process 1
```

### Options

```
  -h, --help   help for process
```

### Options inherited from parent commands

```
      --json   Output results as JSON
```

### SEE ALSO

* [mp watch](#mp-watch)	 - Manage watch folders


---

## mp watch remove

Remove a watch folder

### Synopsis

Stop watching a folder and remove its configuration.

This does not delete any clips that were previously imported from the folder.

```
mp watch remove <id> [flags]
```

### Examples

```
  # Remove watch folder 1
  mp watch remove 1
```

### Options

```
  -h, --help   help for remove
```

### Options inherited from parent commands

```
      --json   Output results as JSON
```

### SEE ALSO

* [mp watch](#mp-watch)	 - Manage watch folders


---

## mp watch resume

Resume a watch folder or all folders

### Synopsis

Resume file watching for a paused folder or globally.

Use --global to resume all watch folders at once.

```
mp watch resume [id] [flags]
```

### Examples

```
  # Resume a specific folder
  mp watch resume 1

  # Resume all watch folders
  mp watch resume --global
```

### Options

```
      --global   Resume all watch folders
  -h, --help     help for resume
```

### Options inherited from parent commands

```
      --json   Output results as JSON
```

### SEE ALSO

* [mp watch](#mp-watch)	 - Manage watch folders


---

## mp watch status

Show watch folder system status

### Synopsis

Display the overall status of the watch folder system.

Shows whether watching is globally paused, how many folders are being
watched, how many should be watched but are not (folder missing, volume
unmounted, or the operating system refused the watch), and the total count
of configured folders.

```
mp watch status [flags]
```

### Examples

```
  # Show watch status
  mp watch status

  # Show status as JSON
  mp watch status --json
```

### Options

```
  -h, --help   help for status
```

### Options inherited from parent commands

```
      --json   Output results as JSON
```

### SEE ALSO

* [mp watch](#mp-watch)	 - Manage watch folders


---

## mp watch update

Update a watch folder's settings

### Synopsis

Modify the configuration of an existing watch folder.

Only the flags you provide will be updated; other settings remain unchanged.

```
mp watch update <id> [flags]
```

### Examples

```
  # Change filter mode
  mp watch update 1 --filter presets --presets images,videos

  # Set auto-tag
  mp watch update 1 --auto-tag screenshots

  # Change regex pattern
  mp watch update 1 --filter custom --regex "\\.(png|jpg)$"
```

### Options

```
      --auto-archive      Automatically archive imported clips
      --auto-tag string   Tag name or ID to auto-assign
      --filter string     File filter mode: all, presets, or custom
  -h, --help              help for update
      --presets string    Comma-separated preset names (images,videos,documents)
      --regex string      Custom regex filter pattern
```

### Options inherited from parent commands

```
      --json   Output results as JSON
```

### SEE ALSO

* [mp watch](#mp-watch)	 - Manage watch folders


---

