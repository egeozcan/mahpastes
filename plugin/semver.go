package plugin

import (
	"strconv"
	"strings"
)

// CompareSemver compares two semver strings.
// Returns -1 if a < b, 0 if a == b, 1 if a > b.
// Handles missing parts (e.g., "1.0" treated as "1.0.0").
// Returns 0 for unparseable versions.
func CompareSemver(a, b string) int {
	ap := parseSemverParts(a)
	bp := parseSemverParts(b)
	if ap == nil || bp == nil {
		return 0
	}
	for i := 0; i < 3; i++ {
		if ap[i] < bp[i] {
			return -1
		}
		if ap[i] > bp[i] {
			return 1
		}
	}
	return 0
}

// IsNewerVersion returns true if remote is a newer version than current.
func IsNewerVersion(current, remote string) bool {
	return CompareSemver(current, remote) == -1
}

func parseSemverParts(v string) []int {
	v = strings.TrimPrefix(v, "v")
	parts := strings.SplitN(v, ".", 3)
	result := make([]int, 3)
	for i, p := range parts {
		if i >= 3 {
			break
		}
		p = strings.SplitN(p, "-", 2)[0]
		p = strings.SplitN(p, "+", 2)[0]
		n, err := strconv.Atoi(p)
		if err != nil {
			return nil
		}
		result[i] = n
	}
	return result
}
