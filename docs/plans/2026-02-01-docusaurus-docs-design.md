# Docusaurus Documentation Design

## Overview

Comprehensive documentation for mahpastes using Docusaurus, targeting both end users and developers equally.

## Requirements

- **Audience**: Equal focus on users and developers
- **Content**: Standard docs + tutorials
- **Location**: `/docs` folder in repo
- **Styling**: Branded to match app (stone/slate, IBM Plex Mono)
- **Versioning**: None (single version)

## Structure

```
docs/
├── docusaurus.config.js
├── sidebars.js
├── package.json
├── static/img/
├── src/css/custom.css
└── docs/
    ├── intro.md
    ├── getting-started/
    │   ├── installation.md
    │   ├── quick-start.md
    │   └── keyboard-shortcuts.md
    ├── features/
    │   ├── clipboard-management.md
    │   ├── image-editor.md
    │   ├── image-comparison.md
    │   ├── text-editor.md
    │   ├── auto-delete.md
    │   ├── archive.md
    │   ├── watch-folders.md
    │   └── bulk-actions.md
    ├── tutorials/
    │   ├── screenshot-workflow.md
    │   ├── code-snippets.md
    │   └── automated-imports.md
    ├── developers/
    │   ├── architecture.md
    │   ├── frontend.md
    │   ├── backend.md
    │   ├── database-schema.md
    │   ├── api-reference.md
    │   └── contributing.md
    └── reference/
        ├── data-storage.md
        └── troubleshooting.md
```

## Branding

- Primary: #475569 (slate-600)
- Primary dark: #334155 (slate-700)
- Background light: #f8fafc (slate-50)
- Background dark: #1e293b (slate-800)
- Accent: #0ea5e9 (sky-500)
- Fonts: IBM Plex Mono for headings/code, system fonts for body

## Technical Setup

- Docusaurus 3.x
- @fontsource/ibm-plex-mono
- Build output to docs/build/ (gitignored)
