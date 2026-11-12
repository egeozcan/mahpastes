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
| `mahpastes-windows-amd64-installer.exe` | Windows on x64, installer |
| `mahpastes-windows-arm64-installer.exe` | Windows on ARM, installer |
| `mahpastes-windows-amd64.zip`, `mahpastes-windows-arm64.zip` | Windows, portable archive |
| `mahpastes.json`, `mahpastes.rb` | Scoop manifest and Homebrew cask matching the release |
| `mahpastes-linux-amd64.tar.gz` | Linux on x64 with GTK 3 and WebKitGTK 4.1 |
| `mp-<os>-<arch>`, `mahpastesd-<os>-<arch>` | Standalone CLI and headless server |
| `SHA256SUMS` | Checksums of every asset |

Every app archive also contains the `mp` CLI, the bundled plugins and examples.

### macOS

1. Download `mahpastes-macos-universal.zip` and open it
2. Drag `mahpastes.app` from the extracted folder to your Applications folder
3. Launch it like any other app

Release builds are signed with an Apple Developer ID and notarized by Apple,
so they open without a Gatekeeper override. The app includes
[Finder integration](https://github.com/egeozcan/mahpastes/blob/master/docs/FILE_PROVIDER.md).
An unsigned build, such as one you compiled yourself, is blocked on first
launch; see [Troubleshooting](../reference/troubleshooting.md#macos-app-is-damaged-or-security-warning)
for how to allow it.

To use `mp`, copy it from the extracted folder to a directory on your `PATH`,
for example `~/.local/bin`.

### Windows

**Installer.** Download `mahpastes-windows-amd64-installer.exe` (or the `arm64`
one on an ARM PC) and run it. It installs for all users (it asks for
administrator rights), adds Start Menu and desktop shortcuts and an entry in
Settings, Apps, and installs `mp.exe` next to the app. Uninstalling removes the
program but keeps your clips and settings in `%APPDATA%\mahpastes`.

**Portable.** Download `mahpastes-windows-amd64.zip` (or `arm64`), extract it to
a folder of your choice and run `mahpastes.exe`.

**Scoop.** The release carries a manifest that matches its zips exactly. It puts
`mp` on your `PATH` and adds a Start Menu shortcut:

```powershell
scoop install https://github.com/egeozcan/mahpastes/releases/latest/download/mahpastes.json
```

The Windows builds are not code-signed, so SmartScreen may warn on first
launch; choose **More info**, then **Run anyway**. mahpastes uses the Microsoft
Edge WebView2 runtime, which Windows 10 and 11 normally include. The installer
installs it when it is missing; the portable zip does not, so install it from
Microsoft if the app will not start. With the installer or the zip, `mp.exe`
sits next to the app; add that folder to your `PATH` to run `mp` from any
terminal (Scoop does this for you).

### Homebrew

Each release also carries `mahpastes.rb`, a Homebrew cask for the macOS zip that
links `mp` onto your `PATH`. mahpastes does not run a tap; see
[`packaging/README.md`](https://github.com/egeozcan/mahpastes/blob/master/packaging/README.md)
for how to host it in your own.

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
somewhere else. If `~/.local/bin` is not on your `PATH`, the script says so;
add it in your shell profile. `./install.sh --uninstall` removes the app, `mp`,
the desktop entry and the icons again and keeps your data.

The app itself needs only the GTK 3 and WebKitGTK 4.1 libraries, which desktop
systems normally ship. To browse clips in your file manager (**File manager
access** in Settings), also install the `fuse3` package (`sudo apt install fuse3`,
`sudo dnf install fuse3`, `sudo pacman -S fuse3`). See
[File manager access on Linux](https://github.com/egeozcan/mahpastes/blob/master/docs/FILE_PROVIDER.md#linux-file-manager-access).

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
make install    # Build, kill running app, install (/Applications; %LOCALAPPDATA%\Programs and a Start Menu shortcut on Windows), launch
make uninstall  # Remove installed app (keeps your data)
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
# Only needed for an unsigned local build that carries a download quarantine
xattr -cr /Applications/mahpastes.app
# Copy bundled plugins
mkdir -p ~/Library/Application\ Support/mahpastes/plugins
cp plugins/*.lua ~/Library/Application\ Support/mahpastes/plugins/
```

### Desktop Integration

What the installed app registers with the operating system:

- **macOS**: Finder **Open With > mahpastes** for any file (never as the default app), drops on the Dock
  icon, and [`mahpastes://` clip links](../features/deep-links.md). The bundle identifier is
  `io.github.egeozcan.mahpastes`; builds from before it was fixed used `com.wails.mahpastes`, so after
  upgrading from one of those macOS asks once more for folder and local network access.
- **Linux**: `make install` writes a desktop entry that lists mahpastes under **Open With** for text,
  Markdown, images, PDF and JSON, handles `mahpastes://` links, and adds a **Save Clipboard as Clip**
  action you can bind to a global shortcut ([details](../features/clipboard-management.md#save-the-clipboard-from-a-global-shortcut-linux)).
- **Windows**: dropping files on `mahpastes.exe` imports them. Add a **Send to** entry yourself as
  described in [Send to on Windows](../features/clipboard-management.md#send-to-on-windows); the zip
  release registers nothing, including `mahpastes://` links.

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

`make mp-install` (and `make install` on Linux) also installs shell completions and man pages under `~/.local/share` (or `$DATAROOT`, `$PREFIX/share`). `make mp-uninstall` removes them again. See [Shell Completion](#shell-completion) below.

### Shell Completion

`mp` can complete commands, flags, tag names (`--tag`, `mp tag delete <TAB>`), content types and API roles. Tag names are fetched from the running app; when the app is not running, or `MP_API_KEY` is not set, completion stays silent and returns immediately.

`make mp-install` already writes the scripts. To set them up by hand, or on Windows, generate them with `mp completion`:

```bash
# bash (needs bash-completion v2)
mp completion bash > ~/.local/share/bash-completion/completions/mp

# zsh: ~/.local/share/zsh/site-functions is not on zsh's default fpath
mp completion zsh > ~/.local/share/zsh/site-functions/_mp
# then add to ~/.zshrc, before compinit:
#   fpath=(~/.local/share/zsh/site-functions $fpath)

# fish
mp completion fish > ~/.config/fish/completions/mp.fish

# PowerShell: add to your profile ($PROFILE)
mp completion powershell | Out-String | Invoke-Expression
```

Start a new shell afterwards. Man pages (`man mp`, `man mp-clip-list`) are installed to `~/.local/share/man`; add it to `MANPATH` if your system does not search it. The complete command list is in the [CLI Reference](../reference/cli.md).

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

To read the key from a file instead, set `MP_API_KEY_FILE` to its path. The CLI connects to `http://localhost:44557` by default. Override with `MP_API_URL` if needed. Turn on **Start on launch** in the API modal so the server starts with the app.

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

## Version and Update Notices

**Settings → About** shows the installed version. At most once a day (and right away when you switch the check back on) mahpastes asks GitHub's public releases endpoint whether a newer release exists and, if so, shows a notice linking to the release page; once dismissed, the notice does not come back for that release. mahpastes never downloads or installs anything. The request is anonymous (no version, no identifiers). Switch it off with **Settings → About → Check for new releases**. Headless runs (`mahpastesd`, e2e) never check.

## Next Steps

- Learn the basics in [Quick Start](./quick-start)
- See [keyboard shortcuts](./keyboard-shortcuts)
- Set up [watch folders](../features/watch-folders) for automatic imports
