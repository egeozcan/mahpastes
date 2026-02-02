# mahpastes

A Wails desktop clipboard manager for macOS with image editing, comparison, and watch folder features.

## Tech Stack

- **Backend**: Go with Wails framework
- **Frontend**: Vanilla JavaScript + Tailwind CSS (no build step except Tailwind)
- **Database**: SQLite (via Wails)
- **Testing**: Playwright e2e tests

## E2E Testing Requirements

**CRITICAL: All changes MUST pass e2e tests. Run tests before and after any modification.**

### Test Workflow

1. **Before starting work**: Run `cd e2e && npm test` to verify baseline
2. **Fix any failing tests first**: Even if unrelated to your changes, fix them before proceeding
3. **After making changes**: Run tests again and ensure all pass
4. **Add tests for new functionality**: Every feature/bugfix must have corresponding test coverage

### Running Tests

```bash
cd e2e
npm test              # Run all tests
npm run test:headed   # Run with browser visible (for debugging)
npm run test:debug    # Debug mode with Playwright inspector
npm run test:ui       # Interactive UI mode
```

### Test Organization

Tests are organized by feature in `e2e/tests/`:
- `clips/` - Upload, view, delete, archive operations
- `bulk/` - Multi-select operations
- `images/` - Lightbox, editor, comparison
- `search/` - Filtering functionality
- `watch/` - Watch folders feature
- `edge-cases/` - Error handling, expiration

### Test Abstractions

Use the established patterns in the codebase for consistency:

**AppHelper fixture** (`e2e/fixtures/test-fixtures.ts`): Provides high-level methods for all app interactions:
```typescript
// Example usage
test('should upload and delete a clip', async ({ app }) => {
  const imagePath = await createTempFile(generateTestImage(), 'png');
  await app.uploadFile(imagePath);
  await app.expectClipCount(1);
  await app.deleteClip(path.basename(imagePath));
  await app.expectClipCount(0);
});
```

**Test data helpers** (`e2e/helpers/test-data.ts`): Generate test files:
- `generateTestImage(width, height, color)` - Creates PNG images
- `generateTestText(prefix)` - Creates text content
- `generateTestJSON()` / `generateTestHTML()` - Structured content
- `createTempFile(content, extension)` - Writes to temp location

**Selectors** (`e2e/helpers/selectors.ts`): Centralized DOM selectors - use these instead of hardcoding selectors in tests.

### Writing Good Tests

- Use descriptive test names that explain the behavior being tested
- One assertion focus per test when possible
- Use `app.expectClipVisible()`, `app.expectClipCount()` for assertions
- Clean up is automatic via the fixture's `afterEach`
- Tests run in parallel - each worker gets its own app instance

## Design System

**CRITICAL: All UI changes must match the existing design language exactly.**

### Color Palette (Stone-based)

The app uses Tailwind's `stone` color scale exclusively:
- **Background**: `bg-stone-50` (main), `bg-white` (cards)
- **Text**: `text-stone-800` (primary), `text-stone-600` (secondary), `text-stone-400` (muted)
- **Borders**: `border-stone-200` (default), `border-stone-300` (hover)
- **Interactive**: `bg-stone-800` (buttons), `hover:bg-stone-700`
- **Accents**: Only `stone` variants - no blue, green, or other colors except:
  - Error states: `red-500`, `red-50`
  - Success indicator: `emerald-500` (watch indicator only)

### Typography

- **Font**: IBM Plex Mono (monospace throughout)
- **Sizes**:
  - Headers: `text-sm font-semibold uppercase tracking-wide`
  - Body: `text-xs font-medium`
  - Micro: `text-[9px]`, `text-[10px]`, `text-[11px]` for labels/badges

### Component Patterns

**Buttons**:
```html
<!-- Primary -->
<button class="bg-stone-800 hover:bg-stone-700 text-white text-xs font-medium py-2.5 px-5 rounded-md transition-colors">

<!-- Secondary -->
<button class="border border-stone-200 hover:border-stone-300 hover:bg-stone-100 text-stone-600 text-xs font-medium py-2 px-3 rounded-md transition-colors">
```

**Cards**:
```html
<li class="bg-white rounded-md border border-stone-200 overflow-hidden flex flex-col transition-all duration-150 hover:border-stone-300 relative group">
```

**Icon buttons** (action buttons in cards):
```html
<button class="p-1 text-stone-400 hover:text-stone-600 hover:bg-stone-100 rounded transition-colors">
```

**Form inputs**:
```html
<input class="block w-full border border-stone-200 rounded-md text-sm bg-white placeholder-stone-400 focus:outline-none focus:border-stone-400 focus:ring-1 focus:ring-stone-400/20 transition-colors">
```

### Icons

- Use inline SVG with `stroke="currentColor"` and `stroke-width="1.5"`
- Standard size: `w-4 h-4` with `opacity-60` when paired with text
- Small: `w-3 h-3` for action buttons

### Accessibility

- Use semantic HTML (`<header>`, `<main>`, `<nav>`, `<section>`)
- Include `aria-label`, `aria-pressed`, `role` attributes
- Support keyboard navigation (focus states, tab order)
- Screen reader text with `sr-only` class

### Animation

- Use `transition-all duration-150` or `duration-200` for interactions
- `cubic-bezier(0.4, 0, 0.2, 1)` timing (set globally in CSS)
- Hover effects: `hover:scale-[1.02]` for subtle zoom

## File Structure

```
mahpastes/
├── app.go              # Main Wails app logic
├── database.go         # SQLite operations
├── falai.go            # FAL AI integration
├── watcher.go          # Watch folder implementation
├── main.go             # Entry point
├── frontend/
│   ├── index.html      # Single HTML file with all markup
│   ├── js/
│   │   ├── app.js      # Main app initialization, event handlers
│   │   ├── ui.js       # Card rendering, gallery management
│   │   ├── modals.js   # All modal/lightbox/editor logic
│   │   ├── watch.js    # Watch folders UI
│   │   ├── settings.js # Settings modal
│   │   ├── wails-api.js # Wails bindings wrapper
│   │   └── utils.js    # Shared utilities
│   ├── css/
│   │   ├── main.css    # Global styles, scrollbars, form styling
│   │   └── modals.css  # Modal-specific styles
│   └── wailsjs/        # Generated Wails bindings
└── e2e/                # Playwright tests
    ├── tests/          # Test files by feature
    ├── fixtures/       # Test fixtures (AppHelper)
    └── helpers/        # Test utilities and selectors
```

## Code Style

### JavaScript

- Vanilla JS, no framework
- Module pattern with function scope
- DOM elements cached at top of file
- Event delegation where appropriate
- Use `const`/`let`, never `var`

### Go

- Standard Go formatting (gofmt)
- Wails app methods exposed via `wailsjs/`

### CSS

- Tailwind utility classes preferred
- Custom CSS only when utilities insufficient
- CSS custom properties for dynamic values (e.g., modals)

## Common Tasks

### Adding a new feature

1. Run e2e tests to verify baseline
2. Add Go backend method if needed
3. Update `frontend/wailsjs/` bindings (run `wails generate module`)
4. Add UI in appropriate JS file
5. Add CSS if needed (prefer Tailwind utilities)
6. Add e2e tests for the feature
7. Run all tests and fix any failures
