---
sidebar_position: 1
---

# Installation

Get mahpastes running on your system in a few minutes.

## Download Pre-built Release

The easiest way to install mahpastes is to download a pre-built release from the
[Releases page](https://github.com/egeozcan/mahpastes/releases). Each release
contains:

| Asset | For |
|-------|-----|
| `mahpastes-macos-universal.zip` | macOS 13 or later, Intel and Apple Silicon |
| `mahpastes-windows-amd64.zip` | Windows on x64 |
| `mahpastes-windows-arm64.zip` | Windows on ARM |
| `mahpastes-linux-amd64.tar.gz` | Linux on x64 with GTK 3 and WebKitGTK 4.1 |
| `mp-<os>-<arch>`, `mahpastesd-<os>-<arch>` | Standalone CLI and headless server |
| `SHA256SUMS` | Checksums of every asset |

Every app archive also contains the `mp` CLI, the bundled plugins and examples.

### macOS

1. Download `mahpastes-macos-universal.zip` and open it
2. Drag `mahpastes.app` from the extracted folder to your Applications folder
3. Launch it like any other app

The app is signed and notarized by Apple, so it opens without Gatekeeper
warnings and includes [Finder integration](https://github.com/egeozcan/mahpastes/blob/master/docs/FILE_PROVIDER.md). To use
`mp`, copy it from the extracted folder to a directory on your `PATH`, for
example `~/.local/bin`.

### Windows

1. Download `mahpastes-windows-amd64.zip`, or `mahpastes-windows-arm64.zip` on an ARM PC
2. Extract it to a folder of your choice, for example `%LOCALAPPDATA%\Programs\mahpastes`
3. Run `mahpastes.exe`

The Windows build is not code-signed, so SmartScreen may warn on first launch;
choose **More info**, then **Run anyway**. mahpastes uses the Microsoft Edge
WebView2 runtime, which Windows 10 and 11 normally include; if it is missing,
the app offers to install it. `mp.exe` sits next to the app; add that folder to
your `PATH` to run `mp` from any terminal.

### Linux

The Linux build runs on distributions with GTK 3, WebKitGTK 4.1 and glibc 2.39
or newer: Ubuntu 24.04, Debian 13, Fedora 40 and later, and current Arch. On
image-based Fedora desktops (Silverblue, Kinoite, Bazzite), check with
`rpm -q webkit2gtk4.1` and layer the package with
`rpm-ostree install webkit2gtk4.1` if it is missing.

```bash
tar -xzf mahpastes-linux-amd64.tar.gz
cd mahpastes-linux-amd64
./install.sh
```

`install.sh` installs for the current user without root: the app and `mp` go
to `~/.local/bin`, a desktop entry and icons to `~/.local/share`, and the
bundled plugins to `~/.config/mahpastes/plugins`. Set `PREFIX` to install
somewhere else. `./install.sh --uninstall` removes the app, `mp`, the desktop
entry and the icons again and keeps your data.

### Standalone CLI and server

The `mp` CLI and the `mahpastesd` headless server are also published as single
files: `mp-linux-amd64`, `mp-linux-arm64`, `mp-windows-amd64.exe`,
`mp-windows-arm64.exe` and `mp-darwin-universal`, and the same names for
`mahpastesd`. Downloads lose their executable bit on macOS and Linux:

```bash
chmod +x mp-linux-amd64
mv mp-linux-amd64 ~/.local/bin/mp
```

### Verify a download

`SHA256SUMS` lists the checksum of every asset. In the folder holding your
downloads, check the line for the file you downloaded:

```bash
# Linux
grep mahpastes-linux-amd64.tar.gz SHA256SUMS | sha256sum --check
# macOS
grep mahpastes-macos-universal.zip SHA256SUMS | shasum -a 256 --check
```

On Windows, compare the output of `Get-FileHash mahpastes-windows-amd64.zip`
with the matching line.

## Build from Source

For the latest features or to contribute, build mahpastes from source.

### Prerequisites

Before building, install these dependencies:

| Dependency | Version | Purpose |
|------------|---------|---------|
| [Go](https://go.dev/dl/) | 1.25.7+ (fetches the 1.26.6 toolchain go.mod asks for) | Backend language |
| [Node.js](https://nodejs.org/) | 18+ | Frontend build tools |
| [Wails CLI](https://wails.io/) | 2.12.0 | Desktop app framework |

#### Install Wails CLI

```bash
go install github.com/wailsapp/wails/v2/cmd/wails@v2.12.0
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
make clean      # Remove build artifacts
make install    # Build, kill running app, install to /Applications, launch
make uninstall  # Remove installed app
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
xattr -cr /Applications/mahpastes.app
# Copy bundled plugins
mkdir -p ~/Library/Application\ Support/mahpastes/plugins
cp plugins/*.lua ~/Library/Application\ Support/mahpastes/plugins/
```

## Install the CLI (`mp`)

The `mp` CLI is a standalone binary that talks to mahpastes over the REST API. It requires no CGo and cross-compiles for macOS, Linux, and Windows.

### Build and Install

```bash
make mp            # Build for current platform
make mp-install    # Install to your user bin dir (or GOBIN if set)
make mp-cross      # Cross-compile for macOS, Linux and Windows (amd64 and arm64)
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

1. Open the menu drawer (press <span className="keyboard-key">M</span> or click the menu button) and click **API**
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
