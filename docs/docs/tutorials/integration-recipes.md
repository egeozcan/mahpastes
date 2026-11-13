---
sidebar_position: 5
---

# Integration Recipes

Copy-paste snippets that wire `mp` into the tools you already use. Swap in your own tag names. Set `MP_API_KEY` (and `MP_API_URL` if you changed the port) first, as described in the [CLI Workflow tutorial](cli-workflow.md#setup).

:::note
Recipes that only read or write clips work against both the desktop app and the headless `mahpastesd` server. `mp clipboard copy` needs the desktop app: `mahpastesd` has no system clipboard and answers 501.
:::

Tags must exist before you upload into them (`mp tag create notes`), otherwise the upload fails with `tag not found`. The recipes below assume the tags `notes` and `git-stash` exist.

## Shell: pbcopy / pbpaste style

`mp clip upload` reads stdin when given no files, and `mp clip data --latest` prints the newest clip, so two functions give you `pbcopy`/`pbpaste` behaviour backed by mahpastes history. Add them to `~/.bashrc` or `~/.zshrc`:

```bash
# mpcopy [filename]  -- stdin -> new clip, prints the clip ID
mpcopy() {
  if [ $# -gt 0 ]; then mp clip upload --filename "$1" -q; else mp clip upload -q; fi
}

# mppaste [--tag NAME]  -- newest clip -> stdout
mppaste() { mp clip data --latest "$@"; }

alias mpyank='mp clip upload --tag notes --filename note.txt -q'
```

```bash
echo "hello" | mpcopy greeting.txt
mppaste                 # hello
mppaste --tag notes     # newest clip tagged "notes"
git diff | mpcopy change.diff
mpcopy < screenshot.png # no name: the type is sniffed, stored as an image
```

Without `--filename`, the clip is named `paste-<timestamp>.<ext>` with the extension sniffed from the content. With `--filename`, the extension decides the type, so do not give binary data a `.txt` name. Identical content is de-duplicated, so re-uploading the same text returns the existing clip ID. `mp` reports that clip as `Already exists` and leaves it as it is: `--tag` and `--expire` only apply to clips the upload created, so a temporary upload never puts an expiry on a clip you already keep.

For binary clips, text that is not valid UTF-8, and text clips carrying control characters (escape sequences other than colours, a carriage return without a line feed, bidirectional overrides), `mp clip data` refuses to write to a terminal; redirect it (`mppaste > out.png`) or add `--force`. Piped and redirected output is never filtered.

## fzf picker

Pick a clip by filename, preview it, and print it:

```bash
mp clip list --json --limit 200 \
  | jq -r '.clips[] | "\(.id)\t\(.filename)"' \
  | fzf --delimiter '\t' --with-nth 2 \
        --preview 'mp clip data {1} | head -50' \
  | cut -f1 \
  | xargs -r mp clip data
```

`mp clip list` returns the 50 newest clips by default and at most 200 per call, so `--limit 200` widens the pick list; add `--tag` or `--search` to narrow it instead. `xargs -r` does nothing when you cancel fzf with Esc.

To put the pick on the system clipboard instead (desktop app required), end the pipeline with `xargs -r mp clipboard copy` rather than `xargs -r mp clip data`.

Wrap it as a function:

```bash
mpfind() {
  mp clip list --json --limit 200 "$@" \
    | jq -r '.clips[] | "\(.id)\t\(.filename)"' \
    | fzf --delimiter '\t' --with-nth 2 --preview 'mp clip data {1} | head -50' \
    | cut -f1 | xargs -r mp clip data
}
# mpfind --tag notes
```

`mp clip list -q` prints bare IDs, which is handy when you do not need names: `mp clip list -q --tag notes | head -5`.

## tmux

Bind a key to send the current tmux paste buffer to mahpastes, and another to load the newest clip into the buffer. In `~/.tmux.conf`:

```tmux
# prefix + C-y : paste buffer -> new clip
bind-key C-y run-shell 'tmux show-buffer | mp clip upload --filename tmux-buffer.txt --tag notes -q >/dev/null && tmux display-message "saved to mahpastes"'

# prefix + C-p : newest clip -> paste buffer
bind-key C-p run-shell 'mp clip data --latest | tmux load-buffer - && tmux display-message "loaded newest clip"'
```

tmux runs `run-shell` commands with the environment of the tmux server, so start the server from a shell where `MP_API_KEY` is exported (or add `export MP_API_KEY=...` inside the quoted command).

## Vim / Neovim

Send the buffer, or a visual selection, to a new clip:

```vim
" :w !cmd feeds the buffer (or the range) to the command's stdin without touching the file
" :MpSave saves the whole buffer; :'<,'>MpSave saves the visual selection
command! -range=% MpSave <line1>,<line2>w !mp clip upload --filename %:t:S -q
```

Put the newest clip below the cursor:

```vim
nnoremap <leader>mp :r !mp clip data --latest<CR>
```

`%:t` expands to the current file name, so the clip keeps its name (and syntax highlighting in mahpastes); the `:S` modifier quotes it for the shell, so names with spaces work. For an unnamed buffer use a literal `--filename scratch.txt`.

## Git

Stash a diff as a tagged clip that expires on its own:

```bash
git config --global alias.mpstash '!git diff HEAD | mp clip upload --filename "$(basename "$PWD").diff" --tag git-stash --expire 7d -q'
```

```bash
git mpstash                      # prints the new clip ID
mp clip data <id> | git apply    # restore it later
```

`git diff HEAD` covers staged and unstaged changes to tracked files only; `git add` new files first if they belong in the stash. On a clean tree there is nothing to send, and `mp` fails with `no input: stdin is empty`.

A `post-commit` hook that saves every commit as a patch (`.git/hooks/post-commit`, mark it executable):

```bash
#!/bin/sh
git format-patch -1 --stdout HEAD \
  | mp clip upload --filename "$(git rev-parse --short HEAD).patch" --tag git-stash --expire 30d -q >/dev/null 2>&1 || true
```

The trailing `|| true` and the redirects keep a stopped mahpastes from ever blocking a commit.

## File manager actions

Anything that can run a command with the selected paths as arguments can upload them: `mp clip upload FILE...` accepts many files at once. Folders are not uploaded; select the files inside them.

File managers do not read `~/.bashrc` or `~/.zshrc`, so `MP_API_KEY` must be in the desktop session's environment. On Linux, put it in `~/.config/environment.d/mahpastes.conf` (a line `MP_API_KEY=...`, plus `MP_API_URL=...` if you changed the port) and log out and back in. If `mp` is not on the session's `PATH`, use its absolute path (`command -v mp`) in the snippets below.

**KDE Dolphin**: save as `~/.local/share/kio/servicemenus/mahpastes.desktop` and make it executable (`chmod +x`), which Dolphin requires for service menus in your home directory:

```ini
[Desktop Entry]
Type=Service
MimeType=all/allfiles;
Actions=addToMahpastes;

[Desktop Action addToMahpastes]
Name=Add to mahpastes
Icon=edit-paste
Exec=mp clip upload -q %F
```

**GNOME Files (Nautilus)**: save as an executable in `~/.local/share/nautilus/scripts/Add to mahpastes`:

```bash
#!/bin/sh
printf '%s\n' "$NAUTILUS_SCRIPT_SELECTED_FILE_PATHS" | while IFS= read -r f; do
  if [ -f "$f" ]; then mp clip upload -q "$f"; fi
done
```

**Windows Explorer "Send to"**: press `Win+R`, run `shell:sendto`, and save a batch file named `Add to mahpastes.cmd` there:

```bat
@set MP_API_KEY=paste-your-key
@"%USERPROFILE%\go\bin\mp.exe" clip upload -q %*
@if errorlevel 1 pause
```

Replace the `mp.exe` path with the output of `where mp` (the path above is where `make mp-install` puts it by default). Explorer passes the selected files as arguments, which `%*` forwards. The `pause` keeps the window open when an upload fails, so you can read the error. If `MP_API_KEY` is already a user environment variable, drop the first line.

## macOS Quick Action

A Finder / Services Quick Action that adds selected files or selected text to mahpastes.

:::warning
Automator and Shortcuts shell steps do not read `~/.zshrc`, and their `PATH` is minimal. `mp` only reads its credentials from the environment (`MP_API_KEY`, `MP_API_URL`), so the script must set them itself, and it must call `mp` by absolute path. The API must also be enabled (menu drawer > **API**). The key ends up in the workflow, so create a dedicated key for it (`mp api key create quick-action --role editor`; a `viewer` key cannot upload) and revoke that key if the workflow is shared.
:::

1. Find the path to `mp`: run `command -v mp` in Terminal (for example `/usr/local/bin/mp`, or `/Users/you/.local/bin/mp` after `make mp-install`).
2. Open **Automator**, choose **New Document > Quick Action**.
3. Set **Workflow receives current** to **files or folders** in **Finder** to capture files. For selected text, make a second Quick Action that receives **text** in **any application**.
4. Add a **Run Shell Script** action. Set **Shell** to `/bin/zsh`, and **Pass input** to **as arguments** (files) or **to stdin** (text).
5. Paste the script from [`examples/macos-quick-action/add-to-mahpastes.sh`](https://github.com/egeozcan/mahpastes/blob/master/examples/macos-quick-action/add-to-mahpastes.sh), edit the `MP` path and API key at the top, and save as "Add to mahpastes".

The script uploads files with `mp clip upload -q` and text with `mp clip upload --filename selection.txt -q`. Selected folders are skipped. The action then appears under **Services** (right-click > Quick Actions in Finder).

For **Shortcuts**, first turn on **Settings > Advanced > Allow Running Scripts** in the Shortcuts app. Then add a **Run Shell Script** action with the same body, set **Input** to Shortcut Input, and **Pass Input** to "as arguments" for files or "to stdin" for text. In **Shortcut Details**, enable "Use as Quick Action".

## Without the desktop app: mahpastesd

Every recipe except `mp clipboard copy` also works against `mahpastesd`, the headless server, for example on a machine without a desktop session. Build it with `make mahpastesd` (the binary lands in `build/bin/`), then start it:

```bash
MAHPASTESD_PORT=44557 build/bin/mahpastesd
```

- It listens on `127.0.0.1` at `MAHPASTESD_PORT` (default `44557`, the same port `mp` uses by default), and serves a browser UI at that address that you sign in to with an API key.
- It uses the same data directory as the desktop app (override it with `MAHPASTES_DATA_DIR`), and only one of them can run on a data directory at a time.
- On the first start, when no API key exists yet, it creates an admin key and writes it to `bootstrap_admin_key` in the data directory (mode 0600). Export it as `MP_API_KEY`, delete the file, create your own keys (`mp api key create laptop --role editor`), and revoke the bootstrap key (`mp api key list`, then `mp api key revoke <id>`) once you no longer need an admin key.

## Related

- [CLI Workflow](cli-workflow.md)
- [CLI reference](../reference/cli.md)
- [REST API](../features/rest-api.md)
