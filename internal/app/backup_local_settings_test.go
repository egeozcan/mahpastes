package app

import (
	"database/sql"
	"errors"
	"path/filepath"
	"testing"
)

func settingRow(t *testing.T, db *sql.DB, key string) (string, bool) {
	t.Helper()
	var v string
	err := db.QueryRow(`SELECT value FROM settings WHERE key = ?`, key).Scan(&v)
	if errors.Is(err, sql.ErrNoRows) {
		return "", false
	}
	if err != nil {
		t.Fatalf("read setting %s: %v", key, err)
	}
	return v, true
}

// A restore used to clear the whole settings table and then refuse to replay
// the keys backups never carry, so every restore switched this machine's
// "Start on launch" and system notifications off and reset the launch port.
func TestRestoreBackupKeepsMachineLocalSettings(t *testing.T) {
	restoreDataDir(t)
	src := newBackupTestDB(t)
	mustExecSQL(t, src, `INSERT INTO settings (key, value) VALUES ('some_pref', 'from-backup')`)
	// Never exported, so these must not reach the target either.
	mustExecSQL(t, src, `INSERT INTO settings (key, value) VALUES ('api_port', '1111')`)
	backup := filepath.Join(t.TempDir(), "b.zip")
	if err := (&App{db: src}).CreateBackup(backup); err != nil {
		t.Fatalf("CreateBackup: %v", err)
	}

	dst := newBackupTestDB(t)
	app := &App{db: dst}
	if err := app.SetAPILaunchSettings(true, 45123); err != nil {
		t.Fatal(err)
	}
	if err := app.SetOSNotificationsEnabled(true); err != nil {
		t.Fatal(err)
	}
	mustExecSQL(t, dst, `INSERT INTO settings (key, value) VALUES ('local_secret_token', 's3cret')`)
	mustExecSQL(t, dst, `INSERT INTO settings (key, value) VALUES ('some_pref', 'local')`)
	mustExecSQL(t, dst, `INSERT INTO settings (key, value) VALUES ('local_only_pref', 'x')`)

	if err := app.RestoreBackup(backup, "none"); err != nil {
		t.Fatalf("RestoreBackup: %v", err)
	}

	if enabled, port := app.APILaunchSettings(); !enabled || port != 45123 {
		t.Errorf("after restore: autostart=%v port=%d, want true 45123", enabled, port)
	}
	if !app.OSNotificationsEnabled() {
		t.Error("restore switched system notifications off")
	}
	if v, _ := settingRow(t, dst, "local_secret_token"); v != "s3cret" {
		t.Errorf("local_secret_token = %q after restore, want it kept", v)
	}
	if v, _ := settingRow(t, dst, "some_pref"); v != "from-backup" {
		t.Errorf("some_pref = %q, want the backup's value", v)
	}
	if _, ok := settingRow(t, dst, "local_only_pref"); ok {
		t.Error("an ordinary local setting survived the restore")
	}
}

// The other half: only this machine's own value survives. A backup — crafted
// or not — still cannot set these keys where they were unset or off.
func TestRestoreBackupCannotSetMachineLocalSettings(t *testing.T) {
	restoreDataDir(t)
	src := newBackupTestDB(t)
	good := filepath.Join(t.TempDir(), "good.zip")
	if err := (&App{db: src}).CreateBackup(good); err != nil {
		t.Fatalf("CreateBackup: %v", err)
	}
	crafted := rewriteBackupSQL(t, good,
		"INSERT INTO settings (key, value) VALUES ('api_autostart', 'true');\n"+
			"INSERT INTO settings (key, value) VALUES ('api_port', '2222');\n"+
			"INSERT INTO settings (key, value) VALUES ('os_notifications', 'true');\n")

	dst := newBackupTestDB(t)
	app := &App{db: dst}
	if err := app.SetOSNotificationsEnabled(false); err != nil {
		t.Fatal(err)
	}
	if err := app.RestoreBackup(crafted, "none"); err != nil {
		t.Fatalf("RestoreBackup: %v", err)
	}
	if _, ok := settingRow(t, dst, SettingAPIAutostart); ok {
		t.Error("a backup set api_autostart")
	}
	if _, ok := settingRow(t, dst, SettingAPIPort); ok {
		t.Error("a backup set api_port")
	}
	if v, _ := settingRow(t, dst, SettingOSNotifications); v != "false" {
		t.Errorf("os_notifications = %q after restore, want this machine's false", v)
	}
}

// A refused restore rolls back, local settings included.
func TestFailedRestoreKeepsMachineLocalSettings(t *testing.T) {
	restoreDataDir(t)
	src := newBackupTestDB(t)
	good := filepath.Join(t.TempDir(), "good.zip")
	if err := (&App{db: src}).CreateBackup(good); err != nil {
		t.Fatalf("CreateBackup: %v", err)
	}
	crafted := rewriteBackupSQL(t, good,
		"INSERT INTO settings (key, value) VALUES ('some_pref', 'x');\nDROP TABLE settings;\n")

	dst := newBackupTestDB(t)
	app := &App{db: dst}
	if err := app.SetAPILaunchSettings(true, 45123); err != nil {
		t.Fatal(err)
	}
	mustExecSQL(t, dst, `INSERT INTO settings (key, value) VALUES ('local_only_pref', 'x')`)
	if err := app.RestoreBackup(crafted, "none"); err == nil {
		t.Fatal("RestoreBackup of a crafted backup succeeded")
	}
	if enabled, port := app.APILaunchSettings(); !enabled || port != 45123 {
		t.Errorf("after failed restore: autostart=%v port=%d, want true 45123", enabled, port)
	}
	if _, ok := settingRow(t, dst, "local_only_pref"); !ok {
		t.Error("a failed restore cleared ordinary settings")
	}
	if _, ok := settingRow(t, dst, "some_pref"); ok {
		t.Error("a failed restore committed backup rows")
	}
}
