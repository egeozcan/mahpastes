# SPA Examples Design — Expense Tracker, Bookmarks, Habit Tracker

**Date**: 2026-03-11
**Status**: Approved

## Overview

Three new self-contained SPA examples under `examples/SPAs/`, each demonstrating the mahpastes JSON API (`/_api`) through a practical mini-app. All follow the established pattern from the kanban example: single `index.html` + companion `.json` data file, stone-based design system, IBM Plex Mono, API log panel.

---

## 1. Expense Tracker (`examples/SPAs/expenses/`)

### Files
- `index.html` — SPA
- `expenses.json` — seed data

### JSON Structure
```json
{
  "title": "Expense Tracker",
  "categories": [
    {
      "id": 1,
      "name": "Food",
      "budget": 500,
      "entries": [
        { "id": 1, "description": "Groceries", "amount": 42.50, "date": "2026-03-10" }
      ]
    }
  ]
}
```

### UI Layout
- **Header**: Title (editable via PATCH) + total spent summary
- **Category cards**: Each shows category name, budget, spent total, and entries table
- **Entries table**: Compact rows with description, amount, date. Hover actions (edit/delete)
- **Add entry**: Inline form at bottom of each category card
- **Add category**: Dashed button at end (same pattern as kanban's add-column)
- **API log panel**: Bottom footer

### API Methods Exercised
- GET: Load all data, read individual categories
- POST: Add categories, add entries
- PATCH: Rename categories, edit entry fields, update budgets
- PUT: Replace entire entry
- DELETE: Remove entries, remove empty categories

---

## 2. Bookmarks (`examples/SPAs/bookmarks/`)

### Files
- `index.html` — SPA
- `bookmarks.json` — seed data

### JSON Structure
```json
{
  "title": "Bookmarks",
  "folders": [
    {
      "id": 1,
      "name": "Dev Tools",
      "links": [
        { "id": 1, "url": "https://example.com", "title": "Example", "note": "", "favorite": false, "added": "2026-03-10T12:00:00Z" }
      ]
    }
  ]
}
```

### UI Layout
- **Two-pane layout**: Folder sidebar (left, narrow) + link list (right, main)
- **Sidebar**: Folder names with link counts, active state, add folder button
- **Link list**: Cards with title, truncated URL, note preview, favorite star, hover actions
- **Add link**: Input bar at top of link list (URL + title)
- **API log panel**: Bottom footer

### API Methods Exercised
- GET: Load all data, read individual folders
- POST: Add folders, add links
- PATCH: Toggle favorite, edit notes/title, rename folders
- PUT: Replace entire link (edit form)
- DELETE: Remove links, remove empty folders

---

## 3. Habit Tracker (`examples/SPAs/habits/`) — Atomic Habits inspired

### Concept
Inspired by James Clear's Atomic Habits — focuses on "don't break the chain" streak tracking, identity-based habits, and making habits visible. The core idea: track daily completions on a visual grid and watch streaks grow.

### Files
- `index.html` — SPA
- `tracker.json` — seed data

### JSON Structure
```json
{
  "title": "Atomic Habits",
  "habits": [
    {
      "id": 1,
      "name": "Read 10 pages",
      "identity": "I am a reader",
      "cue": "After morning coffee",
      "streak": 0
    }
  ],
  "log": [
    { "id": 1, "habitId": 1, "date": "2026-03-10", "done": true }
  ]
}
```

### UI Layout
- **Header**: Title + motivational subtitle ("Every action is a vote for the person you want to become")
- **Habit rows**: Each habit as a row with name, identity label, 7-day dot grid (current week), streak counter
- **Day dots**: Filled = completed, empty = missed, today highlighted. Click to toggle (POST/DELETE log entry)
- **Streak display**: Current streak count with "chain" metaphor
- **Add habit form**: Bottom section with name, identity ("I am a..."), cue ("After...")
- **Week navigation**: Prev/next week arrows to view history
- **API log panel**: Bottom footer

### API Methods Exercised
- GET: Load all data
- POST: Add habits, add log entries
- PATCH: Edit habit details, toggle log entries, update streak counts
- PUT: Replace entire habit
- DELETE: Remove habits, remove log entries

---

## Shared Patterns

All three SPAs share:
- Stone color palette (no accent colors except red for errors)
- IBM Plex Mono via Google Fonts
- Inline SVG icons (stroke="currentColor", stroke-width="1.5")
- XSS-safe text escaping
- API log panel footer (collapsible, with method-colored badges)
- Error banner at top
- Same setup instructions in HTML comment header
- `cubic-bezier(0.4, 0, 0.2, 1)` transition timing
- Accessible: aria-labels, semantic HTML, keyboard support
