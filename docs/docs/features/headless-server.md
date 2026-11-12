---
sidebar_position: 16
---

# Headless Server (mahpastesd)

`mahpastesd` is the mahpastes server without the desktop window. It runs the same clip store, REST API, plugins and tag servers, and serves the web UI over HTTP. Use it on a home server, a VPS or a container, and talk to it with a browser, the [`mp` CLI](../tutorials/cli-workflow.md) or the [REST API](./rest-api.md).

It does **not** touch the system clipboard: copy-to-clipboard, clipboard watching and other desktop-only features are not available.

## Build and install

```bash
make mahpastesd            # build/bin/mahpastesd
make mahpastesd-install    # Linux: also installs a systemd user unit (see below)
```

Or build it directly with `CGO_ENABLED=0 go build -o mahpastesd ./cmd/mahpastesd`. The binary is static on Linux and Windows. On macOS it needs a native (cgo) build.

## Configuration

`mahpastesd` takes **no command-line flags**. It is configured only through environment variables:

| Variable | Default | Meaning |
| --- | --- | --- |
| `MAHPASTESD_PORT` | `44557` | TCP port to listen on. Must be an integer. |
| `MAHPASTESD_BIND_ALL` | unset | Set to `1` to listen on `0.0.0.0`. Otherwise it listens on `127.0.0.1` only. |
| `MAHPASTESD_TRUST_PROXY` | unset | Set to `1` behind a TLS-terminating reverse proxy: session cookies get the `Secure` flag (from `X-Forwarded-Proto`) and the login rate limiter uses the real client IP (from `X-Forwarded-For`). |
| `MAHPASTESD_BOOTSTRAP_KEY` | enabled | Set to `0` or `false` to skip creating the first admin key. |
| `MAHPASTES_DATA_DIR` | OS default | Directory for the database, keys and files. Created if missing. |

Stop it with `SIGINT` or `SIGTERM` (Ctrl+C, `systemctl stop`); it shuts down cleanly.

:::warning One server per data directory
`mahpastesd` and the desktop app take the same per-data-directory lock. If the desktop app is running against the default data directory, `mahpastesd` refuses to start. Give the server its own `MAHPASTES_DATA_DIR` (the systemd unit and container image already do) or quit the desktop app first.
:::

## First start and the bootstrap key

A fresh data directory has no API keys, so you could not log in. On first start `mahpastesd` creates an admin key named `bootstrap` and writes it to `bootstrap_admin_key` in the data directory (mode `0600`). The log prints only the file path, never the key:

```
api: bootstrap admin key written to /var/lib/mahpastesd/bootstrap_admin_key (mode 0600)
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

The bootstrap key is only created when no keys exist, so it does not come back on restart. Open `http://127.0.0.1:44557/` in a browser to use the web UI with a key.

## Running as a systemd user service (Linux)

`make mahpastesd-install` copies the binary to `~/.local/bin/mahpastesd` and the unit from `scripts/linux/mahpastesd.service` to `~/.config/systemd/user/`. It does not enable anything. Then:

```bash
systemctl --user daemon-reload
systemctl --user enable --now mahpastesd
journalctl --user -u mahpastesd     # shows the bootstrap key file path
loginctl enable-linger "$USER"      # optional: keep running when logged out
```

The unit stores data in `~/.local/share/mahpastesd` and listens on `127.0.0.1:44557`. Edit it (or use `systemctl --user edit mahpastesd`) to change the environment.

## Running in a container

The repository ships a `Containerfile` (a static binary on a distroless image, running as a non-root user):

```bash
podman build -t mahpastesd -f Containerfile .
podman run -d --name mahpastesd -p 127.0.0.1:44557:44557 -v mahpastesd-data:/data mahpastesd
podman logs mahpastesd
podman volume inspect mahpastesd-data   # Mountpoint holds bootstrap_admin_key
```

The image sets `MAHPASTESD_BIND_ALL=1` so the published port is reachable, and the server logs a plaintext warning because of it. The distroless image has no shell or `cat`, so read the bootstrap key from the volume's mountpoint on the host, or start with `-e MAHPASTESD_BOOTSTRAP_KEY=0` and create keys another way. `docker` works the same way.

## Security notes

- **There is no TLS.** API keys and session cookies travel in plaintext. Keep the default loopback binding, or put a reverse proxy that terminates TLS in front and set `MAHPASTESD_TRUST_PROXY=1`.
- When publishing a container port, bind it to loopback (`-p 127.0.0.1:...`) unless a TLS proxy is in front.
- The bootstrap key is an admin key that never expires. Rotate it as described above and delete the file.
- With no desktop UI to ask, filesystem permission requests for paths outside the data directory are denied automatically.
- Prefer scoped, lower-role keys (`viewer`, `editor`, `--scoped-tag`) for scripts and integrations.
