package main

import (
	"fmt"
	"strings"
)

// validateSubtagPath validates a relative tag path and returns the full tag name.
// An empty relativeTag returns the servedTagName unchanged.
// Rejects "..", ".", empty segments, and "_api" segments.
func validateSubtagPath(servedTagName, relativeTag string) (string, error) {
	relativeTag = strings.TrimSpace(relativeTag)
	if relativeTag == "" {
		return servedTagName, nil
	}

	segments := strings.Split(relativeTag, "/")
	for _, seg := range segments {
		if seg == "" {
			return "", fmt.Errorf("invalid tag path: empty segment")
		}
		if seg == ".." || seg == "." {
			return "", fmt.Errorf("invalid tag path: %q not allowed", seg)
		}
		if seg == "_api" {
			return "", fmt.Errorf("invalid tag path: '_api' is a reserved segment")
		}
	}

	return servedTagName + "/" + relativeTag, nil
}
