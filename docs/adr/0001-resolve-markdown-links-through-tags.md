---
status: accepted
---

# Resolve local Markdown references through tags

Relative links in a Markdown clip resolve from every exact tag assigned to that clip, or from the tag root when the clip is untagged, and may address clips on the same or a descendant tag path. Resolution is case-sensitive, forbids parent (`..`) traversal, deduplicates matches by clip ID, opens a unique match automatically, and asks the user to choose among distinct matches. Archived clips remain valid targets.

This makes references deterministic regardless of the gallery, search, or folder from which a clip is opened. We rejected using the active folder because the same document would behave differently between views, and rejected choosing a primary tag because clips do not have one. Parent traversal was excluded to keep a reference inside each candidate tag subtree.
