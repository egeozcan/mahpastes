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

## Install the CLI (`mp`)

The `mp` CLI is a standalone binary that talks to mahpastes over the REST API. It requires no CGo and cross-compiles for macOS, Linux, and Windows.

### Build and Install

```bash
make mp            # Build for current platform
make mp-install    # Install to your user bin dir (or GOBIN if set)
make mp-cross      # Cross-compile for all platforms
```

Default install locations:

- macOS/Linux: `~/.local/bin` unless `GOBIN` is set
- Windows: `GOBIN`, then `%GOPATH%\bin`, then `%USERPROFILE%\go\bin`

On macOS and Linux, if `~/.local/bin` is not already in your `PATH`, add it in your shell profile:

```bash
export PATH="$HOME/.local/bin:$PATH"
```

On Windows, make sure the resolved install directory is in your `PATH`.

If you want a different location, override the target directory:

```bash
make mp-install MP_INSTALL_DIR=/usr/local/bin
```

### Configure API Access

The CLI authenticates with an API key. Generate one in the desktop app:

1. Click the **API** button in the toolbar to open the API settings modal
2. Start the API server if it is not already running
3. Create a new API key

Then set the environment variable:

```bash
export MP_API_KEY=mp_your_key_here
```

:::tip
Add the export to your shell profile (`~/.zshrc`, `~/.bashrc`) so it persists across sessions.
:::

The CLI connects to `http://localhost:44557` by default. Override with `MP_API_URL` if needed.

Verify connectivity:

```bash
mp api status
```

## Verify Installation

Launch mahpastes. You should see an empty gallery ready to receive your first clips.

Try pasting something:
1. Copy some text or an image to your clipboard
2. Focus the mahpastes window
3. Press <span className="keyboard-key">Cmd</span> + <span className="keyboard-key">V</span>

Your first clip should appear in the gallery.

## Next Steps

- Learn the basics in [Quick Start](./quick-start)
- See [keyboard shortcuts](./keyboard-shortcuts)
- Set up [watch folders](../features/watch-folders) for automatic imports
