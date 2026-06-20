package indexstate

import (
	"context"
	"database/sql"
	"fmt"
	"os"
	"path/filepath"
	"time"

	_ "modernc.org/sqlite"
)

type Store struct {
	db *sql.DB
}

type FileState struct {
	Path string
	Hash string
}

func Open(root string) (*Store, error) {
	dir := filepath.Join(root, ".adex")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return nil, fmt.Errorf("create index state dir: %w", err)
	}
	db, err := sql.Open("sqlite", filepath.Join(dir, "index.sqlite"))
	if err != nil {
		return nil, fmt.Errorf("open index state: %w", err)
	}
	store := &Store{db: db}
	if err := store.init(context.Background()); err != nil {
		db.Close()
		return nil, err
	}
	return store, nil
}

func (s *Store) Close() error {
	if s == nil || s.db == nil {
		return nil
	}
	return s.db.Close()
}

func (s *Store) Load(ctx context.Context) (map[string]string, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT path, hash FROM files`)
	if err != nil {
		return nil, fmt.Errorf("load index state: %w", err)
	}
	defer rows.Close()
	state := map[string]string{}
	for rows.Next() {
		var path, hash string
		if err := rows.Scan(&path, &hash); err != nil {
			return nil, fmt.Errorf("scan index state: %w", err)
		}
		state[path] = hash
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("read index state: %w", err)
	}
	return state, nil
}

func (s *Store) Replace(ctx context.Context, files []FileState) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("begin index state replace: %w", err)
	}
	defer tx.Rollback()
	if _, err := tx.ExecContext(ctx, `DELETE FROM files`); err != nil {
		return fmt.Errorf("clear index state: %w", err)
	}
	stmt, err := tx.PrepareContext(ctx, `INSERT INTO files(path, hash, indexed_at) VALUES (?, ?, ?)`)
	if err != nil {
		return fmt.Errorf("prepare index state replace: %w", err)
	}
	defer stmt.Close()
	now := time.Now().UTC().Format(time.RFC3339Nano)
	for _, file := range files {
		if _, err := stmt.ExecContext(ctx, file.Path, file.Hash, now); err != nil {
			return fmt.Errorf("write index state: %w", err)
		}
	}
	return tx.Commit()
}

func (s *Store) Apply(ctx context.Context, changed []FileState, deleted []string) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("begin index state update: %w", err)
	}
	defer tx.Rollback()
	for _, path := range deleted {
		if _, err := tx.ExecContext(ctx, `DELETE FROM files WHERE path = ?`, path); err != nil {
			return fmt.Errorf("delete index state: %w", err)
		}
	}
	stmt, err := tx.PrepareContext(ctx, `
INSERT INTO files(path, hash, indexed_at) VALUES (?, ?, ?)
ON CONFLICT(path) DO UPDATE SET hash = excluded.hash, indexed_at = excluded.indexed_at
`)
	if err != nil {
		return fmt.Errorf("prepare index state update: %w", err)
	}
	defer stmt.Close()
	now := time.Now().UTC().Format(time.RFC3339Nano)
	for _, file := range changed {
		if _, err := stmt.ExecContext(ctx, file.Path, file.Hash, now); err != nil {
			return fmt.Errorf("update index state: %w", err)
		}
	}
	return tx.Commit()
}

func (s *Store) init(ctx context.Context) error {
	_, err := s.db.ExecContext(ctx, `
CREATE TABLE IF NOT EXISTS files (
	path TEXT PRIMARY KEY,
	hash TEXT NOT NULL,
	indexed_at TEXT NOT NULL
);
`)
	if err != nil {
		return fmt.Errorf("init index state: %w", err)
	}
	return nil
}
