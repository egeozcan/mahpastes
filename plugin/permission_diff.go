package plugin

import (
	"sort"
	"strings"
)

// ManifestsHavePermissionChanges returns true if the two manifests differ
// in any permission-related field (network, filesystem, clipboard, events,
// or the network methods a url setting grants).
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
	if !urlGrantSettingsEqual(current.Settings, remote.Settings) {
		return true
	}
	return false
}

// urlGrantSettingsEqual compares the network grants declared by url-typed
// settings: any change to a url setting's key set or method list is a
// permission change, so a manifest update that widens e.g. GET to GET, POST
// goes through the permission review modal.
//
// Compared as a sorted list of (key, sorted methods) entries rather than a
// map keyed by setting key: the policy unions methods across EVERY url
// setting, so two declarations sharing a key must not collapse — a change to
// the overwritten one could otherwise widen the effective policy unseen.
func urlGrantSettingsEqual(a, b []SettingField) bool {
	collect := func(settings []SettingField) [][2]string {
		var out [][2]string
		for _, s := range settings {
			if s.Type != "url" {
				continue
			}
			methods := append([]string(nil), s.GrantsNetwork...)
			sort.Strings(methods)
			out = append(out, [2]string{s.Key, strings.Join(methods, ",")})
		}
		sort.Slice(out, func(i, j int) bool {
			if out[i][0] != out[j][0] {
				return out[i][0] < out[j][0]
			}
			return out[i][1] < out[j][1]
		})
		return out
	}
	aList, bList := collect(a), collect(b)
	if len(aList) != len(bList) {
		return false
	}
	for i := range aList {
		if aList[i] != bList[i] {
			return false
		}
	}
	return true
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
