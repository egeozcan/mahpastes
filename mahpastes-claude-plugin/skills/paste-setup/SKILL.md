---
name: paste-setup
description: "Set up mahpastes connection — configure API URL and key"
user-invocable: true
allowed-tools:
  - Bash
  - Read
  - Write
argument-hint: "[url] [api-key]"
---

# /paste-setup — Configure mahpastes Connection

Set up or verify the connection between Claude Code and a running mahpastes instance.

## Steps

### 1. Check existing configuration

Read `~/.claude/settings.json` and look for `env.MAHPASTES_API_URL` and `env.MAHPASTES_API_KEY`.

```bash
cat ~/.claude/settings.json 2>/dev/null | grep -E "MAHPASTES_API_(URL|KEY)"
```

### 2. If both exist, verify the connection

```bash
curl -sf -H "Authorization: Bearer $MAHPASTES_API_KEY" "$MAHPASTES_API_URL/api/v1/tags"
```

- If the request succeeds → report "mahpastes is already configured and working" with the URL. Ask if the user wants to reconfigure.
- If it fails (connection refused) → warn that mahpastes may not be running, offer to reconfigure.
- If it returns 401 → warn the key may be revoked, offer to reconfigure.

### 3. If missing or reconfiguring

Ask the user for:
1. **API URL** — default: `http://127.0.0.1:8484`
2. **API Key** — the `mp_...` key from the mahpastes API settings

If arguments were provided to the command, use those instead of asking.

### 4. Verify the new credentials

```bash
curl -sf -H "Authorization: Bearer $KEY" "$URL/api/v1/tags"
```

If this fails, report the error and ask the user to check that:
- mahpastes is running
- The API server is started (Settings → API → Start Server)
- The API key is valid and not revoked

### 5. Save to settings

Merge into `~/.claude/settings.json`, preserving all existing keys. Try `jq` first, fall back to `python3`:

```bash
# jq approach
jq --arg url "$URL" --arg key "$KEY" \
  '.env = (.env // {}) | .env.MAHPASTES_API_URL = $url | .env.MAHPASTES_API_KEY = $key' \
  ~/.claude/settings.json > /tmp/.claude-settings-merge.json \
  && jq empty /tmp/.claude-settings-merge.json \
  && mv /tmp/.claude-settings-merge.json ~/.claude/settings.json
```

```bash
# python3 fallback (if jq not installed)
python3 -c "
import json, sys
path = sys.argv[1]
try:
    with open(path) as f: data = json.load(f)
except: data = {}
data.setdefault('env', {})
data['env']['MAHPASTES_API_URL'] = sys.argv[2]
data['env']['MAHPASTES_API_KEY'] = sys.argv[3]
with open(path, 'w') as f: json.dump(data, f, indent=2)
    f.write('\n')
" ~/.claude/settings.json "$URL" "$KEY"
```

### 6. Create `claude-memory` tag

Create a tag for cross-session memory if it doesn't already exist:

```bash
# Check if tag exists
curl -sf -H "Authorization: Bearer $KEY" "$URL/api/v1/tags" | grep -q '"claude-memory"'

# If not, create it
curl -sf -X POST -H "Authorization: Bearer $KEY" -H "Content-Type: application/json" \
  -d '{"name":"claude-memory"}' "$URL/api/v1/tags"
```

### 7. Report success

Tell the user:
- Configuration saved to `~/.claude/settings.json`
- Environment variables `MAHPASTES_API_URL` and `MAHPASTES_API_KEY` are set
- **Important:** These env vars take effect in the next Claude Code session (restart required)
- The `claude-memory` tag was created (or already existed)

## Edge Cases

| Condition | Behavior |
|-----------|----------|
| `~/.claude/settings.json` doesn't exist | Create it with just the env block |
| `~/.claude/settings.json` is invalid JSON | Warn user, ask before overwriting |
| Neither `jq` nor `python3` available | Write the JSON directly with the Write tool |
| Connection refused | mahpastes not running or API not started |
| 401 response | Key is invalid or revoked |
