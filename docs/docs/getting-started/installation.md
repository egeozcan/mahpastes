---
sidebar_position: 1
---

# Installation

Get mahpastes running on your system in a few minutes.

## Download Pre-built Release

The easiest way to install mahpastes is to download a pre-built release.

1. Go to the [Releases page](https://github.com/egeozcan/mahpastes/releases)
2. Download the appropriate file for your platform:
   - **macOS**: `mahpastes-darwin-universal.dmg` (works on Intel and Apple Silicon)
   - **Windows**: `mahpastes-windows-amd64.exe`
   - **Linux**: `mahpastes-linux-amd64`

### macOS Installation

1. Open the downloaded `.dmg` file
2. Drag `mahpastes.app` to your Applications folder
3. On first launch, right-click the app and select "Open" to bypass Gatekeeper

:::note First Launch on macOS
Since mahpastes is not signed with an Apple Developer certificate, macOS may show a security warning. Right-click the app and select "Open" the first time to allow it to run.
:::

### Windows Installation

1. Run the downloaded `.exe` file
2. Follow the installation wizard
3. Launch mahpastes from the Start menu

### Linux Installation

1. Make the downloaded file executable:
   ```bash
   chmod +x mahpastes-linux-amd64
   ```
2. Move it to a location in your PATH:
   ```bash
   sudo mv mahpastes-linux-amd64 /usr/local/bin/mahpastes
   ```
3. Run `mahpastes` from your terminal or application launcher

## Build from Source

For the latest features or to contribute, build mahpastes from source.

### Prerequisites

Before building, install these dependencies:

| Dependency | Version | Purpose |
|------------|---------|---------|
| [Go](https://go.dev/dl/) | 1.21+ | Backend language |
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

### Platform-Specific Builds

Build for a specific platform:

```bash
# macOS Universal (Intel + Apple Silicon)
wails build -platform darwin/universal

# Windows 64-bit
wails build -platform windows/amd64

# Linux 64-bit
wails build -platform linux/amd64
```

### Install the Built App

#### macOS

```bash
cp -R build/bin/mahpastes.app /Applications/
```

#### Windows

Copy `build/bin/mahpastes.exe` to your preferred location.

#### Linux

```bash
sudo cp build/bin/mahpastes /usr/local/bin/
```

## Verify Installation

Launch mahpastes. You should see an empty gallery ready to receive your first clips.

Try pasting something:
1. Copy some text or an image to your clipboard
2. Focus the mahpastes window
3. Press <span className="keyboard-key">Cmd</span> + <span className="keyboard-key">V</span> (macOS) or <span className="keyboard-key">Ctrl</span> + <span className="keyboard-key">V</span> (Windows/Linux)

Your first clip should appear in the gallery.

## Next Steps

- Learn the basics in [Quick Start](/getting-started/quick-start)
- Explore [keyboard shortcuts](/getting-started/keyboard-shortcuts)
- Set up [watch folders](/features/watch-folders) for automatic imports
