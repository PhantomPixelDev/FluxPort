package collections

import (
	"context"
	"fmt"
	"strings"
	"testing"
	"time"
)

func TestSQLIsolatedJoinsAggregationsAndLimits(t *testing.T) {
	s := testService(t)
	users, err := s.Import("users.json", strings.NewReader(`[{"id":1,"name":"Ada"},{"id":2,"name":"Grace"}]`))
	if err != nil {
		t.Fatal(err)
	}
	orders, err := s.Import("orders.json", strings.NewReader(`[{"user_id":1,"amount":2.5},{"user_id":1,"amount":3},{"user_id":2,"amount":9}]`))
	if err != nil {
		t.Fatal(err)
	}
	query := fmt.Sprintf(`SELECT u.name,SUM(o.amount) AS total FROM "c_%s" u JOIN "c_%s" o ON u.id=o.user_id GROUP BY u.name ORDER BY total DESC`, users.ID, orders.ID)
	result, err := s.SQL(context.Background(), []string{users.ID, orders.ID}, query)
	if err != nil || len(result.Rows) != 2 || result.Rows[0][0] != "Grace" || result.Rows[0][1] != int64(9) {
		t.Fatalf("%+v %v", result, err)
	}
	for _, query := range []string{`SELECT * FROM collections`, `SELECT * FROM settings`, `SELECT * FROM jobs`, `SELECT zeroblob(1000000000)`, `SELECT printf('%1000000000s','x')`, `SELECT 1e999`, `SELECT CAST(x'ff' AS TEXT)`} {
		if _, err = s.SQL(context.Background(), []string{users.ID}, query); err == nil {
			t.Fatal("unsafe or oversized SQL succeeded", query)
		}
	}
	result, err = s.SQL(context.Background(), []string{users.ID}, `SELECT x'00ff' AS binary`)
	if err != nil || result.Rows[0][0] != "AP8=" {
		t.Fatal("BLOB conversion", result, err)
	}
	result, err = s.SQL(context.Background(), []string{users.ID}, `WITH RECURSIVE n(x) AS (SELECT 1 UNION ALL SELECT x+1 FROM n WHERE x<2000) SELECT x FROM n`)
	if err != nil || len(result.Rows) != 1000 || !result.Truncated {
		t.Fatal(result, err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Millisecond)
	defer cancel()
	_, err = s.SQL(ctx, []string{users.ID}, `WITH RECURSIVE n(x) AS (SELECT 1 UNION ALL SELECT x+1 FROM n) SELECT SUM(x) FROM n`)
	if err == nil {
		t.Fatal("infinite SQL did not time out")
	}
}
func TestSQLStatementValidation(t *testing.T) {
	for _, text := range []string{`SELECT 1; ATTACH 'secret' AS s`, `WITH x AS (SELECT 1) DELETE FROM records`, `PRAGMA database_list`, `SELECT load_extension('x')`, `SELECT * FROM pragma_table_info('x')`, `SELECT 1 /* unterminated`, `SELECT 'unterminated`} {
		if ValidateSQL(text) == nil {
			t.Fatal("accepted", text)
		}
	}
	for _, text := range []string{`SELECT 'DELETE; ATTACH' AS message; -- safe`, `/* SELECT */ WITH x AS (SELECT 1) SELECT * FROM x`, `SELECT "a;b" FROM "c_data"`} {
		if err := ValidateSQL(text); err != nil {
			t.Fatal(text, err)
		}
	}
}

func TestSQLEngineSelection(t *testing.T) {
	s := testService(t)
	c, err := s.Import("rows.json", strings.NewReader(`[{"id":1}]`))
	if err != nil {
		t.Fatal(err)
	}
	result, err := s.SQLWithEngine(context.Background(), []string{c.ID}, fmt.Sprintf(`SELECT count(*) AS n FROM "c_%s"`, c.ID), "auto")
	if err != nil || result.Engine != "SQLite" || len(result.Rows) != 1 {
		t.Fatal(result, err)
	}
	if _, err = s.SQLWithEngine(context.Background(), []string{c.ID}, `SELECT 1`, "unknown"); err == nil {
		t.Fatal("unknown engine was accepted")
	}
}

func TestSQLLowDiskAndSavedQueryPersistence(t *testing.T) {
	s := testService(t)
	c, err := s.Import("rows.json", strings.NewReader(`[{"id":1}]`))
	if err != nil {
		t.Fatal(err)
	}
	q, err := s.SaveSQL(SavedQuery{Name: "Count", Collections: []string{c.ID}, SQL: fmt.Sprintf(`SELECT count(*) AS count FROM "c_%s"`, c.ID), Published: true})
	if err != nil {
		t.Fatal(err)
	}
	got, err := s.GetSQL(q.ID)
	if err != nil || got.SQL != q.SQL || !got.Published {
		t.Fatal(got, err)
	}
	if err = s.RecordSQL(q.SQL, q.Collections, 1, nil); err != nil {
		t.Fatal(err)
	}
	history, err := s.SQLHistory()
	if err != nil || len(history) != 1 {
		t.Fatal(history, err)
	}
	s.DiskCheck = func(string, uint64) error { return fmt.Errorf("insufficient free disk") }
	if _, err = s.SQL(context.Background(), q.Collections, q.SQL); err == nil || !strings.Contains(err.Error(), "free disk") {
		t.Fatal("low disk was ignored", err)
	}
	if err = s.DeleteSQL(q.ID); err != nil {
		t.Fatal(err)
	}
}

func TestSQLStreamingDoesNotTruncateAndCancels(t *testing.T) {
	s := testService(t)
	c, err := s.Import("rows.json", strings.NewReader(`[{"id":1}]`))
	if err != nil {
		t.Fatal(err)
	}
	count := 0
	query := `WITH RECURSIVE n(x) AS (SELECT 1 UNION ALL SELECT x+1 FROM n WHERE x<2000) SELECT x FROM n`
	err = s.SQLStream(context.Background(), []string{c.ID}, query, func(columns []string, row []any) error { count++; return nil })
	if err != nil || count != 2000 {
		t.Fatal(count, err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	count = 0
	err = s.SQLStream(ctx, []string{c.ID}, query, func([]string, []any) error {
		count++
		if count == 10 {
			cancel()
		}
		return nil
	})
	if err == nil || count > 11 {
		t.Fatal("stream ignored cancellation", count, err)
	}
}
