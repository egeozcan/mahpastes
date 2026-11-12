---
sidebar_position: 16
---

# Clip Links

`mahpastes://` links bring the mahpastes window forward and show a clip. Paste one into a note, a
task, a chat message or a script, and opening it later takes you straight back to the clip.

## Getting a Link

Right-click a clip (or use its three-dot menu) and choose **Copy > Link**. The link looks like
`mahpastes://clip/42`, where 42 is the clip's ID (the same ID `mp clip list` shows).

## What a Link Does

| Link | Action |
|------|--------|
| `mahpastes://clip/<id>` | Brings the window forward and shows the clip |
| `mahpastes://show` (or `mahpastes://`) | Only brings the window forward |

Showing a clip means:

- **Images and videos** open in the lightbox. If the clip is on screen in the gallery, the arrow keys
  move to its neighbours as usual.
- **Text clips** open in the text editor. If the editor has unsaved changes, you are asked first.
- **Anything else** (PDFs, archives, other files) is scrolled into view and focused in the gallery,
  where **Open** in its menu opens it in another app.

If mahpastes is not running, opening a link starts it. A link to a clip that has been deleted shows a
"not found" notice.

:::info Links only navigate
Any web page, email or document can contain a `mahpastes://` link, so links never change anything:
they cannot create, edit, delete or copy clips, and they never open a clip in another application.
A link mahpastes does not understand is reported and otherwise ignored.
:::

## Where Links Work

| Platform | Setup |
|----------|-------|
| macOS | Works once the app is in `/Applications` (or anywhere else macOS has registered it, for example after you opened it once). |
| Linux | Works after `make install` (`scripts/linux/install.sh`), whose desktop entry registers the `mahpastes:` scheme. |
| Windows | The zip release does not register the scheme. You can still pass a link on the command line: `mahpastes.exe mahpastes://clip/42`. |

Browsers ask before handing a link to an app; choose to open mahpastes.

## From the Command Line

`mp clip open <id> --in-app` opens the clip's link, so a script can bring a clip up in the window:

```bash
mp clip open 42 --in-app
```

This talks to the desktop app on the same computer through the link, not to the API, so it needs no
API key. Without `--in-app`, `mp clip open` downloads the clip and opens it in its default application
instead (see the [CLI guide](../tutorials/cli-workflow.md#opening-clips)).

On a system where nothing handles `mahpastes:` links, the command fails with an error.
