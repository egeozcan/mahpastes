---
name: paste-remember
description: "Save or recall cross-session memory via mahpastes"
user-invocable: true
allowed-tools:
  - Bash
  - Read
  - Write
argument-hint: "<save|recall|list> [topic or query]"
---

# /paste-remember — Cross-Session Memory

Save and recall knowledge across Claude Code sessions using mahpastes as a persistent store. Memories are stored as markdown clips tagged with `claude-memory`.

## Prerequisites

Requires `MAHPASTES_API_URL` and `MAHPASTES_API_KEY` environment variables. If missing, tell the user to run `/paste-setup` first.

The `claude-memory` tag must exist (created by `/paste-setup`).

## Memory Format

**Filename convention:** `memory-{topic-slug}-{YYYYMMDD-HHMMSS}.md`

**Content format:**

```markdown
# {Topic}
**Date**: {ISO timestamp}
**Context**: {why this was saved}
---
{content}
---
**Tags**: {comma-separated keywords}
```

## Commands

### `save [topic]`

Save a memory about a topic.

1. Ask the user what to remember (or use provided content)
2. Generate a topic slug from the topic name
3. Write the markdown content to a temp file:

```bash
SLUG=$(echo "$TOPIC" | tr '[:upper:]' '[:lower:]' | sed 's/[^a-z0-9]/-/g' | sed 's/--*/-/g' | sed 's/^-//;s/-$//')
TIMESTAMP=$(date +%Y%m%d-%H%M%S)
FILENAME="memory-${SLUG}-${TIMESTAMP}.md"
TMPFILE=$(mktemp /tmp/mahpastes-memory-XXXXXX.md)

cat > "$TMPFILE" << 'MEMEOF'
# {Topic}
**Date**: {timestamp}
**Context**: {context}
---
{content}
---
**Tags**: {keywords}
MEMEOF
```

4. Upload to mahpastes:

```bash
CLIP_ID=$(curl -sf -X POST \
  -F "file=@$TMPFILE" \
  -H "Authorization: Bearer $KEY" \
  "$URL/api/v1/clips?filename=$FILENAME" \
  | python3 -c "import json,sys; print(json.load(sys.stdin)['id'])")
```

5. Tag with `claude-memory`:

```bash
# Find claude-memory tag ID
MEMORY_TAG_ID=$(curl -sf -H "Authorization: Bearer $KEY" "$URL/api/v1/tags" \
  | python3 -c "import json,sys; tags=json.load(sys.stdin); print(next((t['id'] for t in tags if t['name']=='claude-memory'),''))")

# Apply tag
curl -sf -X PUT -H "Authorization: Bearer $KEY" \
  "$URL/api/v1/clips/$CLIP_ID/tags/$MEMORY_TAG_ID"
```

6. Clean up temp file and report success.

### `recall [query]`

Search and retrieve memories.

1. Find the `claude-memory` tag ID
2. Search clips with that tag:

```bash
curl -sf -H "Authorization: Bearer $KEY" \
  "$URL/api/v1/clips?tag=$MEMORY_TAG_ID&search=$QUERY&limit=10"
```

3. For each matching clip, fetch the full content:

```bash
curl -sf -H "Authorization: Bearer $KEY" \
  "$URL/api/v1/clips/$CLIP_ID/data"
```

4. Present the memories to the user, most recent first.

### `list`

List all saved memories.

1. Find the `claude-memory` tag ID
2. Fetch all clips with that tag:

```bash
curl -sf -H "Authorization: Bearer $KEY" \
  "$URL/api/v1/clips?tag=$MEMORY_TAG_ID&limit=50"
```

3. Display as a table:

```
ID   | Topic                  | Date       | Size
-----|------------------------|------------|------
42   | memory-api-patterns    | 2026-03-01 | 2.1 KB
43   | memory-debug-tips      | 2026-03-05 | 1.4 KB
```

## Examples

```
/paste-remember save "API authentication patterns"
/paste-remember recall authentication
/paste-remember list
```

## Edge Cases

| Condition | Behavior |
|-----------|----------|
| `claude-memory` tag missing | Create it automatically |
| No memories found | Report empty, suggest `/paste-remember save` |
| Duplicate topic | Create new entry (memories are append-only by date) |
| Very long content | Truncate display, offer to download full content |
