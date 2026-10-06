package fileprovider

import (
	"context"
	"database/sql"
	"encoding/json"
	"log"
	"time"
)

// List returns the current children of a container straight from the
// projection. Unlike Enumerate it writes no snapshot rows, so filesystem
// clients (FUSE lookups and directory reads) can call it as often as they
// like. It does not catch up with the dirty queue; Watch does that every
// second.
func (s *Store) List(ctx context.Context, parent string) ([]Item, error) {
	if !validScope(parent) || parent == "working" {
		return nil, ErrNoSuchItem
	}
	tx, err := s.db.BeginTx(ctx, &sql.TxOptions{ReadOnly: true})
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()
	epoch, _, err := state(ctx, tx)
	if err != nil {
		return nil, err
	}
	if parent == "root" {
		return folders(epoch), nil
	}
	valid, err := validStoreScope(ctx, tx, parent, epoch)
	if err != nil {
		return nil, err
	}
	if !valid {
		return nil, ErrNoSuchItem
	}
	rows, err := tx.QueryContext(ctx, `SELECT item FROM fp_items WHERE parent=? ORDER BY id`, parent)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	items := []Item{}
	for rows.Next() {
		var raw string
		if err = rows.Scan(&raw); err != nil {
			return nil, err
		}
		var i Item
		if err = json.Unmarshal([]byte(raw), &i); err != nil {
			return nil, err
		}
		items = append(items, i)
	}
	return items, rows.Err()
}

// Watch keeps the projection current until ctx ends, polling the dirty queue
// once per second. signal, when set, runs after each observed journal change.
// The returned channel closes when the worker has stopped.
func Watch(ctx context.Context, store *Store, signal func() error) <-chan struct{} {
	done := make(chan struct{})
	go func() {
		defer close(done)
		ticker := time.NewTicker(time.Second)
		defer ticker.Stop()
		lastHead := ""
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				_, err := store.Sync(ctx)
				if err != nil {
					if ctx.Err() == nil {
						log.Printf("File Provider projection: %v", err)
					}
					continue
				}
				head, err := store.Head(ctx)
				if err != nil {
					continue
				}
				if head != lastHead && signal != nil {
					if err := signal(); err != nil {
						if ctx.Err() == nil {
							log.Printf("File Provider notification: %v", err)
						}
						continue
					}
				}
				lastHead = head
			}
		}
	}()
	return done
}
