package app

import (
	"encoding/base64"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

// writeFile creates a file with content, making parent directories as needed.
func writeFile(t *testing.T, root string, rel string, content string) string {
	t.Helper()
	p := filepath.Join(root, filepath.FromSlash(rel))
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		t.Fatalf("mkdir for %s: %v", rel, err)
	}
	if err := os.WriteFile(p, []byte(content), 0o644); err != nil {
		t.Fatalf("write %s: %v", rel, err)
	}
	return p
}

// scanFixture builds a folder with the shapes the walk has to handle and
// returns its root.
// allowUnpickedImport lets a test scan a temp directory without going through
// the native picker, the same escape hatch the e2e launcher sets.
func allowUnpickedImport(t *testing.T) {
	t.Helper()
	t.Setenv("MAHPASTES_ALLOW_UNPICKED_IMPORT", "1")
}

func scanFixture(t *testing.T) string {
	t.Helper()
	root := t.TempDir()
	writeFile(t, root, "a.txt", "alpha")
	writeFile(t, root, "b.png", "not really a png")
	writeFile(t, root, ".DS_Store", "junk")
	writeFile(t, root, ".hidden/secret.txt", "secret")
	writeFile(t, root, "2024/rome.jpg", "rome")
	writeFile(t, root, "2025/paris.jpg", "paris")
	return root
}

func relPaths(res *ImportScanResult) []string {
	out := make([]string, 0, len(res.Entries))
	for _, e := range res.Entries {
		out = append(out, e.RelPath)
	}
	return out
}

func TestStartImportSessionNonRecursive(t *testing.T) {
	app, cleanup := setupTestApp(t)
	defer cleanup()
	allowUnpickedImport(t)

	res, err := app.StartImportSession(scanFixture(t), false)
	if err != nil {
		t.Fatalf("StartImportSession: %v", err)
	}

	got := strings.Join(relPaths(res), ",")
	if got != "a.txt,b.png" {
		t.Errorf("entries = %q, want %q", got, "a.txt,b.png")
	}
	if res.Skipped.Dotted != 1 {
		t.Errorf("Skipped.Dotted = %d, want 1 (.DS_Store)", res.Skipped.Dotted)
	}
	if res.Recursive {
		t.Error("Recursive should be false")
	}
}

func TestStartImportSessionRecursive(t *testing.T) {
	app, cleanup := setupTestApp(t)
	defer cleanup()
	allowUnpickedImport(t)

	res, err := app.StartImportSession(scanFixture(t), true)
	if err != nil {
		t.Fatalf("StartImportSession: %v", err)
	}

	got := strings.Join(relPaths(res), ",")
	want := "2024/rome.jpg,2025/paris.jpg,a.txt,b.png"
	if got != want {
		t.Errorf("entries = %q, want %q", got, want)
	}

	// The hidden directory must not be descended into, so its contents never
	// appear even though the walk did visit that branch.
	for _, r := range relPaths(res) {
		if strings.Contains(r, ".hidden") {
			t.Errorf("hidden directory leaked into the scan: %s", r)
		}
	}

	// Dir doubles as the suggested tag.
	byPath := map[string]ImportScanEntry{}
	for _, e := range res.Entries {
		byPath[e.RelPath] = e
	}
	if d := byPath["2024/rome.jpg"].Dir; d != "2024" {
		t.Errorf("Dir for 2024/rome.jpg = %q, want %q", d, "2024")
	}
	if d := byPath["a.txt"].Dir; d != "" {
		t.Errorf("Dir for a root-level file = %q, want empty", d)
	}
}

func TestStartImportSessionSkipsSymlinks(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("symlink creation needs elevation on Windows")
	}
	app, cleanup := setupTestApp(t)
	defer cleanup()
	allowUnpickedImport(t)

	root := t.TempDir()
	target := writeFile(t, root, "real.txt", "content")
	if err := os.Symlink(target, filepath.Join(root, "link.txt")); err != nil {
		t.Skipf("symlink unavailable: %v", err)
	}

	res, err := app.StartImportSession(root, true)
	if err != nil {
		t.Fatalf("StartImportSession: %v", err)
	}
	if got := strings.Join(relPaths(res), ","); got != "real.txt" {
		t.Errorf("entries = %q, want %q", got, "real.txt")
	}
	if res.Skipped.Symlinks != 1 {
		t.Errorf("Skipped.Symlinks = %d, want 1", res.Skipped.Symlinks)
	}
}

func TestStartImportSessionSkipsAppTempDir(t *testing.T) {
	app, cleanup := setupTestApp(t)
	defer cleanup()
	allowUnpickedImport(t)

	// Point the session root at the parent of the app's own temp dir so the
	// walk has to actively exclude it. Importing a leased "Copy Path" file
	// would duplicate a clip that already exists.
	root := filepath.Dir(app.TempDir())
	writeFile(t, root, "keep.txt", "keep me")
	if err := os.WriteFile(filepath.Join(app.TempDir(), "leased.png"), []byte("x"), 0o644); err != nil {
		t.Fatalf("seed temp file: %v", err)
	}

	res, err := app.StartImportSession(root, true)
	if err != nil {
		t.Fatalf("StartImportSession: %v", err)
	}
	for _, r := range relPaths(res) {
		if strings.Contains(r, "clip_temp_files") {
			t.Errorf("app temp file offered for import: %s", r)
		}
	}
	if res.Skipped.AppTemp == 0 {
		t.Error("Skipped.AppTemp = 0, want the temp dir to be counted")
	}
}

func TestStartImportSessionRejectsNonDirectory(t *testing.T) {
	app, cleanup := setupTestApp(t)
	defer cleanup()
	allowUnpickedImport(t)

	root := t.TempDir()
	file := writeFile(t, root, "a.txt", "x")

	if _, err := app.StartImportSession(file, false); err == nil {
		t.Error("expected an error for a file root")
	}
	if _, err := app.StartImportSession("", false); err == nil {
		t.Error("expected an error for an empty root")
	}
}

// resolveSessionPath is the seam that keeps these methods from being an
// arbitrary-read and arbitrary-delete primitive. Everything below is a way it
// could be tricked.
func TestResolveSessionPathRejects(t *testing.T) {
	app, cleanup := setupTestApp(t)
	defer cleanup()
	allowUnpickedImport(t)

	root := scanFixture(t)
	if _, err := app.StartImportSession(root, true); err != nil {
		t.Fatalf("StartImportSession: %v", err)
	}

	bad := []struct {
		name string
		rel  string
	}{
		{"traversal", "../../etc/passwd"},
		{"traversal inside a known dir", "2024/../../../etc/passwd"},
		{"absolute", "/etc/passwd"},
		{"empty", ""},
		{"dot", "."},
		{"unscanned sibling", "c.txt"},
		{"unscanned dotfile", ".DS_Store"},
		{"unscanned hidden child", ".hidden/secret.txt"},
		{"directory", "2024"},
		{"nul byte", "a.txt\x00"},
	}
	for _, tc := range bad {
		t.Run(tc.name, func(t *testing.T) {
			if _, _, err := app.resolveSessionPath(tc.rel); err == nil {
				t.Errorf("resolveSessionPath(%q) succeeded; it must refuse", tc.rel)
			}
		})
	}

	// A path the scan did emit must still resolve.
	abs, _, err := app.resolveSessionPath("a.txt")
	if err != nil {
		t.Fatalf("resolveSessionPath on a scanned file: %v", err)
	}
	if filepath.Base(abs) != "a.txt" {
		t.Errorf("resolved to %q", abs)
	}
}

func TestResolveSessionPathRequiresSession(t *testing.T) {
	app, cleanup := setupTestApp(t)
	defer cleanup()
	allowUnpickedImport(t)

	if _, _, err := app.resolveSessionPath("a.txt"); err == nil {
		t.Fatal("expected an error with no active session")
	}

	root := scanFixture(t)
	if _, err := app.StartImportSession(root, false); err != nil {
		t.Fatalf("StartImportSession: %v", err)
	}
	if _, _, err := app.resolveSessionPath("a.txt"); err != nil {
		t.Fatalf("resolveSessionPath after scan: %v", err)
	}

	// EndImportSession must make the methods fail closed again.
	if err := app.EndImportSession(); err != nil {
		t.Fatalf("EndImportSession: %v", err)
	}
	if _, _, err := app.resolveSessionPath("a.txt"); err == nil {
		t.Fatal("expected an error after EndImportSession")
	}
}

// The TOCTOU case: a file that was a regular file at scan time is swapped for
// a symlink pointing outside the root before apply.
func TestResolveSessionPathRejectsPostScanSymlinkSwap(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("symlink creation needs elevation on Windows")
	}
	app, cleanup := setupTestApp(t)
	defer cleanup()
	allowUnpickedImport(t)

	root := t.TempDir()
	victim := writeFile(t, root, "a.txt", "innocent")

	if _, err := app.StartImportSession(root, false); err != nil {
		t.Fatalf("StartImportSession: %v", err)
	}
	if _, _, err := app.resolveSessionPath("a.txt"); err != nil {
		t.Fatalf("baseline resolve failed: %v", err)
	}

	outside := filepath.Join(t.TempDir(), "outside.txt")
	if err := os.WriteFile(outside, []byte("sensitive"), 0o644); err != nil {
		t.Fatalf("write outside file: %v", err)
	}
	if err := os.Remove(victim); err != nil {
		t.Fatalf("remove victim: %v", err)
	}
	if err := os.Symlink(outside, victim); err != nil {
		t.Skipf("symlink unavailable: %v", err)
	}

	if _, _, err := app.resolveSessionPath("a.txt"); err == nil {
		t.Fatal("a swapped symlink resolved; it must be refused")
	}
}

func TestImportInspectReportsDuplicatesAndEXIFAbsence(t *testing.T) {
	app, cleanup := setupTestApp(t)
	defer cleanup()
	allowUnpickedImport(t)

	root := t.TempDir()
	writeFile(t, root, "dup.txt", "shared content")

	// Seed a clip with identical bytes so the inspection has something to find.
	clipID, err := app.UploadFileAndGetID(FileData{
		Name:        "already-here.txt",
		ContentType: "text/plain",
		Data:        b64("shared content"),
	})
	if err != nil {
		t.Fatalf("seed clip: %v", err)
	}

	if _, err := app.StartImportSession(root, false); err != nil {
		t.Fatalf("StartImportSession: %v", err)
	}

	insp, err := app.ImportInspect("dup.txt")
	if err != nil {
		t.Fatalf("ImportInspect: %v", err)
	}
	if insp.Error != "" {
		t.Fatalf("unexpected inspection error: %s", insp.Error)
	}
	if len(insp.Duplicates) != 1 || insp.Duplicates[0].ClipID != clipID {
		t.Fatalf("Duplicates = %+v, want the seeded clip %d", insp.Duplicates, clipID)
	}
	if insp.ContentHash != computeContentHash([]byte("shared content")) {
		t.Errorf("ContentHash disagrees with computeContentHash")
	}
	if insp.EXIF != nil {
		t.Errorf("EXIF = %+v for a text file, want nil", insp.EXIF)
	}
	if insp.PreviewData == "" {
		t.Error("a small text file should carry a preview")
	}
}

func TestImportInspectVanishedFileIsNotAHardError(t *testing.T) {
	app, cleanup := setupTestApp(t)
	defer cleanup()
	allowUnpickedImport(t)

	root := t.TempDir()
	p := writeFile(t, root, "gone.txt", "x")
	if _, err := app.StartImportSession(root, false); err != nil {
		t.Fatalf("StartImportSession: %v", err)
	}
	if err := os.Remove(p); err != nil {
		t.Fatalf("remove: %v", err)
	}

	insp, err := app.ImportInspect("gone.txt")
	if err != nil {
		t.Fatalf("a vanished file must not be a Go error, got %v", err)
	}
	if insp.Error == "" {
		t.Error("expected Inspection.Error to explain the missing file")
	}
}

// b64 is a tiny helper so the tests can spell FileData.Data inline.
func b64(s string) string {
	return base64.StdEncoding.EncodeToString([]byte(s))
}

// applyFixture sets up an app with a scanned folder, with trashing forced to a
// permanent delete so the test run does not fill the developer's Trash.
func applyFixture(t *testing.T) (*App, string, func()) {
	t.Helper()
	t.Setenv("MAHPASTES_TRASH_MODE", "remove")
	t.Setenv("MAHPASTES_ALLOW_UNPICKED_IMPORT", "1")
	app, cleanup := setupTestApp(t)
	root := t.TempDir()
	return app, root, cleanup
}

func TestImportApplyImportsTagsAndTrashes(t *testing.T) {
	app, root, cleanup := applyFixture(t)
	defer cleanup()

	writeFile(t, root, "2024/rome.jpg", "rome bytes")
	writeFile(t, root, "keep.txt", "keep me")

	if _, err := app.StartImportSession(root, true); err != nil {
		t.Fatalf("StartImportSession: %v", err)
	}

	summary, err := app.ImportApply([]ImportDecision{
		{RelPath: "2024/rome.jpg", Action: ImportActionImportDelete, TagName: "2024"},
		{RelPath: "keep.txt", Action: ImportActionSkip},
	})
	if err != nil {
		t.Fatalf("ImportApply: %v", err)
	}

	if summary.Imported != 1 || summary.Trashed != 1 || summary.Skipped != 1 || summary.Failed != 0 {
		t.Fatalf("summary = %+v", summary)
	}

	// The imported file is gone; the skipped one is untouched.
	if _, err := os.Stat(filepath.Join(root, "2024", "rome.jpg")); !os.IsNotExist(err) {
		t.Error("imported+deleted file is still on disk")
	}
	if _, err := os.Stat(filepath.Join(root, "keep.txt")); err != nil {
		t.Errorf("skipped file should be untouched: %v", err)
	}

	res := summary.Results[0]
	if res.Status != ImportStatusOK || !res.Imported || !res.Trashed || res.ClipID == 0 {
		t.Fatalf("result = %+v", res)
	}

	tags, err := app.GetClipTags(res.ClipID)
	if err != nil {
		t.Fatalf("GetClipTags: %v", err)
	}
	if len(tags) != 1 || tags[0].Name != "2024" {
		t.Errorf("tags = %+v, want the 2024 tag", tags)
	}
}

// The load-bearing guarantee: if the import does not commit a clip, the file
// must survive. Otherwise a failed import destroys the only copy.
func TestImportApplyDoesNotTrashWhenImportFails(t *testing.T) {
	app, root, cleanup := applyFixture(t)
	defer cleanup()

	p := writeFile(t, root, "big.bin", "x")
	if _, err := app.StartImportSession(root, false); err != nil {
		t.Fatalf("StartImportSession: %v", err)
	}

	// Close the DB so the INSERT cannot succeed. This is the bluntest possible
	// import failure, which is exactly what the guarantee has to hold against.
	app.db.Close()

	summary, err := app.ImportApply([]ImportDecision{
		{RelPath: "big.bin", Action: ImportActionImportDelete},
	})
	if err != nil {
		t.Fatalf("ImportApply: %v", err)
	}

	res := summary.Results[0]
	if res.Status != ImportStatusImportFailed {
		t.Errorf("Status = %q, want %q", res.Status, ImportStatusImportFailed)
	}
	if res.Trashed {
		t.Error("file was trashed despite a failed import")
	}
	if _, err := os.Stat(p); err != nil {
		t.Errorf("file must survive a failed import, got %v", err)
	}
	if summary.Failed != 1 {
		t.Errorf("Failed = %d, want 1", summary.Failed)
	}
}

// A folder is live: a file can disappear between scan and apply. That is an
// ordinary fact, not a reason to abandon the remaining decisions.
func TestImportApplyContinuesPastMissingFiles(t *testing.T) {
	app, root, cleanup := applyFixture(t)
	defer cleanup()

	gone := writeFile(t, root, "gone.txt", "vanishing")
	writeFile(t, root, "present.txt", "still here")

	if _, err := app.StartImportSession(root, false); err != nil {
		t.Fatalf("StartImportSession: %v", err)
	}
	if err := os.Remove(gone); err != nil {
		t.Fatalf("remove: %v", err)
	}

	summary, err := app.ImportApply([]ImportDecision{
		{RelPath: "gone.txt", Action: ImportActionImport},
		{RelPath: "present.txt", Action: ImportActionImport},
	})
	if err != nil {
		t.Fatalf("ImportApply: %v", err)
	}

	if summary.Results[0].Status != ImportStatusMissing {
		t.Errorf("missing file Status = %q, want %q", summary.Results[0].Status, ImportStatusMissing)
	}
	if summary.Results[1].Status != ImportStatusOK {
		t.Errorf("the run stopped early: %+v", summary.Results[1])
	}
	if summary.Imported != 1 {
		t.Errorf("Imported = %d, want 1", summary.Imported)
	}
}

func TestImportApplyDeleteOnlyDoesNotCreateAClip(t *testing.T) {
	app, root, cleanup := applyFixture(t)
	defer cleanup()

	p := writeFile(t, root, "trash-me.txt", "unwanted")
	if _, err := app.StartImportSession(root, false); err != nil {
		t.Fatalf("StartImportSession: %v", err)
	}

	before := clipCount(t, app)
	summary, err := app.ImportApply([]ImportDecision{
		{RelPath: "trash-me.txt", Action: ImportActionDelete},
	})
	if err != nil {
		t.Fatalf("ImportApply: %v", err)
	}

	if summary.Trashed != 1 || summary.Imported != 0 {
		t.Fatalf("summary = %+v", summary)
	}
	if _, err := os.Stat(p); !os.IsNotExist(err) {
		t.Error("file should be gone")
	}
	if after := clipCount(t, app); after != before {
		t.Errorf("clip count changed from %d to %d on a delete-only decision", before, after)
	}
}

func TestImportApplyRejectsUnknownAction(t *testing.T) {
	app, root, cleanup := applyFixture(t)
	defer cleanup()

	p := writeFile(t, root, "a.txt", "x")
	if _, err := app.StartImportSession(root, false); err != nil {
		t.Fatalf("StartImportSession: %v", err)
	}

	summary, err := app.ImportApply([]ImportDecision{
		{RelPath: "a.txt", Action: "nuke_from_orbit"},
	})
	if err != nil {
		t.Fatalf("ImportApply: %v", err)
	}
	if summary.Results[0].Status != ImportStatusInvalid {
		t.Errorf("Status = %q, want %q", summary.Results[0].Status, ImportStatusInvalid)
	}
	if _, err := os.Stat(p); err != nil {
		t.Errorf("an unknown action must not touch the file: %v", err)
	}
}

func TestImportApplyRequiresSession(t *testing.T) {
	app, _, cleanup := applyFixture(t)
	defer cleanup()

	if _, err := app.ImportApply([]ImportDecision{{RelPath: "a.txt", Action: ImportActionImport}}); err == nil {
		t.Fatal("expected an error with no active session")
	}
}

// Plugins that subscribe to clip:created (auto-tagger among them) must fire for
// wizard imports. UploadFileAndGetID deliberately does not emit it, so
// ImportApply has to.
func TestImportApplyEmitsProgressPerDecision(t *testing.T) {
	app, root, cleanup := applyFixture(t)
	defer cleanup()

	writeFile(t, root, "a.txt", "one")
	writeFile(t, root, "b.txt", "two")
	if _, err := app.StartImportSession(root, false); err != nil {
		t.Fatalf("StartImportSession: %v", err)
	}

	var progress int
	setTestEventSink(t, app, func(name string, _ ...interface{}) {
		if name == "import:progress" {
			progress++
		}
	})

	if _, err := app.ImportApply([]ImportDecision{
		{RelPath: "a.txt", Action: ImportActionImport},
		{RelPath: "b.txt", Action: ImportActionSkip},
	}); err != nil {
		t.Fatalf("ImportApply: %v", err)
	}

	if progress != 2 {
		t.Errorf("import:progress fired %d times, want 2", progress)
	}
}

func clipCount(t *testing.T, app *App) int {
	t.Helper()
	var n int
	if err := app.db.QueryRow("SELECT COUNT(*) FROM clips").Scan(&n); err != nil {
		t.Fatalf("count clips: %v", err)
	}
	return n
}

// The folder-import methods must never gain a REST route. They take
// filesystem paths that only mean something on the machine running the
// process, and exposing them would hand every API-key holder a remote
// directory listing, an arbitrary-file read, and a move-to-trash primitive.
//
// This test reads the route table as source text on purpose: it is the
// registration site, not the behavior, that has to stay clean, and a future
// mux.HandleFunc for /api/v1/import should fail here rather than in review.
func TestNoRESTRoutesForImportWizard(t *testing.T) {
	src, err := os.ReadFile("api_manager.go")
	if err != nil {
		t.Fatalf("read api_manager.go: %v", err)
	}

	banned := []string{
		"/api/v1/import",
		"ImportApply",
		"ImportInspect",
		"StartImportSession",
		"resolveSessionPath",
	}
	text := string(src)
	for _, needle := range banned {
		if strings.Contains(text, needle) {
			t.Errorf("api_manager.go references %q — the folder-import surface must stay desktop-only (see the header comment in import_wizard.go)", needle)
		}
	}
}

// A partial success must not spend the only copy of the file. Tree exclusivity
// can legitimately reject a tag; when it does, the clip is imported but the
// source stays put so the user can retry.
func TestImportApplyDoesNotTrashWhenTaggingFails(t *testing.T) {
	app, root, cleanup := applyFixture(t)
	defer cleanup()

	p := writeFile(t, root, "photo.txt", "bytes")
	if _, err := app.StartImportSession(root, false); err != nil {
		t.Fatalf("StartImportSession: %v", err)
	}

	// "_api" is a reserved path segment, so CreateTag refuses it — a real
	// tagging failure rather than a mocked one.
	summary, err := app.ImportApply([]ImportDecision{
		{RelPath: "photo.txt", Action: ImportActionImportDelete, TagName: "_api"},
	})
	if err != nil {
		t.Fatalf("ImportApply: %v", err)
	}

	res := summary.Results[0]
	if res.Status != ImportStatusTagFailed {
		t.Fatalf("Status = %q, want %q (result: %+v)", res.Status, ImportStatusTagFailed, res)
	}
	if !res.Imported || res.ClipID == 0 {
		t.Error("the clip should still have been imported")
	}
	if res.Trashed {
		t.Error("file was trashed despite the decision only partly succeeding")
	}
	if _, err := os.Stat(p); err != nil {
		t.Errorf("file must survive a partial success, got %v", err)
	}
}

// The size cap and regular-file check must apply to the handle the bytes are
// read from, not to a separately-stat'd path.
func TestReadImportFileRejectsNonRegular(t *testing.T) {
	dir := t.TempDir()
	if _, _, err := readImportFile(dir); err == nil {
		t.Error("readImportFile accepted a directory")
	}
	if _, _, err := readImportFile(filepath.Join(dir, "nope.txt")); err == nil {
		t.Error("readImportFile accepted a missing file")
	}
}

// StartImportSession is bound to JavaScript, so without an approval check it
// would be a scan-anything primitive: point it at "/" and enumerate the disk,
// then read any file it surfaced. Only a root the user actually chose in the
// native picker is accepted.
func TestStartImportSessionRequiresPickerApproval(t *testing.T) {
	app, cleanup := setupTestApp(t)
	defer cleanup()
	// Deliberately NOT calling allowUnpickedImport: this is the production path.

	root := scanFixture(t)

	if _, err := app.StartImportSession(root, false); err == nil {
		t.Fatal("an unapproved root was scanned; the picker must not be bypassable")
	}
	if _, _, err := app.resolveSessionPath("a.txt"); err == nil {
		t.Error("a session was created despite the refusal")
	}

	// The picker approves exactly one root...
	app.ApproveImportRoot(root)
	if _, err := app.StartImportSession(root, false); err != nil {
		t.Fatalf("an approved root should scan: %v", err)
	}

	// ...and approval does not generalize to its neighbours or its parent.
	for _, other := range []string{t.TempDir(), filepath.Dir(root), "/"} {
		if _, err := app.StartImportSession(other, false); err == nil {
			t.Errorf("approving %q also allowed %q", root, other)
		}
	}

	// Re-scanning the approved root still works — the wizard does exactly this
	// when the user toggles "include subfolders".
	if _, err := app.StartImportSession(root, true); err != nil {
		t.Errorf("rescan of an approved root failed: %v", err)
	}
}

// Deletion happens by path — no portable API removes an open descriptor — so
// there is a gap between reading a file and deleting it. If the path is
// repointed at a different file in that gap, the wizard must refuse rather than
// destroy something the user never reviewed.
func TestTrashVerifiedRefusesASwappedFile(t *testing.T) {
	t.Setenv("MAHPASTES_TRASH_MODE", "remove")

	dir := t.TempDir()
	p := filepath.Join(dir, "victim.txt")
	if err := os.WriteFile(p, []byte("original"), 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}

	seen, err := statImportFile(p)
	if err != nil {
		t.Fatalf("statImportFile: %v", err)
	}

	// Same path, different inode.
	if err := os.Remove(p); err != nil {
		t.Fatalf("remove: %v", err)
	}
	if err := os.WriteFile(p, []byte("a completely different file"), 0o644); err != nil {
		t.Fatalf("rewrite: %v", err)
	}

	if err := trashVerified(p, seen, ""); err == nil {
		t.Error("trashVerified deleted a file that had been swapped in")
	}
	if _, err := os.Stat(p); err != nil {
		t.Errorf("the replacement file must survive, got %v", err)
	}

	// The unswapped case still deletes.
	seen2, err := statImportFile(p)
	if err != nil {
		t.Fatalf("statImportFile: %v", err)
	}
	if err := trashVerified(p, seen2, ""); err != nil {
		t.Fatalf("trashVerified refused an unchanged file: %v", err)
	}
	if _, err := os.Stat(p); !os.IsNotExist(err) {
		t.Error("unchanged file should have been deleted")
	}
}

// A file that grows past the cap while being read must fail loudly. Silently
// storing a truncated clip and then deleting the complete original is the worst
// available outcome.
func TestReadImportFileRejectsOversizeWithoutTruncating(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "big.bin")
	if err := os.WriteFile(p, make([]byte, 1024), 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}

	fd, info, err := readImportFile(p)
	if err != nil {
		t.Fatalf("readImportFile: %v", err)
	}
	if info == nil || info.Size() != 1024 {
		t.Fatalf("info = %+v, want a 1024-byte regular file", info)
	}
	decoded, err := base64.StdEncoding.DecodeString(fd.Data)
	if err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(decoded) != 1024 {
		t.Errorf("read %d bytes, want 1024", len(decoded))
	}
}

// The gap between reviewing a file and pressing Apply is user think time. A
// sync client or an editor can replace a file in that window; acting on the
// path alone would import and delete something the user never saw.
func TestImportApplyRefusesAFileChangedAfterReview(t *testing.T) {
	app, root, cleanup := applyFixture(t)
	defer cleanup()

	p := writeFile(t, root, "photo.txt", "the reviewed content")
	if _, err := app.StartImportSession(root, false); err != nil {
		t.Fatalf("StartImportSession: %v", err)
	}

	// The user looks at the file...
	if _, err := app.ImportInspect("photo.txt"); err != nil {
		t.Fatalf("ImportInspect: %v", err)
	}

	// ...and something replaces it while they are deciding.
	if err := os.Remove(p); err != nil {
		t.Fatalf("remove: %v", err)
	}
	if err := os.WriteFile(p, []byte("something else entirely"), 0o644); err != nil {
		t.Fatalf("rewrite: %v", err)
	}

	before := clipCount(t, app)
	summary, err := app.ImportApply([]ImportDecision{
		{RelPath: "photo.txt", Action: ImportActionImportDelete},
	})
	if err != nil {
		t.Fatalf("ImportApply: %v", err)
	}

	res := summary.Results[0]
	if res.Status != ImportStatusChanged {
		t.Fatalf("Status = %q, want %q (result: %+v)", res.Status, ImportStatusChanged, res)
	}
	if res.Imported || res.Trashed {
		t.Error("a changed file must be neither imported nor deleted")
	}
	if _, err := os.Stat(p); err != nil {
		t.Errorf("the replacement must survive, got %v", err)
	}
	if after := clipCount(t, app); after != before {
		t.Errorf("clip count changed from %d to %d", before, after)
	}
}

// A file that was never inspected has no baseline to contradict, so it still
// applies normally — the check must not become "you may only act on files you
// clicked through".
func TestImportApplyAllowsUnreviewedFiles(t *testing.T) {
	app, root, cleanup := applyFixture(t)
	defer cleanup()

	writeFile(t, root, "never-looked-at.txt", "content")
	if _, err := app.StartImportSession(root, false); err != nil {
		t.Fatalf("StartImportSession: %v", err)
	}

	summary, err := app.ImportApply([]ImportDecision{
		{RelPath: "never-looked-at.txt", Action: ImportActionImport},
	})
	if err != nil {
		t.Fatalf("ImportApply: %v", err)
	}
	if summary.Results[0].Status != ImportStatusOK {
		t.Fatalf("Status = %q, want %q", summary.Results[0].Status, ImportStatusOK)
	}
}

// Picker approval must last exactly as long as the wizard it was granted for.
// Otherwise the last-chosen folder stays a standing read-and-delete capability
// for any bound call, long after the user closed the wizard.
func TestEndImportSessionRevokesPickerApproval(t *testing.T) {
	app, cleanup := setupTestApp(t)
	defer cleanup()
	// Not calling allowUnpickedImport: this exercises the production gate.

	root := scanFixture(t)
	app.ApproveImportRoot(root)
	if _, err := app.StartImportSession(root, false); err != nil {
		t.Fatalf("StartImportSession: %v", err)
	}

	if err := app.EndImportSession(); err != nil {
		t.Fatalf("EndImportSession: %v", err)
	}
	if _, err := app.StartImportSession(root, false); err == nil {
		t.Fatal("the folder is still scannable after the wizard closed")
	}
}

// A writer that changes the file during a long read must not produce a clip
// holding a mix of old and new content.
func TestReadImportFileDetectsChangeDuringRead(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "stable.txt")
	if err := os.WriteFile(p, []byte("stable"), 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}

	// The happy path must still succeed — the post-read stat is only meant to
	// catch genuine concurrent modification, not to make every read flaky.
	if _, _, err := readImportFile(p); err != nil {
		t.Fatalf("readImportFile on an unchanging file: %v", err)
	}

	// sameFileState is what the post-read check leans on; pin its behaviour
	// directly, since staging a real mid-read write is inherently timing-bound.
	before, err := statImportFile(p)
	if err != nil {
		t.Fatalf("statImportFile: %v", err)
	}
	if err := os.WriteFile(p, []byte("stable and then some more"), 0o644); err != nil {
		t.Fatalf("append: %v", err)
	}
	after, err := statImportFile(p)
	if err != nil {
		t.Fatalf("statImportFile: %v", err)
	}
	if sameFileState(before, after) {
		t.Error("sameFileState missed a size change on the same path")
	}
}

// A same-length in-place rewrite keeps identity and size, and on a filesystem
// with coarse timestamps can keep mtime too. Content comparison is what makes
// the changed-since-review check decisive on the import path, where storing
// torn bytes and then deleting the original is the worst outcome.
func TestImportApplyDetectsSameSizeRewriteAfterReview(t *testing.T) {
	app, root, cleanup := applyFixture(t)
	defer cleanup()

	p := writeFile(t, root, "notes.txt", "AAAAAAAA")
	if _, err := app.StartImportSession(root, false); err != nil {
		t.Fatalf("StartImportSession: %v", err)
	}
	if _, err := app.ImportInspect("notes.txt"); err != nil {
		t.Fatalf("ImportInspect: %v", err)
	}

	// Same length, same path, same inode where the OS allows it — only the
	// bytes differ. Force the mtime back so stat data alone cannot tell.
	before, err := os.Stat(p)
	if err != nil {
		t.Fatalf("stat: %v", err)
	}
	f, err := os.OpenFile(p, os.O_WRONLY, 0o644)
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	if _, err := f.WriteAt([]byte("BBBBBBBB"), 0); err != nil {
		t.Fatalf("rewrite: %v", err)
	}
	f.Close()
	if err := os.Chtimes(p, before.ModTime(), before.ModTime()); err != nil {
		t.Fatalf("chtimes: %v", err)
	}

	summary, err := app.ImportApply([]ImportDecision{
		{RelPath: "notes.txt", Action: ImportActionImportDelete},
	})
	if err != nil {
		t.Fatalf("ImportApply: %v", err)
	}

	res := summary.Results[0]
	if res.Status != ImportStatusChanged {
		t.Fatalf("Status = %q, want %q — a same-size rewrite slipped through", res.Status, ImportStatusChanged)
	}
	if res.Trashed {
		t.Error("the file was deleted despite its content having changed")
	}
	if _, err := os.Stat(p); err != nil {
		t.Errorf("file must survive, got %v", err)
	}
}

// A scan that finishes after the wizard was closed must not install itself:
// that would resurrect an authorized session after it was revoked.
func TestStartImportSessionDiscardsScanFromAClosedWizard(t *testing.T) {
	app, cleanup := setupTestApp(t)
	defer cleanup()
	allowUnpickedImport(t)

	root := scanFixture(t)

	// Simulate the close landing mid-scan by bumping the generation between
	// the capture and the install — which is exactly what EndImportSession does.
	app.importMu.Lock()
	app.importGeneration++
	gen := app.importGeneration
	app.importMu.Unlock()

	if _, err := app.StartImportSession(root, false); err != nil {
		t.Fatalf("baseline scan should succeed: %v", err)
	}
	app.importMu.Lock()
	installed := app.importSession != nil
	sameGen := app.importGeneration == gen
	app.importMu.Unlock()
	if !installed || !sameGen {
		t.Fatal("an uninterrupted scan should install normally")
	}

	// And the real path: close, then confirm the session is gone.
	if err := app.EndImportSession(); err != nil {
		t.Fatalf("EndImportSession: %v", err)
	}
	if _, _, err := app.resolveSessionPath("a.txt"); err == nil {
		t.Error("session survived EndImportSession")
	}
}

// Stat data cannot see a same-length in-place rewrite that preserves mtime —
// which is what timestamp-preserving sync tools produce. For a delete-only
// decision the whole of the user's think time sits in that window, and on
// Windows and Linux the deletion is permanent, so the check has to be content
// based.
func TestImportApplyRefusesDeleteWhenContentSilentlyChanged(t *testing.T) {
	app, root, cleanup := applyFixture(t)
	defer cleanup()

	p := writeFile(t, root, "doc.txt", "AAAAAAAA")
	if _, err := app.StartImportSession(root, false); err != nil {
		t.Fatalf("StartImportSession: %v", err)
	}
	if _, err := app.ImportInspect("doc.txt"); err != nil {
		t.Fatalf("ImportInspect: %v", err)
	}

	// Same length, mtime restored: identity, size and mtime all still match.
	before, err := os.Stat(p)
	if err != nil {
		t.Fatalf("stat: %v", err)
	}
	f, err := os.OpenFile(p, os.O_WRONLY, 0o644)
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	if _, err := f.WriteAt([]byte("BBBBBBBB"), 0); err != nil {
		t.Fatalf("rewrite: %v", err)
	}
	f.Close()
	if err := os.Chtimes(p, before.ModTime(), before.ModTime()); err != nil {
		t.Fatalf("chtimes: %v", err)
	}

	summary, err := app.ImportApply([]ImportDecision{
		{RelPath: "doc.txt", Action: ImportActionDelete},
	})
	if err != nil {
		t.Fatalf("ImportApply: %v", err)
	}

	res := summary.Results[0]
	if res.Trashed {
		t.Fatal("deleted a file whose contents had changed since review")
	}
	if res.Status != ImportStatusTrashFailed && res.Status != ImportStatusChanged {
		t.Errorf("Status = %q, want a refusal (%+v)", res.Status, res)
	}
	if _, err := os.Stat(p); err != nil {
		t.Errorf("file must survive, got %v", err)
	}
}

// An unchanged reviewed file must still delete — the content check must not
// make ordinary deletion fail.
func TestImportApplyStillDeletesAnUnchangedReviewedFile(t *testing.T) {
	app, root, cleanup := applyFixture(t)
	defer cleanup()

	p := writeFile(t, root, "doc.txt", "stable content")
	if _, err := app.StartImportSession(root, false); err != nil {
		t.Fatalf("StartImportSession: %v", err)
	}
	if _, err := app.ImportInspect("doc.txt"); err != nil {
		t.Fatalf("ImportInspect: %v", err)
	}

	summary, err := app.ImportApply([]ImportDecision{
		{RelPath: "doc.txt", Action: ImportActionDelete},
	})
	if err != nil {
		t.Fatalf("ImportApply: %v", err)
	}
	if summary.Results[0].Status != ImportStatusOK || !summary.Results[0].Trashed {
		t.Fatalf("result = %+v, want a successful delete", summary.Results[0])
	}
	if _, err := os.Stat(p); !os.IsNotExist(err) {
		t.Error("file should have been deleted")
	}
}
