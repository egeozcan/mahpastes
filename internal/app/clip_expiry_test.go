package app

import (
	"testing"
	"time"
)

// nyTimezone forces the process local zone to a negative-offset zone for the
// duration of a test, exposing timezone-sensitive expiry bugs. Restores on cleanup.
func nyTimezone(t *testing.T) {
	t.Helper()
	loc, err := time.LoadLocation("America/New_York")
	if err != nil {
		t.Skip("America/New_York tzdata unavailable")
	}
	orig := time.Local
	time.Local = loc
	t.Cleanup(func() { time.Local = orig })
}

func TestClipExpiry_FutureSurvivesInNonUTC(t *testing.T) {
	_, app, clipID, cleanup := setupShareLinkTest(t)
	defer cleanup()
	nyTimezone(t)

	if err := app.SetExpiration(clipID, 60); err != nil {
		t.Fatalf("SetExpiration: %v", err)
	}
	// The cleanup reaper must not delete a clip expiring an hour from now.
	res, err := app.db.Exec("DELETE FROM clips WHERE expires_at IS NOT NULL AND expires_at <= CURRENT_TIMESTAMP")
	if err != nil {
		t.Fatalf("reaper: %v", err)
	}
	if n, _ := res.RowsAffected(); n != 0 {
		t.Fatalf("future-expiring clip wrongly reaped (%d rows)", n)
	}
	// And it must still be visible to the GetClips lifecycle predicate.
	var visible int
	app.db.QueryRow("SELECT COUNT(*) FROM clips WHERE id = ? AND (expires_at IS NULL OR expires_at > CURRENT_TIMESTAMP)", clipID).Scan(&visible)
	if visible != 1 {
		t.Fatalf("future-expiring clip not visible (%d)", visible)
	}
}

func TestClipExpiry_PastIsReaped(t *testing.T) {
	_, app, clipID, cleanup := setupShareLinkTest(t)
	defer cleanup()
	nyTimezone(t)

	past := time.Now().UTC().Add(-time.Hour).Format("2006-01-02 15:04:05")
	if _, err := app.db.Exec("UPDATE clips SET expires_at = ? WHERE id = ?", past, clipID); err != nil {
		t.Fatalf("set past: %v", err)
	}
	res, err := app.db.Exec("DELETE FROM clips WHERE expires_at IS NOT NULL AND expires_at <= CURRENT_TIMESTAMP")
	if err != nil {
		t.Fatalf("reaper: %v", err)
	}
	if n, _ := res.RowsAffected(); n != 1 {
		t.Fatalf("past-expiring clip not reaped (%d)", n)
	}
}

func TestClipExpiry_MigrationFixesGoStringRows(t *testing.T) {
	_, app, clipID, cleanup := setupShareLinkTest(t)
	defer cleanup()
	nyTimezone(t)

	// Simulate a row written by an older build: bind a raw local time.Time, which
	// the driver serializes as a Go String() value the comparison can't handle.
	future := time.Now().Add(time.Hour)
	if _, err := app.db.Exec("UPDATE clips SET expires_at = ? WHERE id = ?", future, clipID); err != nil {
		t.Fatalf("set go-string expiry: %v", err)
	}

	migrateClipExpiresAtToUTC(app.db)

	// After migration the value is canonical UTC text (no longer Go-string parseable)...
	var raw string
	app.db.QueryRow("SELECT CAST(expires_at AS TEXT) FROM clips WHERE id = ?", clipID).Scan(&raw)
	if _, ok := goTimeStringToUTC(raw); ok {
		t.Fatalf("expires_at still in Go-string format after migration: %q", raw)
	}
	// ...and the still-valid clip compares correctly (the original bug).
	var visible int
	app.db.QueryRow("SELECT COUNT(*) FROM clips WHERE id = ? AND expires_at > CURRENT_TIMESTAMP", clipID).Scan(&visible)
	if visible != 1 {
		t.Fatalf("migrated future clip not visible (%d), raw=%q", visible, raw)
	}

	// Migration is idempotent — a second run changes nothing.
	migrateClipExpiresAtToUTC(app.db)
	var raw2 string
	app.db.QueryRow("SELECT CAST(expires_at AS TEXT) FROM clips WHERE id = ?", clipID).Scan(&raw2)
	if raw2 != raw {
		t.Fatalf("migration not idempotent: %q -> %q", raw, raw2)
	}
}
