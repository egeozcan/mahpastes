#!/usr/bin/env bash
# Run the Wails CLI on Linux with the right WebKitGTK build tag.
#
# Usage: scripts/linux/wails.sh <build|dev|generate ...> [wails args...]
#
# The toolchain is checked where the compiler actually runs. When the host
# lacks Go or the GTK/WebKit headers (immutable distros such as Silverblue,
# Kinoite or Bazzite) and a distrobox/toolbox container named $DEV_CONTAINER
# exists, the command is re-run inside it. The repository is shared with the
# container through $HOME, so build output lands in the same build/bin.
set -euo pipefail

repo="$(cd "$(dirname "$0")/../.." && pwd)"
cd "$repo"
DEV_CONTAINER="${DEV_CONTAINER:-mahpastes-dev}"

missing=()
command -v go >/dev/null 2>&1 || missing+=(go)
command -v npm >/dev/null 2>&1 || missing+=(npm)
command -v pkg-config >/dev/null 2>&1 || missing+=(pkg-config)
command -v cc >/dev/null 2>&1 || command -v gcc >/dev/null 2>&1 || missing+=(gcc)
# Too-old distribution packages (Ubuntu 22.04 ships Go 1.18 and Node 12) fail
# halfway through the build with confusing errors, so check versions up front.
# Go 1.21+ fetches the exact toolchain go.mod asks for by itself.
if command -v go >/dev/null 2>&1; then
    go_minor="$(go env GOVERSION 2>/dev/null | sed -n 's/^go1\.\([0-9]*\).*/\1/p')"
    if [[ -n "$go_minor" ]] && ((go_minor < 21)); then
        missing+=("go>=1.21 (found $(go env GOVERSION); get it from https://go.dev/dl/)")
    fi
fi
if command -v node >/dev/null 2>&1; then
    node_major="$(node --version | sed -n 's/^v\([0-9]*\).*/\1/p')"
    if [[ -n "$node_major" ]] && ((node_major < 18)); then
        missing+=("node>=18 (found $(node --version); see https://nodejs.org/en/download)")
    fi
elif command -v npm >/dev/null 2>&1; then
    missing+=(node)
fi
if command -v pkg-config >/dev/null 2>&1; then
    pkg-config --exists gtk+-3.0 || missing+=(gtk3-dev)
    pkg-config --exists webkit2gtk-4.1 || pkg-config --exists webkit2gtk-4.0 || missing+=(webkit2gtk-dev)
fi

container_tool() {
    [[ -z "${MAHPASTES_IN_DEV_CONTAINER:-}" && ! -e /run/.containerenv && ! -e /.dockerenv ]] || return 1
    if command -v distrobox >/dev/null 2>&1 &&
        distrobox list --no-color 2>/dev/null | awk -F'|' 'NR>1 {gsub(/ /,"",$2); print $2}' | grep -qx "$DEV_CONTAINER"; then
        echo distrobox
    elif command -v toolbox >/dev/null 2>&1 &&
        toolbox list -c 2>/dev/null | awk 'NR>1 {print $2}' | grep -qx "$DEV_CONTAINER"; then
        echo toolbox
    else
        return 1
    fi
}

if ((${#missing[@]})); then
    if tool="$(container_tool)"; then
        echo "Host is missing ${missing[*]}; running in the '$DEV_CONTAINER' $tool container." >&2
        cmd=(env MAHPASTES_IN_DEV_CONTAINER=1 WAILS="${WAILS:-}" bash "$repo/scripts/linux/wails.sh" "$@")
        if [[ "$tool" == distrobox ]]; then
            exec distrobox enter "$DEV_CONTAINER" -- "${cmd[@]}"
        fi
        exec toolbox run -c "$DEV_CONTAINER" "${cmd[@]}"
    fi
    {
        echo "Missing build dependencies:"
        printf '  - %s\n' "${missing[@]}"
        echo
        if command -v apt-get >/dev/null 2>&1; then
            echo "  sudo apt install build-essential pkg-config libgtk-3-dev libwebkit2gtk-4.1-dev nodejs npm"
            echo "  Ubuntu 22.04's nodejs and golang-go are too old; use https://nodejs.org and https://go.dev/dl/."
        elif command -v rpm-ostree >/dev/null 2>&1; then
            echo "  This is an image-based system. Build inside a container instead:"
            echo "    distrobox create -n $DEV_CONTAINER -i registry.fedoraproject.org/fedora:latest"
            echo "    distrobox enter $DEV_CONTAINER -- sudo dnf install -y golang nodejs npm gcc pkgconf-pkg-config gtk3-devel webkit2gtk4.1-devel"
            echo "  then rerun make; it will build in '$DEV_CONTAINER' automatically."
        elif command -v dnf >/dev/null 2>&1; then
            echo "  sudo dnf install golang nodejs npm gcc pkgconf-pkg-config gtk3-devel webkit2gtk4.1-devel"
        elif command -v pacman >/dev/null 2>&1; then
            echo "  sudo pacman -S --needed go nodejs npm base-devel pkgconf gtk3 webkit2gtk-4.1"
        elif command -v zypper >/dev/null 2>&1; then
            echo "  sudo zypper install go nodejs npm gcc pkg-config gtk3-devel webkit2gtk3-devel"
        else
            echo "  Install Go, Node.js/npm, a C compiler, pkg-config, GTK 3 and WebKitGTK (4.1 or 4.0) development packages."
        fi
        echo
        echo "Distribution Go packages can be too old (go.mod needs Go $(awk '/^go /{print $2}' go.mod));"
        echo "Go 1.21+ downloads the required toolchain itself, otherwise use https://go.dev/dl/."
    } >&2
    exit 1
fi

wails="${WAILS:-}"
if [[ -z "$wails" || ! -x "$wails" ]]; then
    wails="$(command -v wails 2>/dev/null || true)"
fi
if [[ -z "$wails" ]]; then
    gobin="$(go env GOBIN)"
    wails="${gobin:-$(go env GOPATH)/bin}/wails"
fi
if [[ ! -x "$wails" ]]; then
    echo "Wails CLI not found. Install it with:" >&2
    echo "  go install github.com/wailsapp/wails/v2/cmd/wails@v2.12.0" >&2
    exit 1
fi

# Wails v2 links webkit2gtk-4.0 unless told otherwise. Ubuntu 24.04+, Debian 13
# and Fedora 40+ ship only 4.1; prefer it whenever it is available.
args=("$@")
if pkg-config --exists webkit2gtk-4.1; then
    case "${1:-} ${2:-}" in
    "generate module"*) args=(generate module -tags webkit2_41 "${@:3}") ;;
    build* | dev*) args=("$1" -tags webkit2_41 "${@:2}") ;;
    esac
fi
exec "$wails" "${args[@]}"
