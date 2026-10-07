package app

import (
	"database/sql"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"golang.org/x/sync/semaphore"
	_ "modernc.org/sqlite"
)

func newTempStoreTestDB(t *testing.T, dir string) *sql.DB {
	t.Helper()
	dbPath := filepath.Join(dir, "test.db")
	db, err := sql.Open("sqlite", dbPath)
	if err != nil {
		t.Fatalf("failed to open sqlite db: %v", err)
	}
	t.Cleanup(func() {
		_ = db.Close()
	})

	_, err = db.Exec(`
		CREATE TABLE clips (
			id INTEGER PRIMARY KEY,
			content_type TEXT NOT NULL,
			data BLOB NOT NULL,
			filename TEXT
		)
	`)
	if err != nil {
		t.Fatalf("failed to create clips table: %v", err)
	}

	return db
}

func TestParseClipIDFromTempFilename(t *testing.T) {
	tests := []struct {
		name     string
		filename string
		expectID int64
		expectOK bool
	}{
		{name: "id_with_original_name", filename: "42_document.txt", expectID: 42, expectOK: true},
		{name: "id_with_extension", filename: "7.png", expectID: 7, expectOK: true},
		{name: "id_only", filename: "19", expectID: 19, expectOK: true},
		{name: "invalid_prefix", filename: "clip_12.txt", expectID: 0, expectOK: false},
		{name: "invalid_separator", filename: "12x.txt", expectID: 0, expectOK: false},
		// Link snapshots are never handed to drag-out or playback.
		{name: "stream_snapshot", filename: ".stream-42-0123456789abcdef", expectID: 0, expectOK: false},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			id, ok := parseClipIDFromTempFilename(tc.filename)
			if ok != tc.expectOK {
				t.Fatalf("ok mismatch: got %v want %v", ok, tc.expectOK)
			}
			if id != tc.expectID {
				t.Fatalf("id mismatch: got %d want %d", id, tc.expectID)
			}
		})
	}
}

func TestPrepareClipFile_LeaseRefresh(t *testing.T) {
	tempDir := t.TempDir()
	db := newTempStoreTestDB(t, tempDir)

	_, err := db.Exec(`INSERT INTO clips (id, content_type, data, filename) VALUES (?, ?, ?, ?)`,
		1, "image/png", []byte("first"), "lease.png")
	if err != nil {
		t.Fatalf("failed to insert clip: %v", err)
	}

	store := NewTempClipStore(db, tempDir, 60*time.Minute, 10*time.Minute)
	first, err := store.PrepareClipFile(1)
	if err != nil {
		t.Fatalf("first prepare failed: %v", err)
	}

	firstInfo, err := os.Stat(first.AbsPath)
	if err != nil {
		t.Fatalf("failed to stat first prepared file: %v", err)
	}

	time.Sleep(20 * time.Millisecond)

	second, err := store.PrepareClipFile(1)
	if err != nil {
		t.Fatalf("second prepare failed: %v", err)
	}

	if first.AbsPath != second.AbsPath {
		t.Fatalf("expected same temp file path, got %q and %q", first.AbsPath, second.AbsPath)
	}

	secondInfo, err := os.Stat(second.AbsPath)
	if err != nil {
		t.Fatalf("failed to stat second prepared file: %v", err)
	}

	if secondInfo.ModTime().Before(firstInfo.ModTime()) {
		t.Fatalf("expected lease modtime to refresh, got first=%v second=%v", firstInfo.ModTime(), secondInfo.ModTime())
	}
}

func TestPrune_RemovesStaleAndOrphanFiles(t *testing.T) {
	tempDir := t.TempDir()
	db := newTempStoreTestDB(t, tempDir)

	_, err := db.Exec(`INSERT INTO clips (id, content_type, data, filename) VALUES (?, ?, ?, ?)`,
		1, "text/plain", []byte("keep"), "keep.txt")
	if err != nil {
		t.Fatalf("failed to insert clip: %v", err)
	}

	store := NewTempClipStore(db, tempDir, 60*time.Minute, 10*time.Minute)

	freshExisting := filepath.Join(tempDir, "1_keep.txt")
	staleExisting := filepath.Join(tempDir, "1_stale.txt")
	freshOrphan := filepath.Join(tempDir, "999_orphan.txt")
	staleUnknown := filepath.Join(tempDir, "random.tmp")

	for _, file := range []string{freshExisting, staleExisting, freshOrphan, staleUnknown} {
		if err := os.WriteFile(file, []byte("x"), 0644); err != nil {
			t.Fatalf("failed to create %s: %v", file, err)
		}
	}

	staleTime := time.Now().Add(-2 * time.Hour)
	for _, file := range []string{staleExisting, staleUnknown} {
		if err := os.Chtimes(file, staleTime, staleTime); err != nil {
			t.Fatalf("failed to set stale modtime for %s: %v", file, err)
		}
	}

	if err := store.Prune(true); err != nil {
		t.Fatalf("prune failed: %v", err)
	}

	if _, err := os.Stat(freshExisting); err != nil {
		t.Fatalf("expected fresh existing file to remain, stat error: %v", err)
	}
	if _, err := os.Stat(staleExisting); !os.IsNotExist(err) {
		t.Fatalf("expected stale existing file to be removed, got err=%v", err)
	}
	if _, err := os.Stat(freshOrphan); !os.IsNotExist(err) {
		t.Fatalf("expected orphan file to be removed, got err=%v", err)
	}
	if _, err := os.Stat(staleUnknown); !os.IsNotExist(err) {
		t.Fatalf("expected stale unknown file to be removed, got err=%v", err)
	}
}

func newConcurrencyTestStore(t *testing.T) (*TempClipStore, *sql.DB) {
	t.Helper()
	root := t.TempDir()
	db := newTempStoreTestDB(t, root)
	dir := filepath.Join(root, "temp")
	if err := os.MkdirAll(dir, 0755); err != nil {
		t.Fatal(err)
	}
	return NewTempClipStore(db, dir, time.Hour, time.Hour), db
}

func setTempPrepareAfterLoad(t *testing.T, fn func(clipID int64)) {
	t.Helper()
	tempPrepareAfterLoad = fn
	t.Cleanup(func() { tempPrepareAfterLoad = nil })
}

// Concurrent requests for one clip share one copy out of the database.
func TestPrepareClipFile_ConcurrentSameClipSharesOneCopy(t *testing.T) {
	store, db := newConcurrencyTestStore(t)
	if _, err := db.Exec(`INSERT INTO clips (id, content_type, data, filename) VALUES (1, 'video/mp4', ?, 'v.mp4')`, []byte("video bytes")); err != nil {
		t.Fatal(err)
	}

	var loads atomic.Int32
	release := make(chan struct{})
	setTempPrepareAfterLoad(t, func(int64) {
		loads.Add(1)
		<-release
	})

	const n = 8
	var wg sync.WaitGroup
	errs := make(chan error, n)
	paths := make(chan string, n)
	for i := 0; i < n; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			res, err := store.PrepareClipFile(1)
			if err != nil {
				errs <- err
				return
			}
			paths <- res.AbsPath
		}()
	}
	// Let every caller arrive while the first copy is held open.
	if !waitFor(t, 5*time.Second, func() bool { return loads.Load() == 1 }) {
		t.Fatal("first copy never started")
	}
	time.Sleep(20 * time.Millisecond)
	close(release)
	wg.Wait()
	close(errs)
	close(paths)
	for err := range errs {
		t.Fatal(err)
	}
	if got := loads.Load(); got != 1 {
		t.Fatalf("clip read %d times, want 1", got)
	}
	var first string
	for p := range paths {
		if first == "" {
			first = p
		} else if p != first {
			t.Fatalf("callers got different files: %s vs %s", first, p)
		}
	}
	if b, err := os.ReadFile(first); err != nil || string(b) != "video bytes" {
		t.Fatalf("prepared file = %q, %v", b, err)
	}
}

// Copies of different clips do not wait on one another, and a lookup of a
// file already prepared does not wait behind a copy in progress.
func TestPrepareClipFile_DifferentClipsCopyConcurrently(t *testing.T) {
	store, db := newConcurrencyTestStore(t)
	for id := 1; id <= 3; id++ {
		if _, err := db.Exec(`INSERT INTO clips (id, content_type, data, filename) VALUES (?, 'video/mp4', ?, ?)`,
			id, []byte(fmt.Sprintf("clip %d", id)), fmt.Sprintf("c%d.mp4", id)); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := store.PrepareClipFile(3); err != nil {
		t.Fatal(err)
	}

	bothLoaded := make(chan struct{})
	var loaded sync.WaitGroup
	loaded.Add(2)
	go func() { loaded.Wait(); close(bothLoaded) }()
	setTempPrepareAfterLoad(t, func(clipID int64) {
		if clipID == 3 {
			return
		}
		loaded.Done()
		// Holds clip 1's copy open until clip 2's copy has also read its
		// clip: impossible if copies were serialized.
		select {
		case <-bothLoaded:
		case <-time.After(5 * time.Second):
		}
	})

	done := make(chan error, 2)
	for _, id := range []int64{1, 2} {
		go func(id int64) {
			_, err := store.PrepareClipFile(id)
			done <- err
		}(id)
	}

	select {
	case <-bothLoaded:
	case <-time.After(3 * time.Second):
		t.Fatal("copies of different clips were serialized")
	}

	for i := 0; i < 2; i++ {
		if err := <-done; err != nil {
			t.Fatal(err)
		}
	}
}

func TestFindExistingClipFile_DoesNotWaitBehindACopy(t *testing.T) {
	store, db := newConcurrencyTestStore(t)
	for id := 1; id <= 2; id++ {
		if _, err := db.Exec(`INSERT INTO clips (id, content_type, data, filename) VALUES (?, 'video/mp4', ?, ?)`,
			id, []byte("x"), fmt.Sprintf("c%d.mp4", id)); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := store.PrepareClipFile(2); err != nil {
		t.Fatal(err)
	}

	inCopy := make(chan struct{})
	release := make(chan struct{})
	setTempPrepareAfterLoad(t, func(clipID int64) {
		if clipID == 1 {
			close(inCopy)
			<-release
		}
	})
	copyDone := make(chan error, 1)
	go func() {
		_, err := store.PrepareClipFile(1)
		copyDone <- err
	}()
	<-inCopy

	found := make(chan *tempPreparedFile, 1)
	go func() {
		res, err := store.FindExistingClipFile(2)
		if err != nil {
			t.Error(err)
		}
		found <- res
	}()
	select {
	case res := <-found:
		if res == nil || filepath.Base(res.AbsPath) != "2_c2.mp4" {
			t.Fatalf("lookup = %+v, want the prepared file of clip 2", res)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("FindExistingClipFile waited behind another clip's copy")
	}
	close(release)
	if err := <-copyDone; err != nil {
		t.Fatal(err)
	}
}

// A clip edited (its temp files dropped) while a copy of it was being made
// must not get the old bytes published: the copy is discarded and redone.
func TestPrepareClipFile_DropDuringCopyRereads(t *testing.T) {
	store, db := newConcurrencyTestStore(t)
	if _, err := db.Exec(`INSERT INTO clips (id, content_type, data, filename) VALUES (1, 'text/plain', 'old', 'n.txt')`); err != nil {
		t.Fatal(err)
	}

	var edited atomic.Bool
	setTempPrepareAfterLoad(t, func(int64) {
		if edited.CompareAndSwap(false, true) {
			// A writer of clips.data: update, then drop the clip's files.
			if _, err := db.Exec(`UPDATE clips SET data = 'new' WHERE id = 1`); err != nil {
				t.Error(err)
			}
			if err := store.DeleteForClipIDs([]int64{1}); err != nil {
				t.Error(err)
			}
		}
	})

	res, err := store.PrepareClipFile(1)
	if err != nil {
		t.Fatal(err)
	}
	b, err := os.ReadFile(res.AbsPath)
	if err != nil {
		t.Fatal(err)
	}
	if string(b) != "new" {
		t.Fatalf("prepared file holds %q, want the edited bytes", b)
	}

	// A clip deleted mid-copy is reported missing and leaves no file behind.
	edited.Store(false)
	setTempPrepareAfterLoad(t, func(int64) {
		if edited.CompareAndSwap(false, true) {
			if _, err := db.Exec(`DELETE FROM clips WHERE id = 1`); err != nil {
				t.Error(err)
			}
			if err := store.DeleteForClipIDs([]int64{1}); err != nil {
				t.Error(err)
			}
		}
	})
	if _, err := store.PrepareClipFile(1); !errors.Is(err, ErrClipNotFound) {
		t.Fatalf("prepare of a clip deleted mid-copy: err = %v, want ErrClipNotFound", err)
	}
	entries, _ := os.ReadDir(store.dir)
	for _, e := range entries {
		t.Errorf("file left behind: %s", e.Name())
	}
}

// The leased-file index stays consistent with the directory: drops, files
// removed behind the store's back, and DeleteAll.
func TestFindExistingClipFile_IndexTracksDisk(t *testing.T) {
	store, db := newConcurrencyTestStore(t)
	if _, err := db.Exec(`INSERT INTO clips (id, content_type, data, filename) VALUES (1, 'text/plain', 'a', 'a.txt'), (2, 'text/plain', 'b', 'b.txt')`); err != nil {
		t.Fatal(err)
	}
	// A file left by an earlier run is found without a prepare.
	if err := os.WriteFile(filepath.Join(store.dir, "2_b.txt"), []byte("b"), 0644); err != nil {
		t.Fatal(err)
	}
	if res, err := store.FindExistingClipFile(2); err != nil || res == nil {
		t.Fatalf("file from an earlier run: %+v, %v", res, err)
	}

	res, err := store.PrepareClipFile(1)
	if err != nil {
		t.Fatal(err)
	}
	if got, _ := store.FindExistingClipFile(1); got == nil || got.AbsPath != res.AbsPath {
		t.Fatalf("lookup after prepare = %+v", got)
	}

	if err := os.Remove(res.AbsPath); err != nil {
		t.Fatal(err)
	}
	if got, _ := store.FindExistingClipFile(1); got != nil {
		t.Fatalf("lookup of a file removed behind the store = %+v, want nil", got)
	}

	if _, err := store.PrepareClipFile(1); err != nil {
		t.Fatal(err)
	}
	if err := store.DeleteForClipIDs([]int64{1}); err != nil {
		t.Fatal(err)
	}
	if got, _ := store.FindExistingClipFile(1); got != nil {
		t.Fatalf("lookup after drop = %+v, want nil", got)
	}

	if err := store.DeleteAll(); err != nil {
		t.Fatal(err)
	}
	if got, _ := store.FindExistingClipFile(2); got != nil {
		t.Fatalf("lookup after DeleteAll = %+v, want nil", got)
	}
}

func setTempPrepareBudget(t *testing.T, budget, minWeight int64) {
	t.Helper()
	oldBudget, oldMin, oldSem := tempPrepareByteBudget, tempPrepareMinWeight, tempPrepareSem
	tempPrepareByteBudget, tempPrepareMinWeight = budget, minWeight
	tempPrepareSem = semaphore.NewWeighted(budget)
	t.Cleanup(func() {
		tempPrepareByteBudget, tempPrepareMinWeight, tempPrepareSem = oldBudget, oldMin, oldSem
	})
}

// Copies are weighed by size: two clips that together exceed the byte budget
// never sit in memory at once, while small ones still copy side by side.
func TestPrepareClipFile_LargeCopiesRunAlone(t *testing.T) {
	setTempPrepareBudget(t, 1000, 10)
	store, db := newConcurrencyTestStore(t)
	for id := 1; id <= 4; id++ {
		size := 800 // clips 1 and 2: over half the budget each
		if id > 2 {
			size = 100
		}
		if _, err := db.Exec(`INSERT INTO clips (id, content_type, data, filename) VALUES (?, 'video/mp4', ?, ?)`,
			id, make([]byte, size), fmt.Sprintf("c%d.mp4", id)); err != nil {
			t.Fatal(err)
		}
	}

	var inFlight, peak atomic.Int32
	setTempPrepareAfterLoad(t, func(int64) {
		n := inFlight.Add(1)
		for {
			p := peak.Load()
			if n <= p || peak.CompareAndSwap(p, n) {
				break
			}
		}
		time.Sleep(150 * time.Millisecond)
		inFlight.Add(-1)
	})
	run := func(ids ...int64) {
		var wg sync.WaitGroup
		for _, id := range ids {
			wg.Add(1)
			go func(id int64) {
				defer wg.Done()
				if _, err := store.PrepareClipFile(id); err != nil {
					t.Error(err)
				}
			}(id)
		}
		wg.Wait()
	}

	run(1, 2)
	if got := peak.Load(); got != 1 {
		t.Fatalf("two large clips held in memory at once (peak %d copies)", got)
	}
	peak.Store(0)
	run(3, 4)
	if got := peak.Load(); got != 2 {
		t.Fatalf("small clips did not copy side by side (peak %d copies)", got)
	}
}

// A prune that fails to remove one stale file must still index every leased
// file it had not reached yet.
func TestPrune_RemoveFailureKeepsIndexingLaterFiles(t *testing.T) {
	if runtime.GOOS == "windows" || os.Geteuid() == 0 {
		t.Skip("needs a read-only directory to make os.Remove fail")
	}
	store, db := newConcurrencyTestStore(t)
	if _, err := db.Exec(`INSERT INTO clips (id, content_type, data, filename) VALUES (1, 'text/plain', 'a', 'a.txt'), (2, 'text/plain', 'b', 'b.txt')`); err != nil {
		t.Fatal(err)
	}
	stale := filepath.Join(store.dir, "1_a.txt") // sorts first
	fresh := filepath.Join(store.dir, "2_b.txt")
	for _, f := range []string{stale, fresh} {
		if err := os.WriteFile(f, []byte("x"), 0644); err != nil {
			t.Fatal(err)
		}
	}
	old := time.Now().Add(-2 * time.Hour)
	if err := os.Chtimes(stale, old, old); err != nil {
		t.Fatal(err)
	}

	if err := os.Chmod(store.dir, 0555); err != nil {
		t.Fatal(err)
	}
	store.mu.Lock()
	err := store.pruneLocked(true)
	store.mu.Unlock()
	if err := os.Chmod(store.dir, 0755); err != nil {
		t.Fatal(err)
	}
	if err == nil {
		t.Fatal("prune of a read-only directory succeeded")
	}

	if got, err := store.FindExistingClipFile(2); err != nil || got == nil {
		t.Fatalf("leased file after the failed removal was dropped from the index: %+v, %v", got, err)
	}
}

// A clip that grows after PrepareClipFile sized it must not be read under the
// smaller reservation: the attempt re-sizes and reserves the new size.
func TestPrepareClipFile_GrowthAfterSizingReReserves(t *testing.T) {
	setTempPrepareBudget(t, 1000, 10)
	store, db := newConcurrencyTestStore(t)
	if _, err := db.Exec(`INSERT INTO clips (id, content_type, data, filename) VALUES (1, 'video/mp4', ?, 'v.mp4')`, make([]byte, 20)); err != nil {
		t.Fatal(err)
	}
	big := make([]byte, 800)
	for i := range big {
		big[i] = 'x'
	}

	var sizes []int64
	tempPrepareAfterSize = func(_, size int64) {
		sizes = append(sizes, size)
		if len(sizes) == 1 {
			if _, err := db.Exec(`UPDATE clips SET data = ? WHERE id = 1`, big); err != nil {
				t.Error(err)
			}
		}
	}
	t.Cleanup(func() { tempPrepareAfterSize = nil })
	heldEnough := false
	setTempPrepareAfterLoad(t, func(int64) {
		// The reservation in force while the blob is in memory must cover it.
		if tempPrepareSem.TryAcquire(1000 - 800 + 1) {
			tempPrepareSem.Release(1000 - 800 + 1)
		} else {
			heldEnough = true
		}
	})

	got, err := store.PrepareClipFile(1)
	if err != nil {
		t.Fatal(err)
	}
	if len(sizes) != 2 || sizes[0] != 20 || sizes[1] != 800 {
		t.Fatalf("sizes seen = %v, want [20 800]", sizes)
	}
	if !heldEnough {
		t.Fatal("800-byte clip was read under a reservation smaller than its size")
	}
	b, err := os.ReadFile(got.AbsPath)
	if err != nil || len(b) != 800 {
		t.Fatalf("published file = %d bytes, %v; want 800", len(b), err)
	}
}

// A clip deleted after sizing reports not found, not "kept changing".
func TestPrepareClipFile_DeletedAfterSizingIsNotFound(t *testing.T) {
	store, db := newConcurrencyTestStore(t)
	if _, err := db.Exec(`INSERT INTO clips (id, content_type, data, filename) VALUES (1, 'text/plain', 'a', 'a.txt')`); err != nil {
		t.Fatal(err)
	}
	tempPrepareAfterSize = func(int64, int64) {
		if _, err := db.Exec(`DELETE FROM clips WHERE id = 1`); err != nil {
			t.Error(err)
		}
	}
	t.Cleanup(func() { tempPrepareAfterSize = nil })
	if _, err := store.PrepareClipFile(1); !errors.Is(err, ErrClipNotFound) {
		t.Fatalf("err = %v, want ErrClipNotFound", err)
	}
}

// Multibyte TEXT data (backup restore accepts text literals) is weighed by
// its bytes, not its characters.
func TestPrepareClipFile_MultibyteTextWeighedInBytes(t *testing.T) {
	store, db := newConcurrencyTestStore(t)
	text := ""
	for i := 0; i < 100; i++ {
		text += "é世" // 2 chars, 5 UTF-8 bytes
	}
	if _, err := db.Exec(`INSERT INTO clips (id, content_type, data, filename) VALUES (1, 'text/plain', ?, 't.txt')`, text); err != nil {
		t.Fatal(err)
	}
	var typ string
	if err := db.QueryRow(`SELECT typeof(data) FROM clips WHERE id = 1`).Scan(&typ); err != nil || typ != "text" {
		t.Fatalf("data stored as %q (%v), want text", typ, err)
	}
	size, err := store.sizeClipForPrepare(1)
	if err != nil {
		t.Fatal(err)
	}
	if size != int64(len(text)) {
		t.Fatalf("size = %d, want %d bytes (LENGTH would give %d chars)", size, len(text), 200)
	}

	// Under a budget between the char and byte counts the read still runs
	// (as a whole-budget reservation) and publishes every byte.
	setTempPrepareBudget(t, 300, 10)
	got, err := store.PrepareClipFile(1)
	if err != nil {
		t.Fatal(err)
	}
	b, err := os.ReadFile(got.AbsPath)
	if err != nil || string(b) != text {
		t.Fatalf("published %d bytes, %v; want %d", len(b), err, len(text))
	}
}
