---
sidebar_position: 8.5
---

# System Notifications

mahpastes can show a native notification (Notification Center on macOS, a toast on Windows, your desktop's notification service on Linux) when something happens while the window is in the background. In the window itself these events only show as short in-app toasts, which you miss while the window is minimised or behind other apps.

The option is **off by default**. Turn it on in **Settings → System Notifications → Notify me about background events**.

## What Notifies

| Event | Example |
|-------|---------|
| Clips imported by a [watch folder](./watch-folders) or added through the [REST API](./rest-api) / `mp` | "Imported 12 clips" |
| A watch folder import that failed | "Could not import report.pdf: …" |
| Clips received from a followed share | "3 new clips in inbox" |
| A plugin task finished or failed | "Auto Tagger — Finished: Tagging" |
| A plugin message (`toast.show`) | "Watermarker — Saved 4 images" |

Notifications appear only while the mahpastes window does not have focus. When you are looking at the window you get the usual in-app toasts instead.

## Bursts Are Combined

A folder sync can import hundreds of files at once. Instead of one notification per file, events of the same kind are collected for a couple of seconds and shown together ("Imported 12 clips"). Each kind of event notifies at most once every 30 seconds, and no more than 5 notifications appear per minute overall; anything held back is combined into the next one. Coming back to the window drops what is still waiting.

Clicking a notification brings the mahpastes window back. Some Wayland desktops do not let an app raise its own window; there the window is marked as wanting attention instead (for example, a highlighted taskbar entry).

## Testing

When the setting is on, **Send Test Notification** shows one right away, whatever the focus. The button only appears while the setting is on. Use it to check that your system lets mahpastes notifications through (Focus, Do Not Disturb or Focus assist can hide them).

## Platform Notes

### macOS

macOS asks for permission the first time you turn the setting on. If you declined, allow mahpastes under **System Settings → Notifications** and switch the setting off and on again.

Notifications need the mahpastes `.app` bundle, which carries the bundle identifier macOS keys the permission on. The bare binary that `wails dev` runs cannot show them; Settings then says "Not available" with the reason. If the app's bundle identifier changes (for example between a self-built and a released app), macOS asks again.

### Windows

When you turn the setting on (or, if it was already on, at the first notification after a launch), mahpastes registers itself as a notification source in your user registry (`HKEY_CURRENT_USER\Software\Classes\AppUserModelId\mahpastes.exe` and a `CLSID` entry pointing at `mahpastes.exe`). Nothing is written while the setting stays off. Notifications are labelled with the program name, `mahpastes.exe`.

### Linux

mahpastes uses the freedesktop notification service over the session D-Bus, which KDE Plasma, GNOME and most other desktops provide. Without a notification daemon, Settings says "Not available".

## Where the Setting Lives

The setting is stored per machine: it is not included in [backups](./backup-restore) and cannot be changed through the REST API, because switching it on prompts (macOS) or writes registry keys (Windows) on that machine.

Runs with a hidden window (`MAHPASTES_START_HIDDEN=1`, used by the end-to-end tests) never show notifications.
