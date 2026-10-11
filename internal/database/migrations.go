package database

import (
	"database/sql"
	"fmt"
)

func migrate(db *sql.DB) error {
	var version int
	if err := db.QueryRow(`PRAGMA user_version`).Scan(&version); err != nil {
		return err
	}
	if version > 3 {
		return fmt.Errorf("metadata schema %d is newer than supported schema 3", version)
	}
	if version == 3 {
		return nil
	}
	tx, err := db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	queries := []string{
		`ALTER TABLE collections ADD COLUMN display_name TEXT NOT NULL DEFAULT ''`,
		`ALTER TABLE collections ADD COLUMN status TEXT NOT NULL DEFAULT 'pending'`,
		`ALTER TABLE collections ADD COLUMN source_stamp TEXT NOT NULL DEFAULT ''`,
		`ALTER TABLE collections ADD COLUMN generation TEXT NOT NULL DEFAULT ''`,
		`ALTER TABLE collections ADD COLUMN job_id TEXT NOT NULL DEFAULT ''`,
		`ALTER TABLE collections ADD COLUMN schema_json TEXT NOT NULL DEFAULT '[]'`,
		`CREATE TABLE exclusions (path TEXT PRIMARY KEY,name TEXT NOT NULL,removed TEXT NOT NULL)`,
		`CREATE TABLE jobs (id TEXT PRIMARY KEY,kind TEXT NOT NULL,resource TEXT NOT NULL,name TEXT NOT NULL,state TEXT NOT NULL,payload TEXT NOT NULL,processed INTEGER NOT NULL DEFAULT 0,total INTEGER NOT NULL DEFAULT 0,records INTEGER NOT NULL DEFAULT 0,error TEXT NOT NULL DEFAULT '',result TEXT NOT NULL DEFAULT '',created TEXT NOT NULL,updated TEXT NOT NULL)`,
		`CREATE INDEX jobs_state ON jobs(state,created)`,
		`PRAGMA user_version=2`,
	}
	if version == 2 {
		queries = nil
	}
	queries = append(queries, `ALTER TABLE collections ADD COLUMN source_sha256 TEXT NOT NULL DEFAULT ''`, `ALTER TABLE collections ADD COLUMN index_limits TEXT NOT NULL DEFAULT ''`, `PRAGMA user_version=3`)
	for _, q := range queries {
		if _, err = tx.Exec(q); err != nil {
			return err
		}
	}
	return tx.Commit()
}
