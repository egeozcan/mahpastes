---
sidebar_position: 17
---

# Background & Startup

Watch folders, tag servers, the REST API and the mounted clip folders only work while mahpastes is
running. These settings keep it running without its window, and start it when you log in. Find them
under **Settings > Background & Startup** in the desktop app (the headless server has neither).

## Keep Running When the Window Is Closed

With this on, closing the window hides it instead of quitting. Everything keeps working in the
background.

| Platform | Default | Showing the window again | Quitting |
|----------|---------|--------------------------|----------|
| macOS | On | Click the Dock icon | <span className="keyboard-key">Cmd</span> + <span className="keyboard-key">Q</span>, **Quit** in the Dock menu, or **Quit mahpastes** in the menu drawer |
| Windows, Linux | Off | Open mahpastes again from the Start menu, app launcher or a shortcut | <span className="keyboard-key">Ctrl</span> + <span className="keyboard-key">Q</span> or **Quit mahpastes** in the menu drawer |

There is no tray icon on Windows or Linux. A hidden window comes back when you launch mahpastes again:
the new launch hands over to the running one, which shows its window (see
[Opening Files with mahpastes](clipboard-management.md#opening-files-with-mahpastes)). If that handover
is unavailable when mahpastes starts (it logs why), closing the window quits as usual, so a hidden window
can never be stranded.

The setting is read when mahpastes starts. After changing it, the settings show **Takes effect the next
time mahpastes starts.** until you restart.

## Launch at Login

**Launch at login** registers this copy of mahpastes with your system:

| Platform | Where the entry lives |
|----------|-----------------------|
| Linux | `~/.config/autostart/mahpastes.desktop` (or under `$XDG_CONFIG_HOME`) |
| Windows | The `mahpastes` value under `HKEY_CURRENT_USER\Software\Microsoft\Windows\CurrentVersion\Run` |
| macOS | `~/Library/LaunchAgents/io.github.egeozcan.mahpastes.login.plist` |

**Start hidden** (on by default) starts mahpastes at login without showing its window. On macOS click
the Dock icon to show it; elsewhere launch mahpastes again. A login start that finds mahpastes already
running does nothing visible.

The entry points at the app you turned it on from. mahpastes refuses to register a copy that cannot
stay where it is: one run from a temporary directory, or on macOS one that macOS runs from a randomised
"translocated" location because it was opened straight from Downloads or a disk image (move it to
Applications first).

The settings tell you when the entry needs attention instead of changing it behind your back:

- **Switched off by the system**: Windows' Startup apps page, or a desktop's autostart settings, turned
  the entry off. Turning the setting on in mahpastes enables it again.
- **Starts another copy**: the entry was made by a mahpastes in a different location, for example an
  older download. Turn the setting off and on to point it at this copy.
- **Points at a missing app**: the copy that made the entry was moved or deleted. Turn the setting off
  and on again.

Uninstalling with `make uninstall` (or `scripts/linux/install.sh --uninstall`) removes the entry too
(on Linux only when it starts the installed copy).

:::note macOS
macOS may list mahpastes under **System Settings > General > Login Items** as a background item. The
entry is an ordinary per-user LaunchAgent; removing it there or turning the setting off in mahpastes
are equivalent.
:::

## Quitting With Unsaved Changes

Quitting while the image or text editor has unsaved changes asks first, whichever way you quit: the
window's close button, <span className="keyboard-key">Cmd</span>/<span className="keyboard-key">Ctrl</span> +
<span className="keyboard-key">Q</span>, the Dock or the menu drawer. **Keep Editing** cancels the quit;
**Quit** discards the changes. If the question cannot be shown, mahpastes quits rather than staying open
without a way to close it.

Quitting from outside the app (logging out, `kill`, <span className="keyboard-key">Ctrl</span> +
<span className="keyboard-key">C</span> in the terminal that started it, an installer replacing the app)
does not ask: mahpastes shuts down cleanly, stopping plugins and unmounting its folders. If that takes
longer than ten seconds, or the signal is sent a second time, it exits immediately.
