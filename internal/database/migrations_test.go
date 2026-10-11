package database

import (
	"database/sql"
	"path/filepath"
	"testing"
)

func TestVersionOnePreservesCollection(t *testing.T) {
	dir := t.TempDir()
	db, e := sql.Open("sqlite", filepath.Join(dir, "metadata.db"))
	if e != nil {
		t.Fatal(e)
	}
	_, e = db.Exec(`CREATE TABLE collections (id TEXT PRIMARY KEY,path TEXT NOT NULL UNIQUE,name TEXT NOT NULL,format TEXT NOT NULL,size INTEGER NOT NULL,rows INTEGER NOT NULL,updated TEXT NOT NULL,enabled INTEGER NOT NULL DEFAULT 1,crud INTEGER NOT NULL DEFAULT 0,rules TEXT NOT NULL DEFAULT '',error TEXT NOT NULL DEFAULT ''); INSERT INTO collections(id,path,name,format,size,rows,updated,rules) VALUES('existing-id','source.json','source','json',2,0,'2024','{"select":["id"]}'); PRAGMA user_version=1`)
	if e != nil {
		t.Fatal(e)
	}
	db.Close()
	db, e = Open(dir)
	if e != nil {
		t.Fatal(e)
	}
	defer db.Close()
	var id, rules, status string
	var version int
	if e = db.QueryRow(`SELECT id,rules,status FROM collections`).Scan(&id, &rules, &status); e != nil {
		t.Fatal(e)
	}
	db.QueryRow(`PRAGMA user_version`).Scan(&version)
	if id != "existing-id" || rules != `{"select":["id"]}` || status != "pending" || version != 9 {
		t.Fatal(id, rules, status, version)
	}
	if e = migrate(db); e != nil {
		t.Fatal("migration not idempotent", e)
	}
}
