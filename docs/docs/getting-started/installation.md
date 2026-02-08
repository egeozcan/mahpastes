---
sidebar_position: 1
---

# Installation

Get mahpastes running on your system in a few minutes.

## Download Pre-built Release

The easiest way to install mahpastes is to download a pre-built release.

1. Go to the [Releases page](https://github.com/egeozcan/mahpastes/releases)
2. Download `mahpastes-darwin-universal.dmg` (works on Intel and Apple Silicon)
3. Open the downloaded `.dmg` file
4. Drag `mahpastes.app` to your Applications folder
5. On first launch, right-click the app and select "Open" to bypass Gatekeeper

:::note First Launch on macOS
Since mahpastes is not signed with an Apple Developer certificate, macOS may show a security warning. Right-click the app and select "Open" the first time to allow it to run.
:::

## Build from Source

For the latest features or to contribute, build mahpastes from source.

### Prerequisites

Before building, install these dependencies:

| Dependency | Version | Purpose |
|------------|---------|---------|
| [Go](https://go.dev/dl/) | 1.24+ | Backend language |
| [Node.js](https://nodejs.org/) | 18+ | Frontend build tools |
| [Wails CLI](https://wails.io/) | 2.x | Desktop app framework |

#### Install Wails CLI

```bash
go install github.com/wailsapp/wails/v2/cmd/wails@latest
```

Verify the installation:

```bash
wails doctor
```

This command checks your environment and reports any missing dependencies.

### Clone and Build

```bash
# Clone the repository
git clone https://github.com/egeozcan/mahpastes.git
cd mahpastes

# Install frontend dependencies
cd frontend && npm install && cd ..

# Build for your current platform
wails build

# The app will be in build/bin/
```

### Using the Makefile

The project includes a Makefile with convenient targets:

```bash
make dev        # Start dev server with hot reload
make build      # Clean production build
make install    # Build, kill running app, install to /Applications, launch
make bindings   # Regenerate frontend bindings after Go changes
make test       # Run e2e tests
make help       # Show all targets
```

### Install the Built App

```bash
# Using make (recommended - also updates bundled plugins)
make install

# Or manually
cp -R build/bin/mahpastes.app /Applications/
```

## Verify Installation

Launch mahpastes. You should see an empty gallery ready to receive your first clips.

Try pasting something:
1. Copy some text or an image to your clipboard
2. Focus the mahpastes window
3. Press <span className="keyboard-key">Cmd</span> + <span className="keyboard-key">V</span>

Your first clip should appear in the gallery.

## Next Steps

- Learn the basics in [Quick Start](/getting-started/quick-start)
- Explore [keyboard shortcuts](/getting-started/keyboard-shortcuts)
- Set up [watch folders](/features/watch-folders) for automatic imports
