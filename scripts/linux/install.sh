#!/usr/bin/env bash
# Install (or remove) the Linux desktop app for the current user.
#
# Usage: scripts/linux/install.sh [--uninstall]
#
# Everything goes under $PREFIX (default ~/.local), so no root access is needed:
#   $PREFIX/bin/mahpastes                      the app
#   $PREFIX/share/applications/mahpastes.desktop
#   $PREFIX/share/icons/hicolor/<size>/apps/mahpastes.png
# Bundled plugins are copied into the app's data directory, which the app reads
# from ~/.config/mahpastes unless MAHPASTES_DATA_DIR overrides it.
set -euo pipefail

repo="$(cd "$(dirname "$0")/../.." && pwd)"
cd "$repo"
APP_NAME=mahpastes
PREFIX="${PREFIX:-$HOME/.local}"
BINDIR="${BINDIR:-$PREFIX/bin}"
DATAROOT="${DATAROOT:-$PREFIX/share}"
DATA_DIR="${MAHPASTES_DATA_DIR:-$HOME/.config/$APP_NAME}"
binary="$BINDIR/$APP_NAME"
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
    rm -f "$icon_root"/*/apps/"$APP_NAME".png
    refresh_desktop_caches
    echo "Removed $APP_NAME from $PREFIX (data in $DATA_DIR was kept)"
    exit 0
fi

if [[ ! -x "build/bin/$APP_NAME" ]]; then
    echo "build/bin/$APP_NAME not found; run 'make build' first." >&2
    exit 1
fi

# The app keeps a single-instance lock; a relaunch while the old process is
# still exiting would just focus the stale window.
stop_app

# install(1) writes a new inode, so a still-mapped old binary is never modified.
install -Dm755 "build/bin/$APP_NAME" "$binary"

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
    if ((${#resize[@]})) && "${resize[@]}" build/appicon.png -resize "${size}x${size}" "$dir/$APP_NAME.png" 2>/dev/null; then
        continue
    fi
    install -m644 build/appicon.png "$dir/$APP_NAME.png"
done

# Exec uses an absolute path: desktop sessions (notably Ubuntu's) do not always
# have ~/.local/bin on PATH. StartupWMClass matches the GTK program name, which
# links the running window to this entry on X11 and Wayland.
mkdir -p "$(dirname "$desktop")"
cat >"$desktop" <<EOF
[Desktop Entry]
Type=Application
Name=mahpastes
GenericName=Clipboard Manager
Comment=Clipboard and paste manager
Exec="$binary"
Icon=$APP_NAME
Terminal=false
Categories=Utility;
Keywords=clipboard;paste;snippets;
StartupNotify=true
StartupWMClass=$APP_NAME
EOF
chmod 644 "$desktop"
refresh_desktop_caches

echo "Installed to $binary"
echo "Updated bundled plugins in $DATA_DIR/plugins"
case ":$PATH:" in *:"$BINDIR":*) ;; *) echo "Add $BINDIR to PATH to run '$APP_NAME' from a terminal." ;; esac

if [[ -n "${WAYLAND_DISPLAY:-}${DISPLAY:-}" && -z "${NO_LAUNCH:-}" ]]; then
    setsid -f "$binary" >/dev/null 2>&1 </dev/null
fi
