---
sidebar_position: 2
---

# Managing Code Snippets

Use mahpastes as a personal code snippet library. Store, organize, and reuse code fragments across projects.

## Why Use mahpastes for Snippets?

- **Quick access**: Faster than searching through old projects
- **Cross-project**: Snippets available regardless of current project
- **Editable**: Modify snippets before using
- **Searchable**: Find snippets by filename

## Saving Code Snippets

### From Your Editor

1. Select code in your editor
2. Copy (<span className="keyboard-key">Cmd</span>/<span className="keyboard-key">Ctrl</span> + <span className="keyboard-key">C</span>)
3. Switch to mahpastes
4. Paste (<span className="keyboard-key">Cmd</span>/<span className="keyboard-key">Ctrl</span> + <span className="keyboard-key">V</span>)

The code is saved as a text clip.

### Best Practices for Saving

**Use descriptive paste context**: When you paste, the clip is named `pasted_text.txt`. For better organization, consider:

1. Creating a text file with a descriptive name
2. Pasting your code into it
3. Dragging the file into mahpastes

Example names:
- `react-useEffect-cleanup.js`
- `python-async-retry-pattern.py`
- `sql-pagination-query.sql`

## Organizing Snippets

### Archive Important Snippets

Keep valuable snippets in the archive:

1. Find the snippet in the gallery
2. Click the archive button
3. Snippet moves to Archive tab

Benefits:
- Won't be accidentally deleted
- Separate from temporary clips
- Easy to find in Archive

### Use Search

Filter clips by filename:

1. Type in the search bar
2. Results filter instantly
3. Find your snippet quickly

Search tips:
- Search by language: "python", "react", "sql"
- Search by purpose: "retry", "pagination", "auth"
- Search by project: "myapp", "api"

## Using Snippets

### Copy and Paste

1. Find your snippet
2. Click the copy button (or double-click)
3. Paste into your editor

### Modify Before Using

Often you need to customize a snippet:

1. Click the edit button on the snippet
2. Modify the code in the editor
3. Either:
   - Save changes (updates the stored snippet)
   - Copy the text and cancel (keeps original)

### Copy Path for CLI

Some tools work better with file paths:

1. Click the path icon
2. Path is copied to clipboard
3. Use in terminal:

```bash
# View with syntax highlighting (if bat installed)
bat $(pbpaste)

# Source a shell script
source $(pbpaste)

# Run a Python script
python $(pbpaste)
```

## Snippet Categories

### Configuration Templates

Store config file templates:

```json
// package.json template
{
  "name": "my-project",
  "version": "1.0.0",
  "scripts": {
    "start": "node index.js",
    "test": "jest"
  }
}
```

Archive these for repeated use.

### Utility Functions

Common helper functions:

```javascript
// Debounce function
function debounce(fn, delay) {
  let timeout;
  return (...args) => {
    clearTimeout(timeout);
    timeout = setTimeout(() => fn(...args), delay);
  };
}
```

### API Patterns

Request patterns you use often:

```python
# Retry with exponential backoff
async def retry_request(fn, max_retries=3):
    for attempt in range(max_retries):
        try:
            return await fn()
        except Exception as e:
            if attempt == max_retries - 1:
                raise
            await asyncio.sleep(2 ** attempt)
```

### SQL Queries

Reusable query patterns:

```sql
-- Pagination query
SELECT *
FROM items
WHERE id > :last_id
ORDER BY id
LIMIT :page_size
```

### Shell Commands

Complex commands you want to remember:

```bash
# Find and delete node_modules
find . -name "node_modules" -type d -prune -exec rm -rf {} +

# Git log with graph
git log --oneline --graph --all --decorate
```

## Workflow Examples

### Starting a New Project

1. Open Archive in mahpastes
2. Search for "template" or project type
3. Copy configuration files
4. Paste into new project
5. Customize as needed

### Implementing a Pattern

1. Search for the pattern name
2. Open snippet in editor
3. Customize for current context
4. Copy the modified version
5. Paste into your code

### Sharing with Team

1. Find relevant snippets
2. Select multiple (Shift-click)
3. Download as ZIP
4. Share with team
5. Each person imports into their mahpastes

## Tips

### Naming Conventions

Use consistent naming:
- `language-purpose-variant.ext`
- `react-form-validation.js`
- `python-db-connection-pool.py`

### Keep Snippets Focused

Each snippet should do one thing:
- ✅ Single function
- ✅ One config file
- ❌ Entire module with many functions
- ❌ Multiple unrelated utilities

### Update Regularly

When you improve a snippet:
1. Edit it in mahpastes
2. Save the updated version
3. Old version is replaced

### Version Control Alternative

For extensive snippet libraries, consider:
- A Git repository of snippets
- Watch folder pointing to the repo
- mahpastes auto-imports updates

## Limitations

mahpastes is great for quick access to snippets but isn't a replacement for:
- Full-featured snippet managers with variables
- IDE snippet expansion
- Version-controlled snippet repositories

Use mahpastes for:
- Quick capture of useful code
- Cross-project code storage
- Temporary code storage during development
