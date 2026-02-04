---
sidebar_position: 2
---

# Installing Plugins

Add, configure, and manage plugins through the Settings panel.

## Adding a Plugin

1. Open **Settings** (gear icon in header)
2. Navigate to the **Plugins** tab
3. Click **Import Plugin**
4. Select a `.lua` file from your computer
5. Review the permissions requested
6. Click **Install**

The plugin activates immediately after installation.

## Reviewing Permissions

Before installing, review what the plugin requests:

### Network Permissions

Shows which domains the plugin can contact:

```
Network Access:
  api.dropbox.com — GET, POST
  hooks.slack.com — POST
```

Only listed domains with listed methods are allowed.

### Filesystem Permissions

Shows if the plugin wants to read or write files:

```
Filesystem Access:
  Read: Yes
  Write: Yes
```

You'll approve specific folders the first time the plugin tries to access them.

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

## Troubleshooting

### Plugin shows "Error" status

The plugin failed 3 times in a row. Options:
- View logs to diagnose the issue
- Disable and re-enable to retry
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
