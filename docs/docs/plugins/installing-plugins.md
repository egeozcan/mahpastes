---
sidebar_position: 2
---

# Installing Plugins

Add, configure, and manage plugins through the Settings panel.

## Adding a Plugin from File

1. Open **Settings** (gear icon in header)
2. Navigate to the **Plugins** tab
3. Click **Import Plugin**
4. Select a `.lua` file from your computer
5. Review the permissions requested
6. Click **Install**

The plugin activates immediately after installation.

## Installing from URL

You can install plugins directly from a URL:

1. Open **Settings** → **Plugins**
2. Click the **link icon** to show the URL input
3. Paste a URL pointing to a `.lua` plugin file
4. Click **Install from URL**
5. Review the plugin's name, version, permissions, and events
6. Click **Confirm** to install

URL-installed plugins remember their source URL, enabling automatic update checks.

The plugin source is fetched, parsed, and cached before the review dialog appears. The cached copy is used for installation, so the remote source cannot change between your review and the actual install.

:::note
Only `http://` and `https://` URLs are supported. The plugin file must be under 1MB.
:::

## Reviewing Permissions

Before installing, review what the plugin requests:

### Network Permissions

Shows which domains the plugin can contact:

```
Network Access:
  api.dropbox.com — GET, POST
  hooks.slack.com — POST
```

Only listed domains with listed methods are allowed. Wildcard subdomains (e.g., `*.cdn.example.com`) may also appear.

### Filesystem Permissions

Shows if the plugin wants to read or write files:

```
Filesystem Access:
  Read: Yes
  Write: Yes
```

You'll approve specific folders the first time the plugin tries to access them. Approving a parent directory covers all files and subdirectories within it.

### Clipboard Permission

Shows if the plugin can write to your system clipboard:

```
Clipboard Access: Yes
```

## Configuring Plugin Settings

Some plugins have configurable settings:

1. Click the **gear icon** next to a plugin
2. Fill in the settings form
3. Click **Save**

Settings types include:
- **Text** — Free-form input
- **Password** — Hidden input (for API keys)
- **Checkbox** — On/off toggle
- **Select** — Dropdown choices

## Enabling and Disabling

Toggle a plugin on/off without removing it:

1. Find the plugin in the list
2. Click the **toggle switch**

Disabled plugins:
- Don't respond to events
- Don't run scheduled tasks
- Keep their settings and permissions

## Viewing Plugin Logs

See what a plugin is doing:

1. Click the **log icon** next to a plugin
2. View recent log entries

Logs show:
- Handler executions with timestamps
- Errors and warnings
- Custom `log()` messages from the plugin

## Revoking Permissions

Remove filesystem permissions granted to a plugin:

1. Click the **permissions icon** next to a plugin
2. View granted folder permissions
3. Click **Revoke** next to any permission

The plugin will need to request access again.

## Removing a Plugin

1. Click the **delete icon** next to a plugin
2. Confirm removal

This removes:
- The plugin code
- All granted permissions
- Plugin storage data

:::note
Plugin settings and storage are deleted when you remove a plugin. Export any important data first.
:::

## Updating Plugins

### Manual Updates

For URL-installed plugins, click the **update** button next to the plugin to check for a new version.

- If the update has **no permission changes**, it applies immediately.
- If the update has **new or changed permissions** (network domains, filesystem, clipboard, events), you'll see a review dialog before confirming.
- If the update fails to load, the previous version is automatically restored.

### Automatic Update Checks

Configure how often mahpastes checks for plugin updates:

1. Open **Settings** → **Plugins**
2. Find the **Update Check Interval** setting
3. Choose an interval:
   - **Startup only** — Check once when the app launches
   - **Every 6 hours**
   - **Every 24 hours**
   - **Disabled** — Never check automatically

When an update is available, a badge appears on the plugin card.

## Troubleshooting

### Plugin shows "Error" status

The plugin is auto-disabled after 3 consecutive handler errors. It stops receiving events and running scheduled tasks until you re-enable it. Options:
- View logs to diagnose the issue
- Disable and re-enable to reset the error counter and retry
- Remove and reinstall if the plugin was updated

### Plugin not responding to events

Check that:
- The plugin is enabled (toggle is on)
- The plugin subscribes to that event (check plugin docs)
- No errors in the plugin log

### Filesystem prompts appearing repeatedly

The plugin is accessing new folders. Either:
- Approve the folders it needs
- Check if the plugin is misconfigured
