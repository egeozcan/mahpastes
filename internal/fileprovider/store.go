package fileprovider

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"io"
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

func Open(ctx context.Context, db *sql.DB) (*Store, error) {
	if err := initialize(ctx, db); err != nil {
		return nil, err
	}
	s := &Store{db: db}
	_, err := s.Sync(ctx)
	return s, err
}

// visibility is evaluated in every content read, including the seconds between
// an expiry/hidden-tag change and the projection worker processing it.
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
	for _, name := range []string{"Active", "Archive"} {
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
	if _, err = tx.ExecContext(ctx, `INSERT OR IGNORE INTO fp_dirty SELECT c.id FROM clips c JOIN fp_items p ON p.clip_id=c.id WHERE c.expires_at IS NOT NULL AND julianday(c.expires_at)<=julianday('now')`); err != nil {
		return 0, err
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
		var old Item
		var raw string
		err = tx.QueryRowContext(ctx, `SELECT item FROM fp_items WHERE clip_id=?`, id).Scan(&raw)
		if err != nil && !errors.Is(err, sql.ErrNoRows) {
			return 0, err
		}
		if err == nil {
			if err = json.Unmarshal([]byte(raw), &old); err != nil {
				return 0, err
			}
		}
		item, e := sourceItem(ctx, tx, id, epoch)
		if e != nil && !errors.Is(e, ErrNoSuchItem) {
			return 0, e
		}
		if old.ID != "" && (e != nil || old.ID != item.ID) {
			if _, err = tx.ExecContext(ctx, `INSERT INTO fp_changes(id,old_parent,parent,item) VALUES(?,?, '',NULL)`, old.ID, old.Parent); err != nil {
				return 0, err
			}
			if _, err = tx.ExecContext(ctx, `DELETE FROM fp_items WHERE clip_id=?`, id); err != nil {
				return 0, err
			}
			old = Item{}
		}
		if e == nil && item != old {
			b, e := json.Marshal(item)
			if e != nil {
				return 0, e
			}
			if _, err = tx.ExecContext(ctx, `INSERT INTO fp_items(id,clip_id,parent,item) VALUES(?,?,?,?) ON CONFLICT(id) DO UPDATE SET parent=excluded.parent,item=excluded.item`, item.ID, id, item.Parent, string(b)); err != nil {
				return 0, err
			}
			if _, err = tx.ExecContext(ctx, `INSERT INTO fp_changes(id,old_parent,parent,item) VALUES(?,?,?,?)`, item.ID, old.Parent, item.Parent, string(b)); err != nil {
				return 0, err
			}
		}
		if _, err = tx.ExecContext(ctx, `DELETE FROM fp_dirty WHERE clip_id=?`, id); err != nil {
			return 0, err
		}
	}
	// Bound metadata caches and journal growth. Old cursors explicitly expire.
	if _, err = tx.ExecContext(ctx, `DELETE FROM fp_snapshot_items WHERE snapshot IN (SELECT id FROM fp_snapshots WHERE expires<unixepoch()); DELETE FROM fp_snapshots WHERE expires<unixepoch(); DELETE FROM fp_changes WHERE seq<(SELECT COALESCE(MAX(seq),0)-100000 FROM fp_changes) OR (created<unixepoch()-2592000 AND seq<(SELECT MAX(seq) FROM fp_changes))`); err != nil {
		return 0, err
	}
	return len(ids), tx.Commit()
}

func sourceItem(ctx context.Context, tx *sql.Tx, id int64, epoch string) (Item, error) {
	var i Item
	var uuid, name string
	var content, metadata int64
	var archived bool
	err := tx.QueryRowContext(ctx, `SELECT s.uuid,s.content_rev,s.metadata_rev,s.modified,COALESCE(c.filename,''),c.content_type,length(c.data),strftime('%Y-%m-%dT%H:%M:%fZ',c.created_at),c.is_archived FROM clips c JOIN fp_sources s ON s.clip_id=c.id WHERE c.id=? AND `+visible, id).Scan(&uuid, &content, &metadata, &i.Modified, &name, &i.MIME, &i.Size, &i.Created, &archived)
	if errors.Is(err, sql.ErrNoRows) {
		return i, ErrNoSuchItem
	}
	if err != nil {
		return i, err
	}
	i.ID = epoch + ":" + uuid
	i.Parent = "active"
	if archived {
		i.Parent = "archive"
	}
	i.Name = Filename(name, i.MIME, uuid)
	i.ContentVersion = strconv.FormatInt(content, 10)
	i.MetadataVersion = strconv.FormatInt(metadata, 10)
	return i, nil
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
	clipID, err := resolve(ctx, tx, id, epoch)
	if err != nil {
		return Item{}, err
	}
	return sourceItem(ctx, tx, clipID, epoch)
}

func resolve(ctx context.Context, tx *sql.Tx, id, epoch string) (int64, error) {
	parts := strings.Split(id, ":")
	if len(parts) != 2 || parts[0] != epoch {
		return 0, ErrNoSuchItem
	}
	var clipID int64
	err := tx.QueryRowContext(ctx, `SELECT clip_id FROM fp_sources WHERE uuid=?`, parts[1]).Scan(&clipID)
	if errors.Is(err, sql.ErrNoRows) {
		err = ErrNoSuchItem
	}
	return clipID, err
}

// Content calls begin only after checking visibility/version in the SAME WAL
// snapshot used for every blob chunk. begin can set HTTP headers; a short read
// is an error and the caller must abort the response (never mark it complete).
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
	clipID, err := resolve(ctx, tx, id, epoch)
	if err != nil {
		return err
	}
	i, err := sourceItem(ctx, tx, clipID, epoch)
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
	return token(cursor{Epoch: e, Scope: scope, Sequence: h}), err
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
			if _, err = tx.ExecContext(ctx, `INSERT INTO fp_snapshot_items SELECT ?,row_number() OVER(ORDER BY id)+2,item FROM fp_items WHERE ?='working' OR parent=?`, c.Snapshot, scope, scope); err != nil {
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
		if raw.Valid && (scope == "working" || scope == parent) {
			var i Item
			if err = json.Unmarshal([]byte(raw.String), &i); err != nil {
				break
			}
			updates[id] = i
			delete(deletes, id)
		} else if scope == "working" || scope == old {
			deletes[id] = true
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
	for _, i := range updates {
		p.Items = append(p.Items, i)
	}
	for id := range deletes {
		p.Deleted = append(p.Deleted, id)
	}
	if !p.More {
		c.Sequence = c.High
		c.High = 0
	}
	p.Anchor = token(c)
	return p, nil
}
