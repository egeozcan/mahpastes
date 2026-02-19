package plugin

import "sort"

// ManifestsHavePermissionChanges returns true if the two manifests differ
// in any permission-related field (network, filesystem, clipboard, events).
func ManifestsHavePermissionChanges(current, remote *Manifest) bool {
	if !networkEqual(current.Network, remote.Network) {
		return true
	}
	if current.Filesystem != remote.Filesystem {
		return true
	}
	if current.Clipboard != remote.Clipboard {
		return true
	}
	if !stringSliceEqual(current.Events, remote.Events) {
		return true
	}
	return false
}

func networkEqual(a, b map[string][]string) bool {
	if len(a) != len(b) {
		return false
	}
	for domain, methodsA := range a {
		methodsB, ok := b[domain]
		if !ok {
			return false
		}
		if !stringSliceEqual(methodsA, methodsB) {
			return false
		}
	}
	return true
}

func stringSliceEqual(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	aSorted := make([]string, len(a))
	bSorted := make([]string, len(b))
	copy(aSorted, a)
	copy(bSorted, b)
	sort.Strings(aSorted)
	sort.Strings(bSorted)
	for i := range aSorted {
		if aSorted[i] != bSorted[i] {
			return false
		}
	}
	return true
}
