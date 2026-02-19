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
