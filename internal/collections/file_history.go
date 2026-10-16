package collections

import (
	"bytes"
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"time"
)

const fileHistoryCount = 20
const fileHistoryBytes int64 = 64 << 20

type FileRevision struct {
	Revision int64  `json:"revision"`
	Label    string `json:"label"`
	Created  string `json:"created"`
	Size     int64  `json:"size"`
	Checksum string `json:"checksum"`
	Format   string `json:"format"`
	snapshot string
	ids      []string
}
type FileHistory struct {
	Current   int64          `json:"current"`
	Previous  int64          `json:"previous"`
	Next      int64          `json:"next"`
	Stale     bool           `json:"stale"`
	Revisions []FileRevision `json:"revisions"`
}

func (s *Service) deleteFileHistory(id string) error {
	tx, err := s.DB.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if _, err = tx.Exec("DELETE FROM file_history WHERE collection_id=?", id); err != nil {
		return err
	}
	if _, err = tx.Exec("DELETE FROM file_revisions WHERE collection_id=?", id); err != nil {
		return err
	}
	if err = tx.Commit(); err != nil {
		return err
	}
	entries, err := os.ReadDir(s.historyFolder(id))
	if os.IsNotExist(err) {
		return nil
	}
	if err != nil {
		return err
	}
	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}
		path, e := s.historyPath(id, entry.Name())
		if e != nil {
			continue
		}
		if e = os.Remove(path); e != nil && !os.IsNotExist(e) {
			return e
		}
	}
	return os.Remove(s.historyFolder(id))
}

func fileDigest(b []byte) string {
	sum := sha256.Sum256(b)
	return hex.EncodeToString(sum[:])
}
func (s *Service) historyFolder(id string) string {
	sum := sha256.Sum256([]byte(id))
	return filepath.Join(s.HistoryDir, hex.EncodeToString(sum[:]))
}
func (s *Service) historyPath(id, name string) (string, error) {
	if filepath.Base(name) != name || len(name) != 39 || filepath.Ext(name) != ".source" {
		return "", fmt.Errorf("invalid file revision")
	}
	if _, err := hex.DecodeString(name[:32]); err != nil {
		return "", fmt.Errorf("invalid file revision")
	}
	return filepath.Join(s.historyFolder(id), name), nil
}
func (s *Service) fileRevision(id string, revision int64) (FileRevision, error) {
	var r FileRevision
	var ids string
	err := s.DB.QueryRow("SELECT revision,label,created,size,source_sha256,format,snapshot,ids_json FROM file_revisions WHERE collection_id=? AND revision=?", id, revision).Scan(&r.Revision, &r.Label, &r.Created, &r.Size, &r.Checksum, &r.Format, &r.snapshot, &ids)
	if err != nil {
		return r, err
	}
	err = json.Unmarshal([]byte(ids), &r.ids)
	return r, err
}
func (s *Service) insertFileRevision(c Collection, b []byte, ids []string, label, state string) (int64, error) {
	if err := os.MkdirAll(s.historyFolder(c.ID), 0700); err != nil {
		return 0, err
	}
	name := newRecordID() + ".source"
	path, err := s.historyPath(c.ID, name)
	if err != nil {
		return 0, err
	}
	if err = atomicWrite(path, b); err != nil {
		return 0, err
	}
	encoded, err := json.Marshal(ids)
	if err != nil {
		os.Remove(path)
		return 0, err
	}
	result, err := s.DB.Exec("INSERT INTO file_revisions(collection_id,source_sha256,format,snapshot,ids_json,size,label,created,state) VALUES(?,?,?,?,?,?,?,?,?)", c.ID, fileDigest(b), c.Format, name, string(encoded), len(b), label, time.Now().UTC().Format(time.RFC3339Nano), state)
	if err != nil {
		os.Remove(path)
		return 0, err
	}
	return result.LastInsertId()
}
func (s *Service) historyCursor(id string) (current, pending int64, isNew bool, err error) {
	var value int
	err = s.DB.QueryRow("SELECT current_revision,pending_revision,pending_new FROM file_history WHERE collection_id=?", id).Scan(&current, &pending, &value)
	if errors.Is(err, sql.ErrNoRows) {
		err = nil
	}
	return current, pending, value != 0, err
}

// A pending pointer is a durable publication journal. Before the source rename,
// the old bytes win; after it, the new bytes win. Recovery never rewrites a source.
func (s *Service) recoverFileWrite(c Collection) error {
	current, pending, isNew, err := s.historyCursor(c.ID)
	if err != nil || pending == 0 {
		return err
	}
	var writingJob string
	if err = s.DB.QueryRow("SELECT pending_job FROM file_history WHERE collection_id=?", c.ID).Scan(&writingJob); err != nil {
		return err
	}
	target, err := s.fileRevision(c.ID, pending)
	if err != nil {
		return err
	}
	actual, readErr := sourceChecksum(context.Background(), c.Path)
	success := readErr == nil && actual == target.Checksum
	var previous FileRevision
	if current != 0 {
		previous, err = s.fileRevision(c.ID, current)
		if err != nil {
			return err
		}
	}
	tx, err := s.DB.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	ids, checksum := previous.ids, previous.Checksum
	if success {
		ids, checksum = target.ids, target.Checksum
	}
	if _, err = tx.Exec("DELETE FROM record_overrides WHERE collection_id=?", c.ID); err != nil {
		return err
	}
	for i, id := range ids {
		if _, err = tx.Exec("INSERT INTO record_overrides(collection_id,source_sha256,ordinal,record_id) VALUES(?,?,?,?)", c.ID, checksum, i, id); err != nil {
			return err
		}
	}
	var removed []string
	if success {
		if isNew {
			rows, e := tx.Query("SELECT snapshot FROM file_revisions WHERE collection_id=? AND revision>? AND revision<>?", c.ID, current, pending)
			if e != nil {
				return e
			}
			for rows.Next() {
				var name string
				if e = rows.Scan(&name); e != nil {
					rows.Close()
					return e
				}
				removed = append(removed, name)
			}
			e = rows.Err()
			rows.Close()
			if e != nil {
				return e
			}
			if _, err = tx.Exec("DELETE FROM file_revisions WHERE collection_id=? AND revision>? AND revision<>?", c.ID, current, pending); err != nil {
				return err
			}
			if _, err = tx.Exec("UPDATE file_revisions SET state='ready' WHERE collection_id=? AND revision=?", c.ID, pending); err != nil {
				return err
			}
		}
		if _, err = tx.Exec("UPDATE file_history SET current_revision=?,pending_revision=0,pending_new=0,pending_job='' WHERE collection_id=?", pending, c.ID); err != nil {
			return err
		}
	} else {
		if isNew {
			if _, err = tx.Exec("DELETE FROM file_revisions WHERE collection_id=? AND revision=?", c.ID, pending); err != nil {
				return err
			}
			removed = append(removed, target.snapshot)
		}
		if _, err = tx.Exec("UPDATE file_history SET pending_revision=0,pending_new=0 WHERE collection_id=?", c.ID); err != nil {
			return err
		}
	}
	if success {
		if _, err = tx.Exec("UPDATE collections SET status='pending',index_limits='' WHERE id=?", c.ID); err != nil {
			return err
		}
		if writingJob != "" {
			if _, err = tx.Exec("UPDATE jobs SET state='ready',result='file-saved',processed=total,records=?,error='',updated=? WHERE id=? AND kind='file_cleanup' AND resource=?", len(target.ids), time.Now().UTC().Format(time.RFC3339), writingJob, c.ID); err != nil {
				return err
			}
		}
	}
	if err = tx.Commit(); err != nil {
		return err
	}
	for _, name := range removed {
		if path, e := s.historyPath(c.ID, name); e == nil {
			os.Remove(path)
		}
	}
	if success && isNew {
		return s.pruneFileHistory(c.ID)
	}
	return nil
}
func (s *Service) recoverFileWrites() error {
	rows, err := s.DB.Query("SELECT collection_id FROM file_history WHERE pending_revision<>0")
	if err != nil {
		return err
	}
	var ids []string
	for rows.Next() {
		var id string
		if err = rows.Scan(&id); err != nil {
			rows.Close()
			return err
		}
		ids = append(ids, id)
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return err
	}
	for _, id := range ids {
		c, e := s.Get(id)
		if errors.Is(e, sql.ErrNoRows) {
			continue
		}
		if e != nil {
			return e
		}
		unlock := s.lock(c.Path)
		e = s.recoverFileWrite(c)
		unlock()
		if e != nil {
			return e
		}
	}
	return nil
}
func (s *Service) pruneFileHistory(id string) error {
	rows, err := s.DB.Query("SELECT revision,snapshot,size FROM file_revisions WHERE collection_id=? AND state='ready' ORDER BY revision DESC", id)
	if err != nil {
		return err
	}
	type old struct {
		revision int64
		name     string
	}
	var remove []old
	var total int64
	count := 0
	for rows.Next() {
		var rev, size int64
		var name string
		if err = rows.Scan(&rev, &name, &size); err != nil {
			rows.Close()
			return err
		}
		if count > 0 && (count >= fileHistoryCount || total+size > fileHistoryBytes) {
			remove = append(remove, old{rev, name})
		} else {
			count++
			total += size
		}
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return err
	}
	for _, r := range remove {
		if _, err = s.DB.Exec("DELETE FROM file_revisions WHERE collection_id=? AND revision=?", id, r.revision); err != nil {
			return err
		}
		if path, e := s.historyPath(id, r.name); e == nil {
			os.Remove(path)
		}
	}
	return nil
}
func (s *Service) fileHistory(c Collection) (FileHistory, error) {
	out := FileHistory{Revisions: []FileRevision{}}
	current, _, _, err := s.historyCursor(c.ID)
	if err != nil {
		return out, err
	}
	out.Current = current
	if current == 0 {
		return out, nil
	}
	cursor, err := s.fileRevision(c.ID, current)
	if err != nil {
		return out, err
	}
	out.Stale = c.Status == "ready" && cursor.Checksum != c.SourceSHA
	if err = s.DB.QueryRow("SELECT COALESCE(MAX(revision),0) FROM file_revisions WHERE collection_id=? AND state='ready' AND revision<?", c.ID, current).Scan(&out.Previous); err != nil {
		return out, err
	}
	if err = s.DB.QueryRow("SELECT COALESCE(MIN(revision),0) FROM file_revisions WHERE collection_id=? AND state='ready' AND revision>?", c.ID, current).Scan(&out.Next); err != nil {
		return out, err
	}
	if out.Stale {
		out.Previous, out.Next = 0, 0
	}
	rows, err := s.DB.Query("SELECT revision,label,created,size,source_sha256,format FROM file_revisions WHERE collection_id=? AND state='ready' ORDER BY revision DESC LIMIT ?", c.ID, fileHistoryCount)
	if err != nil {
		return out, err
	}
	defer rows.Close()
	for rows.Next() {
		var r FileRevision
		if err = rows.Scan(&r.Revision, &r.Label, &r.Created, &r.Size, &r.Checksum, &r.Format); err != nil {
			return out, err
		}
		out.Revisions = append(out.Revisions, r)
	}
	return out, rows.Err()
}
func (s *Service) FileHistory(id string) (FileHistory, error) {
	c, err := s.Get(id)
	if err != nil {
		return FileHistory{}, err
	}
	return s.fileHistory(c)
}
func (s *Service) beginFileRevision(c Collection, before, after []byte, beforeIDs, afterIDs []string, label string, writingJob ...string) (int64, error) {
	if err := s.recoverFileWrite(c); err != nil {
		return 0, err
	}
	folder := s.historyFolder(c.ID)
	if err := os.MkdirAll(folder, 0700); err != nil {
		return 0, err
	}
	reserve := s.Reserve + uint64(len(before)) + 2*uint64(len(after))
	if err := s.DiskCheck(folder, reserve); err != nil {
		return 0, err
	}
	if err := s.DiskCheck(s.Root, reserve); err != nil {
		return 0, err
	}
	current, _, _, err := s.historyCursor(c.ID)
	if err != nil {
		return 0, err
	}
	baseline := current == 0
	if current != 0 {
		r, e := s.fileRevision(c.ID, current)
		if e != nil {
			return 0, e
		}
		baseline = r.Checksum != fileDigest(before)
	}
	if baseline {
		label := "File before first save"
		if current != 0 {
			label = "External file change"
		}
		revision, e := s.insertFileRevision(c, before, beforeIDs, label, "ready")
		if e != nil {
			return 0, e
		}
		if _, e = s.DB.Exec("INSERT INTO file_history(collection_id,current_revision) VALUES(?,?) ON CONFLICT(collection_id) DO UPDATE SET current_revision=excluded.current_revision", c.ID, revision); e != nil {
			return 0, e
		}
	}
	revision, err := s.insertFileRevision(c, after, afterIDs, label, "pending")
	if err != nil {
		return 0, err
	}
	jobID := ""
	if len(writingJob) > 0 {
		jobID = writingJob[0]
	}
	if _, err = s.DB.Exec("UPDATE file_history SET pending_revision=?,pending_new=1,pending_job=? WHERE collection_id=?", revision, jobID, c.ID); err != nil {
		return 0, err
	}
	return revision, nil
}
func atomicFileRevision(ctx context.Context, path string, b []byte, expected string) error {
	f, err := os.CreateTemp(filepath.Dir(path), ".fluxport-*")
	if err != nil {
		return err
	}
	tmp := f.Name()
	defer os.Remove(tmp)
	if _, err = f.Write(b); err != nil {
		f.Close()
		return err
	}
	if err = f.Sync(); err != nil {
		f.Close()
		return err
	}
	if err = f.Close(); err != nil {
		return err
	}
	if err = ctx.Err(); err != nil {
		return err
	}
	actual, err := sourceChecksum(ctx, path)
	if err != nil {
		return err
	}
	if actual != expected {
		return ErrConflict
	}
	return os.Rename(tmp, path)
}

func (s *Service) publishFileRevision(ctx context.Context, c Collection, before, after []byte, beforeIDs, afterIDs []string, label string, writingJob ...string) error {
	if err := s.validateFileOutput(c, after, len(afterIDs)); err != nil {
		return err
	}
	if bytes.Equal(before, after) {
		return nil
	}
	if _, err := s.beginFileRevision(c, before, after, beforeIDs, afterIDs, label, writingJob...); err != nil {
		return err
	}
	publish := func() error {
		if err := atomicFileRevision(ctx, c.Path, after, fileDigest(before)); err != nil {
			return err
		}
		return s.recoverFileWrite(c)
	}
	var err error
	if len(writingJob) > 0 && writingJob[0] != "" {
		err = s.Jobs.CommitFile(ctx, writingJob[0], publish)
	} else {
		err = publish()
	}
	if err != nil {
		if recovery := s.recoverFileWrite(c); recovery != nil {
			return recovery
		}
		return err
	}
	return s.syncFile(c.Path, true, writingJob...)
}
func (s *Service) RestoreFile(id, generation, direction string, revision int64, expectedCursor ...int64) error {
	c, err := s.Get(id)
	if err != nil {
		return err
	}
	unlock := s.lock(c.Path)
	defer unlock()
	c, err = s.Get(id)
	if err != nil {
		return err
	}
	if c.Status == "ready" {
		c, err = s.ready(c, true)
		if err != nil {
			return err
		}
	}
	// A native form can outlive a harmless index refresh. A supplied history
	// cursor plus the exact current snapshot checksum is an equally strong
	// precondition; actual source changes still conflict below.
	if (generation == "" || generation != c.Generation) && len(expectedCursor) == 0 {
		return ErrConflict
	}
	if c.ReadOnly || c.Size > s.MaxBytes {
		return fmt.Errorf("this file is read-only")
	}
	if err = s.recoverFileWrite(c); err != nil {
		return err
	}
	h, err := s.fileHistory(c)
	if err != nil {
		return err
	}
	if len(expectedCursor) > 0 && expectedCursor[0] != h.Current {
		return ErrConflict
	}
	switch direction {
	case "undo":
		revision = h.Previous
	case "redo":
		revision = h.Next
	case "restore":
	default:
		return fmt.Errorf("invalid history action")
	}
	if revision == 0 {
		return fmt.Errorf("no %s revision is available", direction)
	}
	target, err := s.fileRevision(id, revision)
	if err != nil {
		return err
	}
	if target.Format != c.Format || target.Size > s.MaxBytes {
		return fmt.Errorf("revision does not fit the current file format or editing limit")
	}
	path, err := s.historyPath(id, target.snapshot)
	if err != nil {
		return err
	}
	b, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	if int64(len(b)) != target.Size || fileDigest(b) != target.Checksum {
		return fmt.Errorf("revision snapshot is damaged; source left unchanged")
	}
	actual, err := sourceChecksum(context.Background(), c.Path)
	if err != nil {
		return err
	}
	expected := c.SourceSHA
	if c.Status != "ready" && h.Current != 0 {
		current, e := s.fileRevision(id, h.Current)
		if e != nil {
			return e
		}
		expected = current.Checksum
	}
	if len(expectedCursor) > 0 {
		current, e := s.fileRevision(id, h.Current)
		if e != nil {
			return e
		}
		if actual != current.Checksum {
			return ErrConflict
		}
		expected = current.Checksum
	}
	if actual != expected {
		return ErrConflict
	}
	if err = s.validateFileOutput(c, b, len(target.ids)); err != nil {
		return err
	}
	if err = s.DiskCheck(s.Root, s.Reserve+uint64(len(b))); err != nil {
		return err
	}
	if direction == "restore" {
		before, e := s.read(c.Path)
		if e != nil {
			return e
		}
		var ids []string
		if c.Status == "ready" {
			ids, e = s.fileRecordIDs(c)
		} else {
			current, readErr := s.fileRevision(id, h.Current)
			e, ids = readErr, current.ids
		}
		if e != nil {
			return e
		}
		return s.publishFileRevision(context.Background(), c, before, b, ids, target.ids, fmt.Sprintf("Restored revision %d", revision))
	}
	if _, err = s.DB.Exec("UPDATE file_history SET pending_revision=?,pending_new=0 WHERE collection_id=?", revision, id); err != nil {
		return err
	}
	err = atomicFileRevision(context.Background(), c.Path, b, expected)
	if recoverErr := s.recoverFileWrite(c); recoverErr != nil {
		return recoverErr
	}
	if err != nil {
		return err
	}
	return s.syncFile(c.Path, true)
}
func (s *Service) fileRecordIDs(c Collection) ([]string, error) {
	s.retainGeneration(c.Generation)
	defer s.releaseGeneration(c.ID, c.Generation)
	db, err := s.openIndex(c)
	if err != nil {
		return nil, err
	}
	defer db.Close()
	rows, err := db.Query("SELECT record_id FROM records ORDER BY ordinal")
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var ids []string
	for rows.Next() {
		var id string
		if err = rows.Scan(&id); err != nil {
			return nil, err
		}
		ids = append(ids, id)
	}
	return ids, rows.Err()
}
