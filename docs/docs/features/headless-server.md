---
sidebar_position: 16
---

# Headless Server (mahpastesd)

`mahpastesd` is the mahpastes server without the desktop window. It runs the same clip store, REST API, plugins, watch folders and tag servers, and serves the web UI over HTTP. Use it on a home server, a VPS or a container, and talk to it with a browser, the [`mp` CLI](../tutorials/cli-workflow.md) or the [REST API](./rest-api.md).

It does **not** touch the system clipboard: copy-to-clipboard, clipboard watching and other desktop-only features are not available.

## Download, build and install

Release builds of `mahpastesd` for Linux, Windows and macOS are published next to the desktop app; see [Standalone CLI and server](../getting-started/installation.md#standalone-cli-and-server).

To build it from a checkout:

```bash
make mahpastesd            # build/bin/mahpastesd
make mahpastesd-install    # Linux only: also installs a systemd user unit (see below)
```

Or build it directly with `CGO_ENABLED=0 go build -o mahpastesd ./cmd/mahpastesd`, which gives a static binary on Linux and Windows. On macOS the server needs a native cgo build (`make mahpastesd` on a Mac).

## Configuration

`mahpastesd` takes **no command-line flags**. It is configured only through environment variables:

| Variable | Default | Meaning |
| --- | --- | --- |
| `MAHPASTESD_PORT` | `44557` | TCP port to listen on. Must be an integer. |
| `MAHPASTESD_BIND_ALL` | unset | Set to exactly `1` to listen on `0.0.0.0`. Any other value (including `true`) keeps it on `127.0.0.1` only. |
| `MAHPASTESD_TRUST_PROXY` | unset | Set to exactly `1` behind a TLS-terminating reverse proxy: session cookies get the `Secure` flag (from `X-Forwarded-Proto`), and the login rate limiter uses the real client IP (from `X-Forwarded-For`). Only set it when a proxy really is in front; see [Security notes](#security-notes). |
| `MAHPASTESD_SECURE_COOKIES` | unset | `1` or `true` always marks session cookies `Secure`, for a TLS proxy that does not send `X-Forwarded-Proto`. Browsers then only send the cookie over HTTPS. |
| `MAHPASTESD_BOOTSTRAP_KEY` | enabled | Set to `0` or `false` to skip creating the first admin key. |
| `MAHPASTES_DATA_DIR` | see below | Directory for the database, keys and files. Created if missing. |

Without `MAHPASTES_DATA_DIR` the server uses the desktop app's data directory: `~/.config/mahpastes` on Linux, `~/Library/Application Support/mahpastes` on macOS and `%APPDATA%\mahpastes` on Windows.

Stop it with `SIGINT` or `SIGTERM` (Ctrl+C, `systemctl stop`); it shuts down cleanly.

:::warning One server per data directory and port
`mahpastesd` and the desktop app take the same per-data-directory lock. If the desktop app is running against its default data directory, `mahpastesd` refuses to start there. Give the server its own `MAHPASTES_DATA_DIR` (the systemd unit and container image already do) or quit the desktop app first.

Port `44557` is also the default port of the desktop app's [REST API](./rest-api.md). If you run both on one machine with the desktop API turned on, set `MAHPASTESD_PORT` to another port and point `mp` at it with `MP_API_URL`.
:::

## First start and the bootstrap key

A fresh data directory has no API keys, so you could not log in. On first start `mahpastesd` creates an admin key named `bootstrap` and writes it to `bootstrap_admin_key` in the data directory (mode `0600`). The log prints only the file path, never the key:

```
api: bootstrap admin key written to /home/me/.local/share/mahpastesd/bootstrap_admin_key (mode 0600)
api: read it once, then delete the file and rotate the key.
```

If the file cannot be written, the key is printed to the log once instead.

The key lifecycle is: read it once, create a real key, revoke the bootstrap key, delete the file.

```bash
export MP_API_URL=http://127.0.0.1:44557
export MP_API_KEY=$(cat "$MAHPASTES_DATA_DIR/bootstrap_admin_key")

mp api key create ops --role admin     # prints the new key once
export MP_API_KEY=mp_...               # switch to the new key
mp api key list                        # find the bootstrap key's ID
mp api key revoke <id>
rm "$MAHPASTES_DATA_DIR/bootstrap_admin_key"
```

The bootstrap key is only created while the server has no keys at all (revoked keys count), so it does not come back on restart. Open `http://127.0.0.1:44557/` in a browser and sign in with a key to use the web UI.

## Running as a systemd user service (Linux)

`make mahpastesd-install` copies the binary to `~/.local/bin/mahpastesd` and the unit from `scripts/linux/mahpastesd.service` to `~/.config/systemd/user/`. It does not enable anything. With a downloaded binary, do the same by hand:

```bash
install -Dm755 mahpastesd-linux-amd64 ~/.local/bin/mahpastesd
install -Dm644 scripts/linux/mahpastesd.service ~/.config/systemd/user/mahpastesd.service
```

Then:

```bash
systemctl --user daemon-reload
systemctl --user enable --now mahpastesd
journalctl --user -u mahpastesd     # shows the bootstrap key file path
loginctl enable-linger "$USER"      # optional: keep running when logged out
```

The unit stores data in `~/.local/share/mahpastesd` and listens on `127.0.0.1:44557`. To change its environment, run `systemctl --user edit mahpastesd` and add an override such as:

```ini
[Service]
Environment=MAHPASTESD_PORT=44600
```

Use an override instead of editing the installed unit: `make mahpastesd-install` overwrites it. If the server keeps failing to start (for example because the port is taken), systemd stops retrying after five attempts in a minute; `journalctl --user -u mahpastesd` shows why.

## Running in a container

The repository ships a `Containerfile` (a static binary on a distroless image, running as a non-root user):

```bash
podman build -t mahpastesd -f Containerfile .
podman run -d --name mahpastesd -p 127.0.0.1:44557:44557 -v mahpastesd-data:/data mahpastesd
podman logs mahpastesd
podman cp mahpastesd:/data/bootstrap_admin_key ./bootstrap_admin_key
```

`docker` works the same way. Use a named volume as shown: the server runs as UID `65532`, and a fresh named volume takes the image's `/data` ownership. A host directory mounted with `-v ./data:/data` must be writable by that UID (with rootless podman, add `:U` to the mount).

The image sets `MAHPASTESD_BIND_ALL=1` so the published port is reachable, and the server logs a plaintext warning because of it. The distroless image has no shell, so copy the bootstrap key out with `podman cp` (or `docker cp`) as above, or start with `-e MAHPASTESD_BOOTSTRAP_KEY=0` and create keys another way. Once you have revoked the bootstrap key, the file left in the volume is useless; to remove it anyway, run `podman unshare rm "$(podman volume inspect -f '{{.Mountpoint}}' mahpastesd-data)/bootstrap_admin_key"` (rootless podman) or delete it from the volume's mountpoint as root.

Only port `44557` is published, so tag servers and incoming peer-to-peer share connections are not reachable from outside the container unless you publish their ports too.

## Security notes

- **There is no TLS.** API keys and session cookies travel in plaintext. Keep the default loopback binding, or put a reverse proxy that terminates TLS in front and set `MAHPASTESD_TRUST_PROXY=1`.
- Set `MAHPASTESD_TRUST_PROXY=1` only when every request comes through that proxy. With it set, the server believes `X-Forwarded-For`, `X-Forwarded-Proto` and `X-Forwarded-Host`, so a client that reaches the server directly can forge them, for example to get around the login rate limit.
- When publishing a container port, bind it to loopback (`-p 127.0.0.1:...`) unless a TLS proxy is in front.
- The bootstrap key is an admin key that never expires. Rotate it as described above and delete the file.
- Peer-to-peer sharing runs in the server as it does in the desktop app, whatever `MAHPASTESD_BIND_ALL` says: it listens on random TCP and UDP ports on all interfaces, announces itself on the local network with mDNS, and connects to the public libp2p DHT bootstrap peers. It only serves what you explicitly share. Set `MAHPASTES_SHARE_DISABLE_WAN_BOOTSTRAP=1` to skip the public bootstrap peers; sharing then cannot use the public DHT to find peers outside the local network.
- Tag servers you start in network mode listen on `0.0.0.0` on their own port; see [Tag Serve](./tag-serve.md).
- With no desktop UI to ask, plugin filesystem permission requests for paths outside the data directory are denied automatically. Watch folders are not confined this way: an `admin` key can watch any folder the server's user can read.
- Prefer scoped, lower-role keys (`viewer`, `editor`, `--scoped-tag`) for scripts and integrations.
