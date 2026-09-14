package fileprovider

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"io"
	"sort"
	"strconv"
	"strings"
	"sync"
)

type Store struct {
	db    *sql.DB
	mu    sync.Mutex
	epoch string
}

// Head lets the notifier observe commits even when a request, rather than the
// background worker, consumed the dirty queue and advanced the journal.
func (s *Store) Head(ctx context.Context) (string, error) {
	var head string
	err := s.db.QueryRowContext(ctx, `SELECT epoch||':'||COALESCE((SELECT MAX(seq) FROM fp_changes),0) FROM fp_state`).Scan(&head)
	return head, err
}

// Scopes returns every container that can currently have a live Finder
// enumerator. The host uses this after a projection change to wake dynamic tag
// folders as well as the fixed root containers. A folder can be empty, so both
// its own identifier and every recorded parent are included.
func (s *Store) Scopes(ctx context.Context) ([]string, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT id,parent FROM fp_items`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	scopes := map[string]bool{"root": true, "active": true, "archive": true, "tags": true, "working": true}
	for rows.Next() {
		var id, parent string
		if err = rows.Scan(&id, &parent); err != nil {
			return nil, err
		}
		scopes[parent] = true
		if strings.HasPrefix(id, "tag:") {
			scopes[id] = true
		}
	}
	if err = rows.Err(); err != nil {
		return nil, err
	}
	result := make([]string, 0, len(scopes))
	for scope := range scopes {
		result = append(result, scope)
	}
	sort.Strings(result)
	return result, nil
}

func Open(ctx context.Context, db *sql.DB) (*Store, error) {
	if err := initialize(ctx, db); err != nil {
		return nil, err
	}
	s := &Store{db: db}
	_, err := s.Sync(ctx)
	return s, err
}

// visible is evaluated for canonical Active/Archive items in every content
// read, including the seconds between an expiry/hidden-tag change and the
// projection worker processing it. Tag-folder aliases deliberately remain
// readable through a hidden tag's hidden directory.
const visible = `(c.expires_at IS NULL OR julianday(c.expires_at)>julianday('now')) AND NOT EXISTS (
 SELECT 1 FROM clip_tags ct JOIN tags t ON t.id=ct.tag_id JOIN tags hidden
 ON (t.name=hidden.name OR substr(t.name,1,length(hidden.name)+1)=hidden.name||'/')
 WHERE ct.clip_id=c.id AND hidden.id IN (SELECT value FROM json_each(COALESCE((SELECT value FROM settings WHERE key='hidden_tags'),'[]')))
)`

func state(ctx context.Context, tx *sql.Tx) (string, int64, error) {
	var epoch string
	var seq int64
	err := tx.QueryRowContext(ctx, `SELECT epoch,COALESCE((SELECT MAX(seq) FROM fp_changes),0) FROM fp_state`).Scan(&epoch, &seq)
	return epoch, seq, err
}

func folders(epoch string) []Item {
	items := []Item{}
	for _, name := range []string{"Active", "Archive", "Tags"} {
		items = append(items, Item{ID: strings.ToLower(name), Parent: "root", Name: name, MIME: "inode/directory", Folder: true, ContentVersion: epoch, MetadataVersion: epoch})
	}
	return items
}

// Sync consumes at most 256 dirty records per write transaction. Revisions,
// projection and immutable change records are committed together. No file data
// is copied into the journal. Direct SQL writers are covered by SQLite triggers.
func (s *Store) Sync(ctx context.Context) (bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	changed := false
	for {
		n, err := s.syncBatch(ctx)
		if err != nil {
			return changed, err
		}
		changed = changed || n > 0
		if n < 256 {
			var epoch string
			if err := s.db.QueryRowContext(ctx, "SELECT epoch FROM fp_state").Scan(&epoch); err != nil {
				return changed, err
			}
			changed = changed || epoch != s.epoch
			s.epoch = epoch
			return changed, nil
		}
	}
}

func (s *Store) syncBatch(ctx context.Context) (int, error) {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return 0, err
	}
	defer tx.Rollback()
	// Acquire SQLite's writer reservation before reading to avoid upgrading an
	// obsolete WAL snapshot if another process/connection commits a clip change.
	if _, err = tx.ExecContext(ctx, `UPDATE fp_state SET version=version`); err != nil {
		return 0, err
	}
	epoch, _, err := state(ctx, tx)
	if err != nil {
		return 0, err
	}
	if _, err = tx.ExecContext(ctx, `INSERT OR IGNORE INTO fp_dirty SELECT DISTINCT c.id FROM clips c JOIN fp_items p ON p.clip_id=c.id WHERE c.expires_at IS NOT NULL AND julianday(c.expires_at)<=julianday('now')`); err != nil {
		return 0, err
	}
	tagsDirty, err := s.tagsDirty(ctx, tx)
	if err != nil {
		return 0, err
	}
	folderChanges := 0
	if tagsDirty {
		folderChanges, err = s.syncTagFolders(ctx, tx, epoch)
		if err != nil {
			return 0, err
		}
		if _, err = tx.ExecContext(ctx, `DELETE FROM fp_tags_dirty`); err != nil {
			return 0, err
		}
	}
	rows, err := tx.QueryContext(ctx, `SELECT clip_id FROM fp_dirty ORDER BY clip_id LIMIT 256`)
	if err != nil {
		return 0, err
	}
	ids := []int64{}
	for rows.Next() {
		var id int64
		if err = rows.Scan(&id); err != nil {
			rows.Close()
			return 0, err
		}
		ids = append(ids, id)
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return 0, err
	}
	for _, id := range ids {
		if err = s.syncClipItems(ctx, tx, id, epoch); err != nil && !errors.Is(err, ErrNoSuchItem) {
			return 0, err
		}
		if _, err = tx.ExecContext(ctx, `DELETE FROM fp_dirty WHERE clip_id=?`, id); err != nil {
			return 0, err
		}
	}
	// Bound metadata caches and journal growth. Old cursors explicitly expire.
	if _, err = tx.ExecContext(ctx, `DELETE FROM fp_snapshot_items WHERE snapshot IN (SELECT id FROM fp_snapshots WHERE expires<unixepoch()); DELETE FROM fp_snapshots WHERE expires<unixepoch(); DELETE FROM fp_changes WHERE seq<(SELECT COALESCE(MAX(seq),0)-100000 FROM fp_changes) OR (created<unixepoch()-2592000 AND seq<(SELECT MAX(seq) FROM fp_changes))`); err != nil {
		return 0, err
	}
	return len(ids) + folderChanges, tx.Commit()
}

func (s *Store) tagsDirty(ctx context.Context, tx *sql.Tx) (bool, error) {
	var count int
	if err := tx.QueryRowContext(ctx, `SELECT count(*) FROM fp_tags_dirty`).Scan(&count); err != nil {
		return false, err
	}
	return count > 0, nil
}

// syncTagFolders records directory changes separately from clip changes. The
// tag ID (not its display name) is the stable Finder identifier, so renaming a
// tag moves/updates one folder without invalidating files inside it.
func (s *Store) syncTagFolders(ctx context.Context, tx *sql.Tx, epoch string) (int, error) {
	desired, err := tagFolders(ctx, tx, epoch)
	if err != nil {
		return 0, err
	}
	old := map[string]Item{}
	rows, err := tx.QueryContext(ctx, `SELECT id,item FROM fp_items WHERE clip_id IS NULL`)
	if err != nil {
		return 0, err
	}
	for rows.Next() {
		var id, raw string
		if err = rows.Scan(&id, &raw); err != nil {
			rows.Close()
			return 0, err
		}
		var item Item
		if err = json.Unmarshal([]byte(raw), &item); err != nil {
			rows.Close()
			return 0, err
		}
		old[id] = item
	}
	if err = rows.Err(); err != nil {
		rows.Close()
		return 0, err
	}
	rows.Close()

	changes := 0
	for _, id := range treeOrder(old, true) {
		previous := old[id]
		if _, exists := desired[id]; exists {
			continue
		}
		if _, err = tx.ExecContext(ctx, `INSERT INTO fp_changes(id,old_parent,parent,item) VALUES(?,?, '',NULL)`, id, previous.Parent); err != nil {
			return 0, err
		}
		if _, err = tx.ExecContext(ctx, `DELETE FROM fp_items WHERE id=?`, id); err != nil {
			return 0, err
		}
		changes++
	}
	for _, id := range treeOrder(desired, false) {
		item := desired[id]
		previous, exists := old[id]
		if exists && previous == item {
			continue
		}
		if err = s.putItem(ctx, tx, item, nil); err != nil {
			return 0, err
		}
		if err = s.recordChange(ctx, tx, item, previous.Parent); err != nil {
			return 0, err
		}
		changes++
	}
	return changes, nil
}

// treeOrder puts parents before children for creates and updates, and children
// before parents for deletions. File Provider applies an item's parent relation
// immediately, so emitting a new child before its new parent is not safe.
func treeOrder(items map[string]Item, reverse bool) []string {
	ids := make([]string, 0, len(items))
	for id := range items {
		ids = append(ids, id)
	}
	depths := map[string]int{}
	visiting := map[string]bool{}
	var depth func(string) int
	depth = func(id string) int {
		if value, known := depths[id]; known {
			return value
		}
		if visiting[id] {
			return 0
		}
		visiting[id] = true
		value := 0
		if parent, found := items[items[id].Parent]; found {
			value = depth(parent.ID) + 1
		}
		delete(visiting, id)
		depths[id] = value
		return value
	}
	sort.Slice(ids, func(i, j int) bool {
		left, right := depth(ids[i]), depth(ids[j])
		if left == right {
			return ids[i] < ids[j]
		}
		if reverse {
			return left > right
		}
		return left < right
	})
	return ids
}

type tagRecord struct {
	id   int64
	name string
}

func tagFolders(ctx context.Context, tx *sql.Tx, epoch string) (map[string]Item, error) {
	rows, err := tx.QueryContext(ctx, `SELECT id,name FROM tags ORDER BY name,id`)
	if err != nil {
		return nil, err
	}
	var tags []tagRecord
	for rows.Next() {
		var t tagRecord
		if err = rows.Scan(&t.id, &t.name); err != nil {
			rows.Close()
			return nil, err
		}
		tags = append(tags, t)
	}
	if err = rows.Err(); err != nil {
		rows.Close()
		return nil, err
	}
	rows.Close()

	hiddenNames, err := hiddenTagNames(ctx, tx)
	if err != nil {
		return nil, err
	}
	return tagFolderItems(tags, hiddenNames, epoch), nil
}

type tagFolder struct {
	id, path, parentPath string
}

func tagFolderItems(tags []tagRecord, hiddenNames []string, epoch string) map[string]Item {
	// Every prefix is a folder, even if a malformed legacy or plugin-created
	// tag lacks an explicit ancestor row. Real tags retain ID-backed folder
	// identity; synthesized ancestors use a deterministic opaque path hash.
	folders := map[string]tagFolder{}
	for _, tag := range tags {
		parts := strings.Split(tag.name, "/")
		for index := range parts {
			path := strings.Join(parts[:index+1], "/")
			parentPath := strings.Join(parts[:index], "/")
			if _, found := folders[path]; !found {
				folders[path] = tagFolder{id: syntheticTagFolderID(epoch, path), path: path, parentPath: parentPath}
			}
		}
		folder := folders[tag.name]
		folder.id = tagFolderID(epoch, tag.id)
		folders[tag.name] = folder
	}

	byParent := map[string][]string{}
	for path, folder := range folders {
		byParent[folder.parentPath] = append(byParent[folder.parentPath], path)
	}
	names := map[string]string{}
	for _, paths := range byParent {
		byBase := map[string][]string{}
		for _, path := range paths {
			base := folderName(path[strings.LastIndex(path, "/")+1:])
			byBase[strings.ToLower(base)] = append(byBase[strings.ToLower(base)], path)
			names[path] = base
		}
		for _, collisions := range byBase {
			if len(collisions) < 2 {
				continue
			}
			for _, path := range collisions {
				names[path] += " [" + folderCollisionSuffix(folders[path].id) + "]"
			}
		}
	}

	paths := make([]string, 0, len(folders))
	for path := range folders {
		paths = append(paths, path)
	}
	sort.Slice(paths, func(i, j int) bool {
		left, right := strings.Count(paths[i], "/"), strings.Count(paths[j], "/")
		return left < right || left == right && paths[i] < paths[j]
	})
	items := make(map[string]Item, len(folders))
	for _, path := range paths {
		folder := folders[path]
		parent := "tags"
		if folder.parentPath != "" {
			parent = folders[folder.parentPath].id
		}
		hidden := tagIsHidden(path, hiddenNames)
		items[folder.id] = Item{
			ID: folder.id, Parent: parent, Name: names[path], MIME: "inode/directory", Folder: true, Hidden: hidden,
			ContentVersion: epoch, MetadataVersion: folderMetadataVersion(names[path], parent, hidden),
		}
	}
	return items
}

func hiddenTagNames(ctx context.Context, tx *sql.Tx) ([]string, error) {
	rows, err := tx.QueryContext(ctx, `SELECT name FROM tags WHERE id IN (SELECT value FROM json_each(COALESCE((SELECT value FROM settings WHERE key='hidden_tags'),'[]')))`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var names []string
	for rows.Next() {
		var name string
		if err = rows.Scan(&name); err != nil {
			return nil, err
		}
		names = append(names, name)
	}
	return names, rows.Err()
}

func tagIsHidden(name string, hiddenNames []string) bool {
	for _, hidden := range hiddenNames {
		if name == hidden || strings.HasPrefix(name, hidden+"/") {
			return true
		}
	}
	return false
}

func (s *Store) syncClipItems(ctx context.Context, tx *sql.Tx, id int64, epoch string) error {
	old := map[string]Item{}
	rows, err := tx.QueryContext(ctx, `SELECT id,item FROM fp_items WHERE clip_id=?`, id)
	if err != nil {
		return err
	}
	for rows.Next() {
		var itemID, raw string
		if err = rows.Scan(&itemID, &raw); err != nil {
			rows.Close()
			return err
		}
		var item Item
		if err = json.Unmarshal([]byte(raw), &item); err != nil {
			rows.Close()
			return err
		}
		old[itemID] = item
	}
	if err = rows.Err(); err != nil {
		rows.Close()
		return err
	}
	rows.Close()

	desired, err := desiredClipItems(ctx, tx, id, epoch)
	if err != nil && !errors.Is(err, ErrNoSuchItem) {
		return err
	}
	for itemID, previous := range old {
		if _, exists := desired[itemID]; exists {
			continue
		}
		if _, err = tx.ExecContext(ctx, `INSERT INTO fp_changes(id,old_parent,parent,item) VALUES(?,?, '',NULL)`, itemID, previous.Parent); err != nil {
			return err
		}
		if _, err = tx.ExecContext(ctx, `DELETE FROM fp_items WHERE id=?`, itemID); err != nil {
			return err
		}
	}
	for itemID, item := range desired {
		previous, exists := old[itemID]
		if exists && previous == item {
			continue
		}
		if err = s.putItem(ctx, tx, item, &id); err != nil {
			return err
		}
		if err = s.recordChange(ctx, tx, item, previous.Parent); err != nil {
			return err
		}
	}
	return nil
}

func (s *Store) putItem(ctx context.Context, tx *sql.Tx, item Item, clipID *int64) error {
	b, err := json.Marshal(item)
	if err != nil {
		return err
	}
	var source any
	if clipID != nil {
		source = *clipID
	}
	_, err = tx.ExecContext(ctx, `INSERT INTO fp_items(id,clip_id,parent,item) VALUES(?,?,?,?) ON CONFLICT(id) DO UPDATE SET clip_id=excluded.clip_id,parent=excluded.parent,item=excluded.item`, item.ID, source, item.Parent, string(b))
	return err
}

func (s *Store) recordChange(ctx context.Context, tx *sql.Tx, item Item, oldParent string) error {
	b, err := json.Marshal(item)
	if err != nil {
		return err
	}
	_, err = tx.ExecContext(ctx, `INSERT INTO fp_changes(id,old_parent,parent,item) VALUES(?,?,?,?)`, item.ID, oldParent, item.Parent, string(b))
	return err
}

func sourceRecord(ctx context.Context, tx *sql.Tx, id int64) (Item, string, error) {
	var i Item
	var uuid, name string
	var content, metadata int64
	var archived bool
	err := tx.QueryRowContext(ctx, `SELECT s.uuid,s.content_rev,s.metadata_rev,s.modified,COALESCE(c.filename,''),c.content_type,length(c.data),strftime('%Y-%m-%dT%H:%M:%fZ',c.created_at),c.is_archived FROM clips c JOIN fp_sources s ON s.clip_id=c.id WHERE c.id=? AND (c.expires_at IS NULL OR julianday(c.expires_at)>julianday('now'))`, id).Scan(&uuid, &content, &metadata, &i.Modified, &name, &i.MIME, &i.Size, &i.Created, &archived)
	if errors.Is(err, sql.ErrNoRows) {
		return i, "", ErrNoSuchItem
	}
	if err != nil {
		return i, "", err
	}
	i.Parent = "active"
	if archived {
		i.Parent = "archive"
	}
	i.Name = Filename(name, i.MIME, uuid)
	i.ContentVersion = strconv.FormatInt(content, 10)
	i.MetadataVersion = strconv.FormatInt(metadata, 10)
	return i, uuid, nil
}

func desiredClipItems(ctx context.Context, tx *sql.Tx, id int64, epoch string) (map[string]Item, error) {
	base, uuid, err := sourceRecord(ctx, tx, id)
	if err != nil {
		return nil, err
	}
	items := map[string]Item{}
	var shown bool
	if err = tx.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM clips c WHERE c.id=? AND `+visible+`)`, id).Scan(&shown); err != nil {
		return nil, err
	}
	if shown {
		canonical := base
		canonical.ID = epoch + ":" + uuid
		items[canonical.ID] = canonical
	}
	rows, err := tx.QueryContext(ctx, `SELECT t.id FROM clip_tags ct JOIN tags t ON t.id=ct.tag_id WHERE ct.clip_id=? ORDER BY t.id`, id)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	for rows.Next() {
		var tagID int64
		if err = rows.Scan(&tagID); err != nil {
			return nil, err
		}
		alias := base
		alias.ID = taggedItemID(epoch, uuid, tagID)
		alias.Parent = tagFolderID(epoch, tagID)
		items[alias.ID] = alias
	}
	return items, rows.Err()
}

func (s *Store) Item(ctx context.Context, id string) (Item, error) {
	tx, err := s.db.BeginTx(ctx, &sql.TxOptions{ReadOnly: true})
	if err != nil {
		return Item{}, err
	}
	defer tx.Rollback()
	epoch, _, err := state(ctx, tx)
	if err != nil {
		return Item{}, err
	}
	if id == "root" {
		return Item{ID: "root", Parent: "root", Name: "Mahpastes", MIME: "inode/directory", Folder: true, ContentVersion: epoch, MetadataVersion: epoch}, nil
	}
	for _, i := range folders(epoch) {
		if id == i.ID {
			return i, nil
		}
	}
	if id == "tags" {
		return Item{ID: "tags", Parent: "root", Name: "Tags", MIME: "inode/directory", Folder: true, ContentVersion: epoch, MetadataVersion: epoch}, nil
	}
	if strings.HasPrefix(id, "tag:") {
		items, err := tagFolders(ctx, tx, epoch)
		if err != nil {
			return Item{}, err
		}
		item, found := items[id]
		if !found {
			return Item{}, ErrNoSuchItem
		}
		return item, nil
	}
	return fileItem(ctx, tx, id, epoch)
}

func resolveUUID(ctx context.Context, tx *sql.Tx, uuid string) (int64, error) {
	var clipID int64
	err := tx.QueryRowContext(ctx, `SELECT clip_id FROM fp_sources WHERE uuid=?`, uuid).Scan(&clipID)
	if errors.Is(err, sql.ErrNoRows) {
		err = ErrNoSuchItem
	}
	return clipID, err
}

func fileItem(ctx context.Context, tx *sql.Tx, id, epoch string) (Item, error) {
	uuid, tagID, tagged, ok := parseFileID(id, epoch)
	if !ok {
		return Item{}, ErrNoSuchItem
	}
	clipID, err := resolveUUID(ctx, tx, uuid)
	if err != nil {
		return Item{}, err
	}
	item, _, err := sourceRecord(ctx, tx, clipID)
	if err != nil {
		return Item{}, err
	}
	if tagged {
		var assigned bool
		if err = tx.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM clip_tags ct JOIN tags t ON t.id=ct.tag_id WHERE ct.clip_id=? AND ct.tag_id=?)`, clipID, tagID).Scan(&assigned); err != nil {
			return Item{}, err
		}
		if !assigned {
			return Item{}, ErrNoSuchItem
		}
		item.ID = taggedItemID(epoch, uuid, tagID)
		item.Parent = tagFolderID(epoch, tagID)
		return item, nil
	}
	var shown bool
	if err = tx.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM clips c WHERE c.id=? AND `+visible+`)`, clipID).Scan(&shown); err != nil {
		return Item{}, err
	}
	if !shown {
		return Item{}, ErrNoSuchItem
	}
	item.ID = epoch + ":" + uuid
	return item, nil
}

// Content calls begin only after checking the current projection
// visibility/membership and version in the SAME WAL snapshot used for every
// blob chunk. begin can set HTTP headers; a short read is an error and the
// caller must abort the response (never mark it complete).
func (s *Store) Content(ctx context.Context, id, version string, begin func(Item) (io.Writer, error)) error {
	tx, err := s.db.BeginTx(ctx, &sql.TxOptions{ReadOnly: true})
	if err != nil {
		return err
	}
	defer tx.Rollback()
	epoch, _, err := state(ctx, tx)
	if err != nil {
		return err
	}
	uuid, _, _, ok := parseFileID(id, epoch)
	if !ok {
		return ErrNoSuchItem
	}
	clipID, err := resolveUUID(ctx, tx, uuid)
	if err != nil {
		return err
	}
	i, err := fileItem(ctx, tx, id, epoch)
	if err != nil {
		return err
	}
	if version != "" && version != i.ContentVersion {
		return ErrVersion
	}
	w, err := begin(i)
	if err != nil {
		return err
	}
	for offset := int64(0); offset < i.Size; {
		var b []byte
		n := min(int64(1<<20), i.Size-offset)
		if err = tx.QueryRowContext(ctx, `SELECT substr(data,?,?) FROM clips WHERE id=?`, offset+1, n, clipID).Scan(&b); err != nil {
			return err
		}
		if int64(len(b)) != n {
			return io.ErrUnexpectedEOF
		}
		written, e := w.Write(b)
		if e != nil {
			return e
		}
		if written != len(b) {
			return io.ErrShortWrite
		}
		offset += n
	}
	return nil
}

func (s *Store) Anchor(ctx context.Context, scope string) (string, error) {
	if !validScope(scope) {
		return "", ErrNoSuchItem
	}
	if _, err := s.Sync(ctx); err != nil {
		return "", err
	}
	tx, err := s.db.BeginTx(ctx, &sql.TxOptions{ReadOnly: true})
	if err != nil {
		return "", err
	}
	defer tx.Rollback()
	e, h, err := state(ctx, tx)
	if err != nil {
		return "", err
	}
	valid, err := validStoreScope(ctx, tx, scope, e)
	if err != nil {
		return "", err
	}
	if !valid {
		return "", ErrNoSuchItem
	}
	return token(cursor{Epoch: e, Scope: scope, Sequence: h}), nil
}

func validStoreScope(ctx context.Context, tx *sql.Tx, scope, epoch string) (bool, error) {
	switch scope {
	case "root", "active", "archive", "tags", "working":
		return true, nil
	}
	if !strings.HasPrefix(scope, "tag:") {
		return false, nil
	}
	items, err := tagFolders(ctx, tx, epoch)
	if err != nil {
		return false, err
	}
	_, exists := items[scope]
	return exists, nil
}

func (s *Store) Enumerate(ctx context.Context, scope, pageToken string) (Page, error) {
	p := Page{Items: []Item{}, Deleted: []string{}}
	if !validScope(scope) {
		return p, ErrNoSuchItem
	}
	if _, err := s.Sync(ctx); err != nil {
		return p, err
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return p, err
	}
	defer tx.Rollback()
	if _, err = tx.ExecContext(ctx, `UPDATE fp_state SET version=version`); err != nil {
		return p, err
	}
	epoch, high, err := state(ctx, tx)
	if err != nil {
		return p, err
	}
	valid, err := validStoreScope(ctx, tx, scope, epoch)
	if err != nil {
		return p, err
	}
	if !valid {
		return p, ErrNoSuchItem
	}
	c := cursor{Epoch: epoch, Scope: scope, Sequence: high}
	if pageToken == "" {
		// A malfunctioning client must not grow metadata snapshots without bound.
		if _, err = tx.ExecContext(ctx, `DELETE FROM fp_snapshot_items WHERE snapshot IN (SELECT id FROM fp_snapshots ORDER BY expires DESC,id DESC LIMIT -1 OFFSET 63); DELETE FROM fp_snapshots WHERE id IN (SELECT id FROM fp_snapshots ORDER BY expires DESC,id DESC LIMIT -1 OFFSET 63)`); err != nil {
			return p, err
		}
		if err = tx.QueryRowContext(ctx, `SELECT lower(hex(randomblob(16)))`).Scan(&c.Snapshot); err != nil {
			return p, err
		}
		if _, err = tx.ExecContext(ctx, `INSERT INTO fp_snapshots VALUES(?,?,?,?,unixepoch()+600)`, c.Snapshot, epoch, scope, high); err != nil {
			return p, err
		}
		if scope == "root" || scope == "working" {
			for n, i := range folders(epoch) {
				b, _ := json.Marshal(i)
				if _, err = tx.ExecContext(ctx, `INSERT INTO fp_snapshot_items VALUES(?,?,?)`, c.Snapshot, n, string(b)); err != nil {
					return p, err
				}
			}
		}
		if scope != "root" {
			// The working set drives background Finder updates for the entire
			// projection, including tag folders and aliases. Enumerate parents
			// before children, even across snapshot page boundaries.
			if _, err = tx.ExecContext(ctx, `WITH RECURSIVE tree(id,parent,item,depth) AS (
 SELECT id,parent,item,0 FROM fp_items WHERE parent IN ('active','archive','tags')
 UNION ALL
 SELECT i.id,i.parent,i.item,t.depth+1 FROM fp_items i JOIN tree t ON i.parent=t.id
) INSERT INTO fp_snapshot_items SELECT ?,row_number() OVER(ORDER BY depth,id)+?,item FROM tree WHERE ?='working' OR (?!='working' AND parent=?)`, c.Snapshot, len(folders(epoch)), scope, scope, scope); err != nil {
				return p, err
			}
		}
	} else {
		c, err = parseToken(pageToken, ErrPage)
		if err != nil {
			return p, err
		}
		if c.Epoch != epoch || c.Scope != scope || c.Snapshot == "" {
			return p, ErrPage
		}
		var valid int
		if err = tx.QueryRowContext(ctx, `SELECT count(*) FROM fp_snapshots WHERE id=? AND epoch=? AND scope=? AND high=? AND expires>=unixepoch()`, c.Snapshot, epoch, scope, c.Sequence).Scan(&valid); err != nil {
			return p, err
		}
		if valid != 1 {
			return p, ErrPage
		}
	}
	rows, err := tx.QueryContext(ctx, `SELECT ordinal,item FROM fp_snapshot_items WHERE snapshot=? AND ordinal>=? ORDER BY ordinal LIMIT ?`, c.Snapshot, c.Offset, PageSize+1)
	if err != nil {
		return p, err
	}
	for rows.Next() {
		var ordinal int64
		var b string
		if err = rows.Scan(&ordinal, &b); err != nil {
			break
		}
		if len(p.Items) == PageSize {
			c.Offset = ordinal
			p.Next = token(c)
			break
		}
		var i Item
		if err = json.Unmarshal([]byte(b), &i); err != nil {
			break
		}
		p.Items = append(p.Items, i)
	}
	if err == nil {
		err = rows.Err()
	}
	rows.Close()
	if err != nil {
		return p, err
	}
	p.Anchor = token(cursor{Epoch: epoch, Scope: scope, Sequence: c.Sequence})
	return p, tx.Commit()
}

func (s *Store) Changes(ctx context.Context, scope, anchor string) (Page, error) {
	p := Page{Items: []Item{}, Deleted: []string{}}
	c, err := parseToken(anchor, ErrAnchor)
	if err != nil {
		return p, err
	}
	if !validScope(scope) || c.Scope != scope || c.Snapshot != "" {
		return p, ErrAnchor
	}
	if _, err = s.Sync(ctx); err != nil {
		return p, err
	}
	tx, err := s.db.BeginTx(ctx, &sql.TxOptions{ReadOnly: true})
	if err != nil {
		return p, err
	}
	defer tx.Rollback()
	epoch, high, err := state(ctx, tx)
	if err != nil {
		return p, err
	}
	valid, err := validStoreScope(ctx, tx, scope, epoch)
	if err != nil {
		return p, err
	}
	if !valid {
		return p, ErrAnchor
	}
	var low int64
	if err = tx.QueryRowContext(ctx, `SELECT COALESCE(MIN(seq),0) FROM fp_changes`).Scan(&low); err != nil {
		return p, err
	}
	if c.Epoch != epoch || c.Sequence > high || c.Sequence < low-1 || c.High > high || c.High != 0 && c.High < c.Sequence {
		return p, ErrAnchor
	}
	if c.High == 0 {
		c.High = high
	}
	rows, err := tx.QueryContext(ctx, `SELECT seq,id,old_parent,parent,item FROM fp_changes WHERE seq>? AND seq<=? ORDER BY seq LIMIT ?`, c.Sequence, c.High, PageSize+1)
	if err != nil {
		return p, err
	}
	// Coalesce repeated IDs within this page so observers never receive both
	// update and deletion for the same item in a single callback batch.
	updates := map[string]Item{}
	deletes := map[string]bool{}
	deleteParents := map[string]string{}
	count := 0
	for rows.Next() {
		var seq int64
		var id, old, parent string
		var raw sql.NullString
		if err = rows.Scan(&seq, &id, &old, &parent, &raw); err != nil {
			break
		}
		if count == PageSize {
			p.More = true
			break
		}
		count++
		c.Sequence = seq
		if raw.Valid && (scope == parent || scope == "working") {
			var i Item
			if err = json.Unmarshal([]byte(raw.String), &i); err != nil {
				break
			}
			updates[id] = i
			delete(deletes, id)
			delete(deleteParents, id)
		} else if scope == old || scope == "working" {
			deletes[id] = true
			deleteParents[id] = old
			delete(updates, id)
		}
	}
	if err == nil {
		err = rows.Err()
	}
	rows.Close()
	if err != nil {
		return p, err
	}
	for _, id := range treeOrder(updates, false) {
		p.Items = append(p.Items, updates[id])
	}
	deletedItems := make(map[string]Item, len(deletes))
	for id := range deletes {
		deletedItems[id] = Item{ID: id, Parent: deleteParents[id]}
	}
	for _, id := range treeOrder(deletedItems, true) {
		p.Deleted = append(p.Deleted, id)
	}
	if !p.More {
		c.Sequence = c.High
		c.High = 0
	}
	p.Anchor = token(c)
	return p, nil
}
