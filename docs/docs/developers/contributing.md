---
sidebar_position: 6
---

# Contributing

Thank you for your interest in contributing to mahpastes! This guide will help you get started.

## Development Setup

### Prerequisites

| Tool | Version | Purpose |
|------|---------|---------|
| [Go](https://go.dev/dl/) | 1.21+ | Backend development |
| [Node.js](https://nodejs.org/) | 18+ | Frontend tooling |
| [Wails CLI](https://wails.io/) | 2.x | Build and development |

### Install Wails CLI

```bash
go install github.com/wailsapp/wails/v2/cmd/wails@latest
```

Verify installation:

```bash
wails doctor
```

### Clone and Setup

```bash
# Clone the repository
git clone https://github.com/egeozcan/mahpastes.git
cd mahpastes

# Install frontend dependencies
cd frontend && npm install && cd ..
```

### Run Development Mode

```bash
wails dev
```

This starts the app with:
- Hot reload for frontend changes
- Automatic rebuild for Go changes
- Dev tools access

## Project Structure

```
mahpastes/
├── main.go              # Entry point
├── app.go               # Core logic, API
├── database.go          # SQLite
├── watcher.go           # File watching
├── go.mod               # Go dependencies
├── wails.json           # Wails config
├── frontend/
│   ├── index.html       # Main UI
│   ├── js/              # JavaScript modules
│   ├── css/             # Styles
│   └── package.json     # Frontend deps
└── build/               # Build output
```

## Making Changes

### Backend Changes

1. Edit Go files (`*.go`)
2. Wails dev auto-rebuilds
3. Test your changes
4. If adding new API methods, they're automatically exposed

### Frontend Changes

1. Edit HTML/CSS/JS in `frontend/`
2. Changes hot-reload automatically
3. If editing Tailwind classes, ensure `npm run watch` is running

### Adding New API Methods

1. Add method to `app.go`:
   ```go
   func (a *App) MyNewMethod(param string) (string, error) {
       // Implementation
       return result, nil
   }
   ```

2. Wails auto-generates bindings in `frontend/wailsjs/go/main/App.js`

3. Call from JavaScript:
   ```javascript
   import { MyNewMethod } from '../wailsjs/go/main/App';
   const result = await MyNewMethod("param");
   ```

## Code Style

### Go

- Follow standard Go conventions
- Use `gofmt` for formatting
- Handle errors explicitly
- Add comments for exported functions

```go
// GetClipData retrieves full clip data by ID.
// Returns an error if the clip is not found.
func (a *App) GetClipData(id int64) (*ClipData, error) {
    // ...
}
```

### JavaScript

- Use async/await for promises
- Cache DOM queries
- Use meaningful variable names
- Add comments for complex logic

```javascript
// Load and render clips for the current view mode
async function loadClips() {
    const clips = await getClips(isViewingArchive);
    renderClips(clips);
}
```

### CSS

- Prefer Tailwind utility classes
- Custom CSS only when necessary
- Keep specificity low
- Mobile-responsive where applicable

## Testing Changes

### Manual Testing Checklist

Before submitting:

- [ ] Paste text content
- [ ] Paste images
- [ ] Drag and drop files
- [ ] Archive/unarchive clips
- [ ] Set and cancel expiration
- [ ] Bulk operations (select, delete, archive)
- [ ] Image editor (all tools, undo/redo)
- [ ] Text editor
- [ ] Watch folders (add, pause, remove)
- [ ] Lightbox navigation
- [ ] Search filtering

### Cross-Platform

If possible, test on multiple platforms:
- macOS (primary development platform)
- Windows
- Linux

## Submitting Changes

### Fork and Branch

1. Fork the repository
2. Create a feature branch:
   ```bash
   git checkout -b feature/my-new-feature
   ```

### Commit Messages

Write clear, concise commit messages:

```
Add watch folder filter validation

- Validate regex patterns before saving
- Show error message for invalid patterns
- Prevent saving invalid configurations
```

### Pull Request

1. Push your branch:
   ```bash
   git push origin feature/my-new-feature
   ```

2. Open a Pull Request on GitHub

3. Include in description:
   - What the change does
   - Why it's needed
   - How to test it
   - Screenshots for UI changes

### PR Checklist

- [ ] Code follows project style
- [ ] Changes tested locally
- [ ] No console errors or warnings
- [ ] Documentation updated if needed

## Bug Reports

When reporting bugs, include:

1. **Environment**: OS, version, how installed
2. **Steps to reproduce**: Numbered steps
3. **Expected behavior**: What should happen
4. **Actual behavior**: What does happen
5. **Screenshots/logs**: If applicable

## Feature Requests

For new features:

1. **Problem**: What problem does this solve?
2. **Solution**: What should the feature do?
3. **Alternatives**: Other ways to solve the problem?
4. **Additional context**: Mockups, examples, etc.

## Getting Help

- Check existing [issues](https://github.com/egeozcan/mahpastes/issues)
- Read the documentation
- Ask in issue comments

## License

By contributing, you agree that your contributions will be licensed under the MIT License.
