# Mahpastes

Mahpastes stores and organizes reusable clipboard content as clips.

## Language

**Markdown clip**:
A clip whose filename ends in `.md` or `.markdown`, matched case-insensitively.
_Avoid_: Markdown paste, inferred Markdown

**Local Markdown reference**:
A relative Markdown link resolved from any exact tag of the source clip, or from the tag root when the source clip is untagged, to a clip on the same or a descendant tag path. Parent traversal is outside this relationship.
_Avoid_: Filesystem path, tag-tree file

**Local Markdown image**:
A Local Markdown reference whose target is a validated image clip.
_Avoid_: Filesystem image
