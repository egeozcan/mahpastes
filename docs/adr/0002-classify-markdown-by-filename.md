---
status: accepted
---

# Classify Markdown clips by filename

A clip is a Markdown clip only when its filename ends in `.md` or `.markdown`, matched case-insensitively. Importing, renaming, or migrating such a clip promotes its stored MIME type to `text/markdown`; removing the extension disables Markdown behavior without downgrading the MIME type, preserving transfer and API metadata. Existing matching clips are promoted when the database opens.

Filename is authoritative because MIME values vary between browser uploads, watched folders, plugins, operating systems, and API clients, while the extension reflects the user-visible file identity. Invalid UTF-8 files retain their original bytes but cannot be previewed or edited in-app.
