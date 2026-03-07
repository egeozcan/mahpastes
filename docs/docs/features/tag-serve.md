---
sidebar_position: 14
---

# Tag Serve

Serve the clips in a tag as files over a local HTTP server.

## Overview

Tag serve starts a per-tag HTTP server that exposes every non-archived clip in that tag as a downloadable file. Each tag gets its own server on a random available port. Requests are served directly from the database -- no files are written to disk.

Use cases:

- Preview a collection of images or HTML files in a browser
- Share files with another device on the same network
- Feed a local tool or script with clip data over HTTP

## Starting a Server

1. Open the menu drawer and click **Serve**
2. Click **+ Serve a Tag**
3. Pick a tag from the dropdown
4. Click **Start** on the tag card

mahpastes allocates a random available port and starts the server immediately. The URL appears on the card and can be clicked to copy it.

![Tag serve view](/img/screenshots/serve-view.png)

## Stopping a Server

Click **Stop** on a running tag card. The port is released and the URL becomes inactive. The tag stays in the serve list so it can be restarted later.

To remove a tag from the serve list entirely, stop the server first, then click the X button.

## Bind Mode

Each tag server can run in one of two modes, toggled with the **Local / Network** button on the card:

| Mode | Bind address | Access |
|------|-------------|--------|
| **Local** | `127.0.0.1` | Only this machine |
| **Network** | `0.0.0.0` | Any device on the network |

The bind mode can only be changed while the server is stopped.

## Served Content

### Directory Listing

Browsing the root URL returns a directory listing of all non-archived clips in the tag, ordered by creation date. The listing includes filename, content type, and human-readable file size.

The response format depends on the `Accept` header:

| Accept header | Response |
|--------------|----------|
| `application/json` | JSON array of `{ name, size, content_type }` objects |
| anything else | Styled HTML page with a file table |

### File Access

Each file is accessible at `/<filename>`. The server sets the correct `Content-Type` and `Content-Length` headers. Clips without a filename are served as `clip-<id>`.

### index.html

If the tag contains a clip named `index.html`, requesting `/` or `/index.html` serves that clip directly instead of the directory listing. All other files remain accessible by name.

### Duplicate Filenames

When multiple clips share the same filename, duplicates get a numbered suffix: `photo.png`, `photo (2).png`, `photo (3).png`.

## Activity Indicators

- A **green dot** appears on running tag cards
- A **request counter** on each card shows total requests served
- The serve view polls status every 2 seconds while visible
- A dot appears on the hamburger menu icon when any tag server is running

## Related

- [Tags](tags.md) -- create and manage the tags used for serving
- [REST API](rest-api.md) -- programmatic access to clips with authentication
