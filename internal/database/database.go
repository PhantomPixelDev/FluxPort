package database

import (
	"database/sql"
	"fmt"
	"os"
	"path/filepath"

	_ "modernc.org/sqlite"
)

func Open(dir string) (*sql.DB, error) {
	if err := os.MkdirAll(dir, 0700); err != nil {
		return nil, err
	}
	db, err := sql.Open("sqlite", filepath.Join(dir, "metadata.db"))
	if err != nil {
		return nil, err
	}
	db.SetMaxOpenConns(1)
	for _, query := range []string{
		`PRAGMA journal_mode=WAL`, `PRAGMA busy_timeout=5000`,
		`CREATE TABLE IF NOT EXISTS collections (id TEXT PRIMARY KEY, path TEXT NOT NULL UNIQUE, name TEXT NOT NULL, format TEXT NOT NULL, size INTEGER NOT NULL, rows INTEGER NOT NULL, updated TEXT NOT NULL, enabled INTEGER NOT NULL DEFAULT 1, crud INTEGER NOT NULL DEFAULT 0, rules TEXT NOT NULL DEFAULT '', error TEXT NOT NULL DEFAULT '')`,
		`CREATE TABLE IF NOT EXISTS settings (key TEXT PRIMARY KEY, value TEXT NOT NULL)`,
	} {
		if _, err = db.Exec(query); err != nil {
			db.Close()
			return nil, fmt.Errorf("database migration: %w", err)
		}
	}
	if err = migrate(db); err != nil {
		db.Close()
		return nil, fmt.Errorf("database migration: %w", err)
	}
	return db, nil
}
