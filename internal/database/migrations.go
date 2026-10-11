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
	if version > 9 {
		return fmt.Errorf("metadata schema %d is newer than supported schema 9", version)
	}
	if version == 9 {
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
	if version >= 2 {
		queries = nil
	}
	if version < 3 {
		queries = append(queries, `ALTER TABLE collections ADD COLUMN source_sha256 TEXT NOT NULL DEFAULT ''`, `ALTER TABLE collections ADD COLUMN index_limits TEXT NOT NULL DEFAULT ''`, `PRAGMA user_version=3`)
	}
	if version < 4 {
		queries = append(queries,
			`CREATE TABLE saved_views (id TEXT PRIMARY KEY,collection_id TEXT NOT NULL,name TEXT NOT NULL,query_json TEXT NOT NULL,columns_json TEXT NOT NULL DEFAULT '[]',updated TEXT NOT NULL,UNIQUE(collection_id,name))`,
			`CREATE INDEX saved_views_collection ON saved_views(collection_id)`,
			`CREATE TABLE record_overrides (collection_id TEXT NOT NULL,source_sha256 TEXT NOT NULL,ordinal INTEGER NOT NULL,record_id TEXT NOT NULL,PRIMARY KEY(collection_id,source_sha256,ordinal))`,
			`PRAGMA user_version=4`)
	}
	if version < 5 {
		queries = append(queries,
			`CREATE TABLE sql_queries (id TEXT PRIMARY KEY,name TEXT NOT NULL UNIQUE,sql_text TEXT NOT NULL,collections_json TEXT NOT NULL,published INTEGER NOT NULL DEFAULT 0,updated TEXT NOT NULL)`,
			`CREATE TABLE sql_history (id TEXT PRIMARY KEY,sql_text TEXT NOT NULL,collections_json TEXT NOT NULL,error TEXT NOT NULL,duration_ms INTEGER NOT NULL,created TEXT NOT NULL)`,
			`PRAGMA user_version=5`)
	}
	if version < 6 {
		queries = append(queries, `CREATE TABLE pipelines (id TEXT PRIMARY KEY,name TEXT NOT NULL UNIQUE,definition_json TEXT NOT NULL,updated TEXT NOT NULL)`, `PRAGMA user_version=6`)
	}
	if version < 7 {
		queries = append(queries, `CREATE TABLE validation_rules (collection_id TEXT PRIMARY KEY,rules_json TEXT NOT NULL)`, `PRAGMA user_version=7`)
	}
	if version < 8 {
		queries = append(queries, `CREATE TABLE pipeline_revisions(pipeline_id TEXT NOT NULL,revision INTEGER NOT NULL,definition_json TEXT NOT NULL,created TEXT NOT NULL,PRIMARY KEY(pipeline_id,revision))`, `INSERT INTO pipeline_revisions SELECT id,1,definition_json,updated FROM pipelines`, `PRAGMA user_version=8`)
	}
	queries = append(queries, `CREATE TABLE pipeline_schedules(pipeline_id TEXT PRIMARY KEY,interval_seconds INTEGER NOT NULL,format TEXT NOT NULL,enabled INTEGER NOT NULL,next_run INTEGER NOT NULL,last_job TEXT NOT NULL DEFAULT '',error TEXT NOT NULL DEFAULT '')`, `PRAGMA user_version=9`)
	for _, q := range queries {
		if _, err = tx.Exec(q); err != nil {
			return err
		}
	}
	return tx.Commit()
}
