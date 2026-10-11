package collections

import (
	"encoding/json"
	"fmt"
	"strings"
	"time"
)

type View struct {
	ID           string   `json:"id"`
	CollectionID string   `json:"collection_id"`
	Name         string   `json:"name"`
	Query        Query    `json:"query"`
	Columns      []string `json:"columns"`
	Updated      string   `json:"updated"`
}

func (s *Service) SaveView(v View) (View, error) {
	if _, err := s.Get(v.CollectionID); err != nil {
		return v, err
	}
	v.Name = strings.TrimSpace(v.Name)
	if v.Name == "" || len(v.Name) > 100 || len(v.Columns) > 128 {
		return v, fmt.Errorf("view name is required (up to 100 characters); select up to 128 columns")
	}
	if err := ValidateQuery(v.Query); err != nil {
		return v, err
	}
	for _, column := range v.Columns {
		if _, err := jsonPath(column); err != nil {
			return v, err
		}
	}
	v.Query.Page = 1
	v.ID = recordID(v.CollectionID, "view:"+v.Name, 0)
	v.Updated = time.Now().UTC().Format(time.RFC3339)
	q, err := json.Marshal(v.Query)
	if err != nil {
		return v, err
	}
	columns, err := json.Marshal(v.Columns)
	if err != nil {
		return v, err
	}
	_, err = s.DB.Exec(`INSERT INTO saved_views(id,collection_id,name,query_json,columns_json,updated) VALUES(?,?,?,?,?,?) ON CONFLICT(collection_id,name) DO UPDATE SET query_json=excluded.query_json,columns_json=excluded.columns_json,updated=excluded.updated`, v.ID, v.CollectionID, v.Name, string(q), string(columns), v.Updated)
	return v, err
}
func (s *Service) Views(collection string) ([]View, error) {
	rows, err := s.DB.Query(`SELECT id,collection_id,name,query_json,columns_json,updated FROM saved_views WHERE collection_id=? ORDER BY name`, collection)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []View{}
	for rows.Next() {
		var v View
		var q, columns string
		if err = rows.Scan(&v.ID, &v.CollectionID, &v.Name, &q, &columns, &v.Updated); err != nil {
			return nil, err
		}
		decoder := json.NewDecoder(strings.NewReader(q))
		decoder.UseNumber()
		if err = decoder.Decode(&v.Query); err != nil {
			return nil, err
		}
		if err = json.Unmarshal([]byte(columns), &v.Columns); err != nil {
			return nil, err
		}
		out = append(out, v)
	}
	return out, rows.Err()
}
func (s *Service) DeleteView(collection, id string) error {
	_, err := s.DB.Exec(`DELETE FROM saved_views WHERE collection_id=? AND id=?`, collection, id)
	return err
}
