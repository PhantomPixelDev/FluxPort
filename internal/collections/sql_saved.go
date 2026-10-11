package collections

import (
	"encoding/json"
	"fmt"
	"strings"
	"time"
)

type SavedQuery struct {
	ID          string   `json:"id"`
	Name        string   `json:"name"`
	SQL         string   `json:"sql"`
	Collections []string `json:"collections"`
	Published   bool     `json:"published"`
	Updated     string   `json:"updated"`
}
type SQLHistory struct {
	SQL         string
	Collections []string
	Error       string
	DurationMS  int64
	Created     string
}

func (s *Service) SaveSQL(q SavedQuery) (SavedQuery, error) {
	q.Name = strings.TrimSpace(q.Name)
	if q.Name == "" || len(q.Name) > 100 {
		return q, fmt.Errorf("query name must contain 1 to 100 characters")
	}
	if err := ValidateSQL(q.SQL); err != nil {
		return q, err
	}
	if len(q.Collections) == 0 || len(q.Collections) > 8 {
		return q, fmt.Errorf("choose between 1 and 8 collections")
	}
	for _, id := range q.Collections {
		if _, err := s.Get(id); err != nil {
			return q, err
		}
	}
	q.ID = recordID("sql", "query:"+q.Name, 0)
	q.Updated = time.Now().UTC().Format(time.RFC3339)
	ids, err := json.Marshal(q.Collections)
	if err != nil {
		return q, err
	}
	_, err = s.DB.Exec(`INSERT INTO sql_queries(id,name,sql_text,collections_json,published,updated) VALUES(?,?,?,?,?,?) ON CONFLICT(name) DO UPDATE SET sql_text=excluded.sql_text,collections_json=excluded.collections_json,published=excluded.published,updated=excluded.updated`, q.ID, q.Name, q.SQL, string(ids), q.Published, q.Updated)
	return q, err
}
func (s *Service) SavedSQL() ([]SavedQuery, error) {
	rows, err := s.DB.Query(`SELECT id,name,sql_text,collections_json,published,updated FROM sql_queries ORDER BY name`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []SavedQuery{}
	for rows.Next() {
		var q SavedQuery
		var ids string
		if err = rows.Scan(&q.ID, &q.Name, &q.SQL, &ids, &q.Published, &q.Updated); err != nil {
			return nil, err
		}
		if err = json.Unmarshal([]byte(ids), &q.Collections); err != nil {
			return nil, err
		}
		out = append(out, q)
	}
	return out, rows.Err()
}
func (s *Service) GetSQL(id string) (SavedQuery, error) {
	var q SavedQuery
	var ids string
	err := s.DB.QueryRow(`SELECT id,name,sql_text,collections_json,published,updated FROM sql_queries WHERE id=?`, id).Scan(&q.ID, &q.Name, &q.SQL, &ids, &q.Published, &q.Updated)
	if err == nil {
		err = json.Unmarshal([]byte(ids), &q.Collections)
	}
	return q, err
}
func (s *Service) DeleteSQL(id string) error {
	_, err := s.DB.Exec(`DELETE FROM sql_queries WHERE id=?`, id)
	return err
}
func (s *Service) RecordSQL(text string, ids []string, duration int64, queryErr error) error {
	if len(text) > 16384 {
		return nil
	}
	encoded, err := json.Marshal(ids)
	if err != nil {
		return err
	}
	message := ""
	if queryErr != nil {
		message = queryErr.Error()
	}
	tx, err := s.DB.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	_, err = tx.Exec(`INSERT INTO sql_history(id,sql_text,collections_json,error,duration_ms,created) VALUES(?,?,?,?,?,?)`, newRecordID(), text, string(encoded), message, duration, time.Now().UTC().Format(time.RFC3339Nano))
	if err != nil {
		return err
	}
	_, err = tx.Exec(`DELETE FROM sql_history WHERE id NOT IN (SELECT id FROM sql_history ORDER BY created DESC LIMIT 200)`)
	if err != nil {
		return err
	}
	return tx.Commit()
}
func (s *Service) SQLHistory() ([]SQLHistory, error) {
	rows, err := s.DB.Query(`SELECT sql_text,collections_json,error,duration_ms,created FROM sql_history ORDER BY created DESC LIMIT 20`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []SQLHistory{}
	for rows.Next() {
		var item SQLHistory
		var ids string
		if err = rows.Scan(&item.SQL, &ids, &item.Error, &item.DurationMS, &item.Created); err != nil {
			return nil, err
		}
		if err = json.Unmarshal([]byte(ids), &item.Collections); err != nil {
			return nil, err
		}
		out = append(out, item)
	}
	return out, rows.Err()
}
