package plugin

import "testing"

func TestManifestsHavePermissionChanges(t *testing.T) {
	base := &Manifest{
		Network:    map[string][]string{"api.example.com": {"GET"}},
		Filesystem: FilesystemPerms{Read: true, Write: false},
		Clipboard:  false,
		Events:     []string{"clip:created"},
	}

	same := &Manifest{
		Network:    map[string][]string{"api.example.com": {"GET"}},
		Filesystem: FilesystemPerms{Read: true, Write: false},
		Clipboard:  false,
		Events:     []string{"clip:created"},
	}
	if ManifestsHavePermissionChanges(base, same) {
		t.Error("identical manifests should have no permission changes")
	}

	newDomain := &Manifest{
		Network:    map[string][]string{"api.example.com": {"GET"}, "evil.com": {"POST"}},
		Filesystem: FilesystemPerms{Read: true, Write: false},
		Clipboard:  false,
		Events:     []string{"clip:created"},
	}
	if !ManifestsHavePermissionChanges(base, newDomain) {
		t.Error("added network domain should be a permission change")
	}

	newMethod := &Manifest{
		Network:    map[string][]string{"api.example.com": {"GET", "POST"}},
		Filesystem: FilesystemPerms{Read: true, Write: false},
		Clipboard:  false,
		Events:     []string{"clip:created"},
	}
	if !ManifestsHavePermissionChanges(base, newMethod) {
		t.Error("added HTTP method should be a permission change")
	}

	newFS := &Manifest{
		Network:    map[string][]string{"api.example.com": {"GET"}},
		Filesystem: FilesystemPerms{Read: true, Write: true},
		Clipboard:  false,
		Events:     []string{"clip:created"},
	}
	if !ManifestsHavePermissionChanges(base, newFS) {
		t.Error("added filesystem write should be a permission change")
	}

	newClip := &Manifest{
		Network:    map[string][]string{"api.example.com": {"GET"}},
		Filesystem: FilesystemPerms{Read: true, Write: false},
		Clipboard:  true,
		Events:     []string{"clip:created"},
	}
	if !ManifestsHavePermissionChanges(base, newClip) {
		t.Error("added clipboard should be a permission change")
	}

	newEvent := &Manifest{
		Network:    map[string][]string{"api.example.com": {"GET"}},
		Filesystem: FilesystemPerms{Read: true, Write: false},
		Clipboard:  false,
		Events:     []string{"clip:created", "clip:deleted"},
	}
	if !ManifestsHavePermissionChanges(base, newEvent) {
		t.Error("added event should be a permission change")
	}

	removedDomain := &Manifest{
		Network:    map[string][]string{},
		Filesystem: FilesystemPerms{Read: true, Write: false},
		Clipboard:  false,
		Events:     []string{"clip:created"},
	}
	if !ManifestsHavePermissionChanges(base, removedDomain) {
		t.Error("removed network domain should be a permission change")
	}
}

func TestManifestsHavePermissionChanges_URLSettingMethodChange(t *testing.T) {
	base := &Manifest{
		Settings: []SettingField{
			{Key: "server_url", Type: "url", GrantsNetwork: []string{"GET"}},
		},
	}

	// Widening GET to GET, POST is a permission change and must go through
	// the review modal.
	widened := &Manifest{
		Settings: []SettingField{
			{Key: "server_url", Type: "url", GrantsNetwork: []string{"GET", "POST"}},
		},
	}
	if !ManifestsHavePermissionChanges(base, widened) {
		t.Fatal("a widened url-setting method set must count as a permission change")
	}

	// Order of the method list does not matter.
	reordered := &Manifest{
		Settings: []SettingField{
			{Key: "server_url", Type: "url", GrantsNetwork: []string{"POST", "GET"}},
		},
	}
	if ManifestsHavePermissionChanges(widened, reordered) {
		t.Fatal("method order must not count as a change")
	}

	// An added url setting is a change; an unrelated text setting is not.
	added := &Manifest{
		Settings: []SettingField{
			{Key: "server_url", Type: "url", GrantsNetwork: []string{"GET", "POST"}},
			{Key: "mirror_url", Type: "url", GrantsNetwork: []string{"GET"}},
		},
	}
	if !ManifestsHavePermissionChanges(widened, added) {
		t.Fatal("an added url setting must count as a permission change")
	}

	nonGrantChange := &Manifest{
		Settings: []SettingField{
			{Key: "server_url", Type: "url", Label: "Renamed Server URL", GrantsNetwork: []string{"GET", "POST"}},
		},
	}
	if ManifestsHavePermissionChanges(widened, nonGrantChange) {
		t.Fatal("a label change must not count as a permission change")
	}

	// Non-url settings never trigger the diff. Both manifests declare no
	// url settings, so only the text setting differs.
	textOnlyA := &Manifest{
		Settings: []SettingField{
			{Key: "note", Type: "text", Label: "Note"},
		},
	}
	textOnlyB := &Manifest{
		Settings: []SettingField{
			{Key: "note", Type: "text", Label: "Renamed Note"},
		},
	}
	if ManifestsHavePermissionChanges(textOnlyA, textOnlyB) {
		t.Fatal("non-url settings must not count as permission changes")
	}
}

func TestManifestsHavePermissionChanges_DuplicateURLKeys(t *testing.T) {
	// The policy unions methods across EVERY url setting; a diff that
	// collapsed duplicate keys into a map could miss a widening on the
	// overwritten entry. Duplicates must compare as a list.
	base := &Manifest{
		Settings: []SettingField{
			{Key: "server_url", Type: "url", GrantsNetwork: []string{"GET"}},
			{Key: "server_url", Type: "url", GrantsNetwork: []string{"GET"}},
		},
	}
	widened := &Manifest{
		Settings: []SettingField{
			{Key: "server_url", Type: "url", GrantsNetwork: []string{"GET"}},
			{Key: "server_url", Type: "url", GrantsNetwork: []string{"GET", "DELETE"}},
		},
	}
	if !ManifestsHavePermissionChanges(base, widened) {
		t.Fatal("a widened method set on a duplicate url key must count as a permission change")
	}
	if ManifestsHavePermissionChanges(base, base) {
		t.Fatal("identical duplicate declarations must not count as a change")
	}
}
