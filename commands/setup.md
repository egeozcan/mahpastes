---
description: Set up or reconfigure the mahpastes API connection
allowed-tools: Bash, Read, Write, Edit
---

Set up the mahpastes API connection for Claude Code.

## Step 1: Check Existing Configuration

Read `~/.claude/settings.json`. Check if `env.MP_API_URL` and `env.MP_API_KEY` exist and are non-empty. These are the variables the `mp` CLI reads, so one configuration serves both the plugin and `mp`.

If both are set, verify connectivity:

```bash
curl -sf -o /dev/null -w "%{http_code}" \
  -H "Authorization: Bearer $MP_API_KEY" \
  "$MP_API_URL/api/v1/tags"
```

If this returns `200`, inform the user that mahpastes is already configured and working. Ask if they want to reconfigure. If they don't, stop here.

## Step 2: Determine API URL

Try the default URL `http://127.0.0.1:44557` (the default port of the app's API server and of `mp`):

```bash
curl -sf -o /dev/null -w "%{http_code}" http://127.0.0.1:44557/api/v1/status 2>/dev/null || echo "UNREACHABLE"
```

`GET /api/v1/status` needs no key. A `200` response means the API is running on the default port.

If unreachable, tell the user to:
1. Open the mahpastes app
2. Open the menu drawer (press M) and click **API**
3. Click **Start API** and note the port number

Ask the user for the port if it differs from 44557, then construct the URL as `http://127.0.0.1:PORT`.

## Step 3: Get API Key

Tell the user to create an API key in the mahpastes app:

1. Open mahpastes, open the menu drawer (press M) and click **API**
2. Click **Create Key**
3. Set name to **Claude Code**
4. Set role to **admin** (required for full functionality including tag-serve)
5. Tag scope is optional — leave blank for full access, or select a tag to restrict the key to only clips in that tag
6. Copy the generated key — it starts with `mp_` and is **only shown once**

Ask the user to paste the API key.

**Note on scoped keys**: If the user chose a tag scope, the key will only access clips within that tag. Uploads are auto-tagged. Tag creation/deletion is blocked. This is fine for dedicated-purpose keys (e.g., a hosting key scoped to a site tag) but limits multi-tag workflows like research organization.

## Step 4: Save Configuration

Read `~/.claude/settings.json` (create with `{}` if it doesn't exist). Merge the mahpastes env vars into the existing `env` object, preserving all other settings:

- Set `env.MP_API_URL` to the URL from step 2
- Set `env.MP_API_KEY` to the key from step 3

Only `MP_*` names are written. The `mp` CLI also accepts the older `MAHPASTES_API_URL` / `MAHPASTES_API_KEY` names as fallbacks, so existing settings written by an earlier version of this command keep working for `mp`. Re-running setup replaces them with `MP_*` values.

Write the updated JSON back to `~/.claude/settings.json` with proper formatting.

## Step 5: Verify

Test the saved credentials:

```bash
curl -sf -o /dev/null -w "%{http_code}" \
  -H "Authorization: Bearer THE_KEY" \
  "THE_URL/api/v1/tags"
```

If successful (200), check if the key is tag-scoped by examining the response:

```bash
curl -s -H "Authorization: Bearer THE_KEY" "THE_URL/api/v1/tags"
```

If the response is a single-element array, the key is scoped to that tag. Inform the user of the scope and its implications (auto-tagging, no tag CRUD, restricted visibility).

Tell the user:
- Setup is complete
- If scoped: mention which tag the key is scoped to and that uploads will auto-tag
- The environment variables will be available in new Claude Code sessions
- They may need to restart the current session for changes to take effect
- Run `/mahpastes:setup` anytime to reconfigure
