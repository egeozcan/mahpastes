//go:build linux && !bindings

package main

import (
	"context"
	"database/sql"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	_ "modernc.org/sqlite"

	"go-clipboard/internal/fpfuse"
)

func isMountpoint(t *testing.T, dir string) bool {
	t.Helper()
	b, err := os.ReadFile("/proc/self/mountinfo")
	if err != nil {
		t.Fatal(err)
	}
	real, err := filepath.EvalSymlinks(dir)
	if err != nil {
		return false
	}
	for _, line := range strings.Split(string(b), "\n") {
		if fields := strings.Fields(line); len(fields) > 4 && fields[4] == real {
			return true
		}
	}
	return false
}

func TestLinuxMountEnableDisableAndRestart(t *testing.T) {
	if err := fpfuse.Available(); err != nil {
		t.Skip(err)
	}
	dataDir := t.TempDir()
	mountDir := filepath.Join(t.TempDir(), "Mahpastes")
	t.Setenv("MAHPASTES_MOUNT_DIR", mountDir)
	db, err := sql.Open("sqlite", filepath.Join(dataDir, "clips.db")+"?_pragma=journal_mode%3Dwal&_pragma=busy_timeout%3D5000")
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	if _, err = db.Exec(`CREATE TABLE clips(id INTEGER PRIMARY KEY AUTOINCREMENT,data BLOB NOT NULL,filename TEXT,content_type TEXT NOT NULL,created_at TEXT DEFAULT CURRENT_TIMESTAMP,is_archived INTEGER DEFAULT 0,expires_at TEXT,content_hash TEXT DEFAULT '',metadata TEXT DEFAULT '{}'); CREATE TABLE settings(key TEXT PRIMARY KEY,value TEXT); CREATE TABLE tags(id INTEGER PRIMARY KEY,name TEXT); CREATE TABLE clip_tags(clip_id INTEGER,tag_id INTEGER,PRIMARY KEY(clip_id,tag_id));`); err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()

	s := &FileProviderService{}
	s.start(ctx, db, dataDir)
	if st := s.Status(); !st.Supported || st.Enabled || st.Running || st.Location != "file manager" || st.Path != mountDir {
		t.Fatalf("initial status = %+v", st)
	}
	st, err := s.Enable()
	if err != nil {
		t.Skipf("FUSE mount unavailable here: %v", err)
	}
	if !st.Enabled || !st.Running || !isMountpoint(t, mountDir) {
		t.Fatalf("enabled status = %+v, mounted = %v", st, isMountpoint(t, mountDir))
	}
	var saved providerEnrollment
	b, err := os.ReadFile(filepath.Join(dataDir, "file-provider.json"))
	if err == nil {
		err = json.Unmarshal(b, &saved)
	}
	if err != nil || !saved.Enabled || saved.MountPath != mountDir {
		t.Fatalf("saved enrollment = %+v, %v", saved, err)
	}
	if _, err = os.ReadDir(filepath.Join(mountDir, "Active")); err != nil {
		t.Fatal(err)
	}

	// Quit unmounts; the next launch remounts from the saved enrollment.
	s.stop()
	if isMountpoint(t, mountDir) {
		t.Fatal("still mounted after stop")
	}
	restarted := &FileProviderService{}
	restarted.start(ctx, db, dataDir)
	if st := restarted.Status(); !st.Running || !isMountpoint(t, mountDir) {
		t.Fatalf("restart status = %+v", st)
	}

	// An unmount outside the app is reported, and Retry (Enable) remounts.
	if out, err := exec.Command("fusermount3", "-u", mountDir).CombinedOutput(); err != nil {
		t.Fatalf("external unmount: %v: %s", err, out)
	}
	deadline := time.Now().Add(5 * time.Second)
	for restarted.Status().Running && time.Now().Before(deadline) {
		time.Sleep(50 * time.Millisecond)
	}
	if st := restarted.Status(); st.Running || !st.Enabled || st.Message != providerFolderUnmounted {
		t.Fatalf("status after external unmount = %+v", st)
	}
	if st, err := restarted.Enable(); err != nil || !st.Running || !isMountpoint(t, mountDir) {
		t.Fatalf("retry = %+v, %v", st, err)
	}

	st, err = restarted.Disable()
	if err != nil {
		t.Fatal(err)
	}
	if st.Enabled || st.Running || isMountpoint(t, mountDir) {
		t.Fatalf("disabled status = %+v", st)
	}
	if _, err = os.Stat(mountDir); !os.IsNotExist(err) {
		t.Fatalf("mount directory left behind: %v", err)
	}
	restarted.stop()
}
