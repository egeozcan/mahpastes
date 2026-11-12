#!/usr/bin/env bash
# Install (or remove) the Linux desktop app for the current user.
#
# Usage: install.sh [--uninstall]
#
# Works from both layouts it ships in:
#   - an extracted release tarball, where the app, mp, mahpastes.png and
#     plugins/ sit next to this script;
#   - a source checkout (scripts/linux/install.sh), after 'make build', where
#     they come from build/bin, build/appicon.png and plugins/.
#
# Everything goes under $PREFIX (default ~/.local), so no root access is needed:
#   $PREFIX/bin/mahpastes                      the app
#   $PREFIX/bin/mp                             the CLI, when one was built or
#                                              shipped, with shell completions
#                                              and man pages (install-mp.sh)
#   $PREFIX/share/applications/mahpastes.desktop
#   $PREFIX/share/icons/hicolor/<size>/apps/mahpastes.png
# Bundled plugins are copied into the app's data directory, which the app reads
# from ~/.config/mahpastes, or $XDG_CONFIG_HOME/mahpastes when XDG_CONFIG_HOME is
# absolute and no ~/.config/mahpastes library predates it (see resolve_data_dir),
# unless MAHPASTES_DATA_DIR overrides it.
# --uninstall removes the app, mp, the desktop entry and the icons, and keeps
# the data directory.
set -euo pipefail

APP_NAME=mahpastes
CLI_NAME=mp
script_dir="$(cd "$(dirname "$0")" && pwd)"
if [[ -f "$script_dir/$APP_NAME" ]]; then
    # Release tarball: everything is beside this script.
    cd "$script_dir"
    app_src="$APP_NAME"
    cli_src="$CLI_NAME"
    icon_src="$APP_NAME.png"
    mp_installer="install-mp.sh"
    build_hint="the release archive looks incomplete; extract it again."
else
    cd "$script_dir/../.."
    app_src="build/bin/$APP_NAME"
    cli_src="build/bin/$CLI_NAME"
    icon_src="build/appicon.png"
    mp_installer="scripts/install-mp.sh"
    build_hint="run 'make build' first."
fi
PREFIX="${PREFIX:-$HOME/.local}"
BINDIR="${BINDIR:-$PREFIX/bin}"
DATAROOT="${DATAROOT:-$PREFIX/share}"
# Same rule as the app (resolveLinuxDataDir in internal/app/database.go):
# an existing $XDG_CONFIG_HOME/mahpastes (XDG_CONFIG_HOME absolute), then an
# existing ~/.config/mahpastes, then $XDG_CONFIG_HOME/mahpastes for a fresh
# install, else ~/.config/mahpastes.
resolve_data_dir() {
    local legacy="$HOME/.config/$APP_NAME" xdg=""
    case "${XDG_CONFIG_HOME:-}" in
        /*) xdg="${XDG_CONFIG_HOME%/}/$APP_NAME" ;;
    esac
    if [ -z "$xdg" ] || [ "$xdg" = "$legacy" ]; then
        echo "$legacy"
    elif [ -e "$xdg" ]; then
        echo "$xdg"
    elif [ -e "$legacy" ]; then
        echo "$legacy"
    else
        echo "$xdg"
    fi
}
DATA_DIR="${MAHPASTES_DATA_DIR:-$(resolve_data_dir)}"
binary="$BINDIR/$APP_NAME"
cli="$BINDIR/$CLI_NAME"
desktop="$DATAROOT/applications/$APP_NAME.desktop"
icon_root="$DATAROOT/icons/hicolor"

# Match the process name exactly: a pattern match would also hit mahpastesd,
# this script, editors or anything else whose command line mentions the repo.
stop_app() {
    pkill -TERM -x "$APP_NAME" 2>/dev/null || return 0
    for _ in $(seq 50); do
        pgrep -x "$APP_NAME" >/dev/null 2>&1 || return 0
        sleep 0.1
    done
    pkill -KILL -x "$APP_NAME" 2>/dev/null || true
    sleep 0.2
}

refresh_desktop_caches() {
    if command -v update-desktop-database >/dev/null 2>&1; then
        update-desktop-database -q "$DATAROOT/applications" 2>/dev/null || true
    fi
    if command -v gtk-update-icon-cache >/dev/null 2>&1 && [[ -f "$icon_root/index.theme" ]]; then
        gtk-update-icon-cache -q -t "$icon_root" 2>/dev/null || true
    fi
}

if [[ "${1:-}" == --uninstall ]]; then
    stop_app
    rm -f "$binary" "$desktop"
    if [[ -f "$mp_installer" ]]; then
        PREFIX="$PREFIX" BINDIR="$BINDIR" DATAROOT="$DATAROOT" bash "$mp_installer" --uninstall >/dev/null
    else
        rm -f "$cli"
    fi
    rm -f "$icon_root"/*/apps/"$APP_NAME".png
    # Launch at login (Settings > Background & Startup), when it starts this copy.
    # The entry holds the resolved path (os.Executable), so also match it
    # through a symlinked home such as /home -> /var/home.
    autostart="${XDG_CONFIG_HOME:-$HOME/.config}/autostart/$APP_NAME.desktop"
    resolved="$(readlink -f "$(dirname "$binary")" 2>/dev/null || true)/$(basename "$binary")"
    if [[ -f "$autostart" ]] && grep -qF -e "\"$binary\"" -e "\"$resolved\"" "$autostart"; then
        rm -f "$autostart"
    fi
    refresh_desktop_caches
    echo "Removed $APP_NAME from $PREFIX (data in $DATA_DIR was kept)"
    exit 0
fi

if [[ ! -f "$app_src" ]]; then
    echo "$app_src not found; $build_hint" >&2
    exit 1
fi

# The app is single-instance per data directory: a launch while an old process
# still holds the lock hands its arguments to that process (raising its window)
# and exits. Stop the old process first so the launch at the end of this script
# starts the newly installed binary instead of focusing the stale one.
stop_app

# install(1) writes a new inode, so a still-mapped old binary is never modified.
# The mode is set explicitly: archive extractors and downloads do not always
# keep the executable bit.
install -Dm755 "$app_src" "$binary"
# The CLI is optional: install it, with completions and man pages generated by
# the binary itself, when one was built or shipped.
if [[ -f "$cli_src" ]]; then
    if [[ -f "$mp_installer" ]]; then
        PREFIX="$PREFIX" BINDIR="$BINDIR" DATAROOT="$DATAROOT" bash "$mp_installer" "$PWD/$cli_src"
    else
        install -Dm755 "$cli_src" "$cli"
    fi
fi

mkdir -p "$DATA_DIR/plugins"
cp plugins/*.lua "$DATA_DIR/plugins/"

# Icon themes look icons up by size directory. Scale the 1536px source when a
# resizer is available; otherwise install it as the largest standard size.
sizes=(512)
resize=()
if command -v magick >/dev/null 2>&1; then
    resize=(magick)
elif command -v convert >/dev/null 2>&1; then
    resize=(convert)
fi
if ((${#resize[@]})); then
    sizes=(512 256 128 64 48 32 16)
fi
for size in "${sizes[@]}"; do
    dir="$icon_root/${size}x${size}/apps"
    mkdir -p "$dir"
    if ((${#resize[@]})) && "${resize[@]}" "$icon_src" -resize "${size}x${size}" "$dir/$APP_NAME.png" 2>/dev/null; then
        continue
    fi
    install -m644 "$icon_src" "$dir/$APP_NAME.png"
done

# Exec uses an absolute path: desktop sessions (notably Ubuntu's) do not always
# have ~/.local/bin on PATH. StartupWMClass matches the GTK program name, which
# links the running window to this entry on X11 and Wayland.
#
# %U passes files the user opens with mahpastes ("Open With", a drop on the
# launcher) as file:// URLs and mahpastes:// links as they are; a launch while
# the app runs hands them to the open window. MimeType lists a few common
# types so mahpastes shows up under "Open With" for them, plus the mahpastes:
# URL scheme. Nothing is made a default handler for files (no xdg-mime
# default), and the app is the only handler for its own scheme.
#
# The desktop actions appear in the launcher's right-click menu, and KDE lists
# them under System Settings > Shortcuts so they can get a global shortcut.
mkdir -p "$(dirname "$desktop")"
cat >"$desktop" <<EOF
[Desktop Entry]
Type=Application
Name=mahpastes
GenericName=Clipboard Manager
Comment=Clipboard and paste manager
Exec="$binary" %U
Icon=$APP_NAME
Terminal=false
Categories=Utility;
Keywords=clipboard;paste;snippets;
MimeType=text/plain;text/markdown;image/png;image/jpeg;image/gif;image/webp;application/pdf;application/json;x-scheme-handler/mahpastes;
StartupNotify=true
StartupWMClass=$APP_NAME
SingleMainWindow=true
Actions=paste-clipboard;show;

[Desktop Action paste-clipboard]
Name=Save Clipboard as Clip
Exec="$binary" --paste-clipboard

[Desktop Action show]
Name=Show mahpastes
Exec="$binary"
EOF
chmod 644 "$desktop"
refresh_desktop_caches

echo "Installed to $binary"
if [[ -f "$cli_src" && ! -f "$mp_installer" ]]; then
    echo "Installed $CLI_NAME to $cli"
fi
echo "Updated bundled plugins in $DATA_DIR/plugins"
case ":$PATH:" in *:"$BINDIR":*) ;; *) echo "Add $BINDIR to PATH to run '$APP_NAME' from a terminal." ;; esac

if [[ -n "${WAYLAND_DISPLAY:-}${DISPLAY:-}" && -z "${NO_LAUNCH:-}" ]]; then
    setsid -f "$binary" >/dev/null 2>&1 </dev/null
fi
