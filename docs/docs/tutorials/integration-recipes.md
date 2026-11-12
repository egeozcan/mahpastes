---
sidebar_position: 5
---

# Integration Recipes

Copy-paste snippets that wire `mp` into the tools you already use. Every command here was run against a real server; swap in your own tag names. Set `MP_API_KEY` (and `MP_API_URL` if you changed the port) first, as described in the [CLI Workflow tutorial](cli-workflow.md#setup).

:::note
Recipes that only read or write clips work against both the desktop app and the headless `mahpastesd` server. `mp clipboard copy` needs the desktop app: `mahpastesd` has no system clipboard and answers 501.
:::

Tags must exist before you upload into them (`mp tag create notes`), otherwise the upload fails with `tag not found`. The recipes below assume the tags `notes` and `git-stash` exist.

## Shell: pbcopy / pbpaste style

`mp clip upload` reads stdin when given no files, and `mp clip data --latest` prints the newest clip, so two functions give you `pbcopy`/`pbpaste` behaviour backed by mahpastes history. Add them to `~/.bashrc` or `~/.zshrc`:

```bash
# mpcopy [filename]  -- stdin -> new clip, prints the clip ID
mpcopy() { mp clip upload --filename "${1:-clip.txt}" -q; }

# mppaste [--tag NAME]  -- newest clip -> stdout
mppaste() { mp clip data --latest "$@"; }

alias mpyank='mp clip upload --tag notes --filename note.txt -q'
```

```bash
echo "hello" | mpcopy greeting.txt
mppaste                 # hello
mppaste --tag notes     # newest clip tagged "notes"
git diff | mpcopy change.diff
```

Without `--filename`, the clip is named `paste-<timestamp>.<ext>` with the extension sniffed from the content. Identical content is de-duplicated, so re-uploading the same text returns the existing clip ID.

For binary clips, `mp clip data` refuses to write to a terminal; redirect it (`mppaste > out.png`) or add `--force`.

## fzf picker

Pick a clip by filename, preview it, and print or copy it:

```bash
mp clip list --json \
  | jq -r '.clips[] | "\(.id)\t\(.filename)"' \
  | fzf --delimiter '\t' --with-nth 2 \
        --preview 'mp clip data {1} | head -50' \
  | cut -f1 \
  | xargs mp clip data
```

To put the pick on the system clipboard instead (desktop app required), end the pipeline with `xargs mp clipboard copy` rather than `xargs mp clip data`.

Wrap it as a function:

```bash
mpfind() {
  mp clip list --json "$@" \
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
command! -range=% MpSave <line1>,<line2>w !mp clip upload --filename %:t -q
```

Put the newest clip below the cursor:

```vim
nnoremap <leader>mp :r !mp clip data --latest<CR>
```

`%:t` expands to the current file name, so the clip keeps its name (and syntax highlighting in mahpastes). For an unnamed buffer use a literal `--filename scratch.txt`.

## Git

Stash a diff as a tagged clip that expires on its own:

```bash
git config --global alias.mpstash '!git diff HEAD | mp clip upload --filename "$(basename "$PWD").diff" --tag git-stash --expire 7d -q'
```

```bash
git mpstash                      # prints the new clip ID
mp clip data <id> | git apply    # restore it later
```

A `post-commit` hook that saves every commit as a patch (`.git/hooks/post-commit`, mark it executable):

```bash
#!/bin/sh
git format-patch -1 --stdout HEAD \
  | mp clip upload --filename "$(git rev-parse --short HEAD).patch" --tag git-stash --expire 30d -q >/dev/null 2>&1 || true
```

The trailing `|| true` and the redirects keep a stopped mahpastes from ever blocking a commit.

## File manager actions

Anything that can run a command with the selected paths as arguments can upload them: `mp clip upload FILE...` accepts many files at once.

**KDE Dolphin** (`~/.local/share/kio/servicemenus/mahpastes.desktop`):

```ini
[Desktop Entry]
Type=Service
MimeType=all/allfiles;
Actions=addToMahpastes;
X-KDE-Submenu=

[Desktop Action addToMahpastes]
Name=Add to mahpastes
Icon=edit-paste
Exec=mp clip upload -q %F
```

**GNOME Files (Nautilus)**: save as an executable in `~/.local/share/nautilus/scripts/Add to mahpastes`:

```bash
#!/bin/sh
printf '%s\n' "$NAUTILUS_SCRIPT_SELECTED_FILE_PATHS" | while IFS= read -r f; do
  [ -n "$f" ] && mp clip upload -q "$f"
done
```

**Windows Explorer "Send to"**: press `Win+R`, run `shell:sendto`, and create a shortcut with the target
`cmd /c mp.exe clip upload %*`. Set `MP_API_KEY` as a user environment variable so Explorer-launched processes see it.

## macOS Quick Action

A Finder / Services Quick Action that adds selected files or selected text to mahpastes.

:::warning
Automator and Shortcuts shell steps do not read `~/.zshrc`, and their `PATH` is minimal. `mp` only reads its credentials from the environment (`MP_API_KEY`, `MP_API_URL`), so the script must set them itself, and it must call `mp` by absolute path. The API must also be enabled in **Settings > API**. The key ends up in the workflow, so create a dedicated key for it and revoke that key if the workflow is shared.
:::

1. Find the path to `mp`: run `command -v mp` in Terminal (for example `/usr/local/bin/mp` or `~/.local/bin/mp` expanded to a full path).
2. Open **Automator**, choose **New Document > Quick Action**.
3. Set **Workflow receives current** to **files or folders** in **Finder** to capture files. For selected text, make a second Quick Action that receives **text** in **any application**.
4. Add a **Run Shell Script** action. Set **Shell** to `/bin/zsh`, and **Pass input** to **as arguments** (files) or **to stdin** (text).
5. Paste the script from [`examples/macos-quick-action/add-to-mahpastes.sh`](https://github.com/egeozcan/mahpastes/blob/master/examples/macos-quick-action/add-to-mahpastes.sh), edit the `MP` path and API key at the top, and save as "Add to mahpastes".

The script uploads files with `mp clip upload -q "$@"` and text with `mp clip upload --filename selection.txt -q`. The action then appears under **Services** (right-click > Quick Actions in Finder).

For **Shortcuts**, add a **Run Shell Script** action with the same body, set **Input** to Shortcut Input, and **Pass Input** to "as arguments" for files or "to stdin" for text. In **Shortcut Details**, enable "Use as Quick Action".

## Related

- [CLI Workflow](cli-workflow.md)
- [CLI reference](../reference/cli.md)
- [REST API](../features/rest-api.md)
