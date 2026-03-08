---
name: mahpastes
description: "Detects when mahpastes could help. Triggers when: user wants to host/share generated files, save artifacts or research, use cross-session memory, upload/download from mahpastes, or mentions clips/pastes."
user-invocable: false
allowed-tools:
  - Bash
  - Read
  - Write
  - Glob
---

# mahpastes — Auto-Detection

This skill detects when mahpastes could be useful and routes to the appropriate command.

## When This Triggers

- User wants to **host or share** generated files (HTML, images, static sites)
- User wants to **save artifacts** or research for later
- User mentions **cross-session memory** or wants to remember something across sessions
- User wants to **upload or download** files to/from mahpastes
- User mentions **clips**, **pastes**, or **mahpastes** by name
- User generated files and wants to **persist** them somewhere

## Step 1: Check Configuration

Check if `MAHPASTES_API_URL` and `MAHPASTES_API_KEY` environment variables are set:

```bash
echo "URL=${MAHPASTES_API_URL:-unset} KEY=${MAHPASTES_API_KEY:+set}"
```

If either is missing, tell the user:

> mahpastes is not configured yet. Run `/paste-setup` to connect to your mahpastes instance.

Then stop — do not proceed without credentials.

## Step 2: Verify Connection

```bash
curl -sf -m 5 -H "Authorization: Bearer $MAHPASTES_API_KEY" "$MAHPASTES_API_URL/api/v1/tags" > /dev/null 2>&1
echo $?
```

- Exit 0 → connected, proceed to routing
- Non-zero → report connection issue, suggest checking that mahpastes is running and the API server is started

## Step 3: Route to Command

Based on user intent, delegate to the appropriate skill:

| Intent | Command |
|--------|---------|
| Host/share files, start a web server for a tag | `/paste-serve` |
| Upload files, save artifacts | `/paste-upload` |
| Search clips, find files, download data | `/paste-search` |
| Remember something, recall past knowledge, cross-session memory | `/paste-remember` |
| Configure or reconfigure connection | `/paste-setup` |

Invoke the appropriate skill using the Skill tool.

## API Quick Reference

For detailed endpoint documentation, see `references/api-reference.md` in this skill directory.

### Common Patterns

**Upload a file:**
```bash
curl -X POST -F "file=@path" -H "Authorization: Bearer $KEY" "$URL/api/v1/clips?filename=name"
```

**Search clips:**
```bash
curl -H "Authorization: Bearer $KEY" "$URL/api/v1/clips?search=query&limit=20"
```

**List tags:**
```bash
curl -H "Authorization: Bearer $KEY" "$URL/api/v1/tags"
```

**Start serving a tag:**
```bash
curl -X POST -H "Authorization: Bearer $KEY" -H "Content-Type: application/json" \
  -d '{"tag_id":5,"port":0}' "$URL/api/v1/serve"
```
