package collections

import (
	"context"
	"fmt"
	"os"
	"strings"
	"testing"
	"time"
)

func TestDuckDBSQLJoinsAggregatesAndIsolation(t *testing.T) {
	s := testService(t)
	users, err := s.Import("users.json", strings.NewReader(`[{"id":1,"name":"Ada","account":{"region":{"city":"Berlin"}},"display.name":"Analyst"},{"id":2,"name":"Grace"}]`))
	if err != nil {
		t.Fatal(err)
	}
	orders, err := s.Import("orders.json", strings.NewReader(`[{"user_id":1,"amount":2.5},{"user_id":1,"amount":3},{"user_id":2,"amount":9}]`))
	if err != nil {
		t.Fatal(err)
	}
	query := fmt.Sprintf(`SELECT u.name,SUM(CAST(o.amount AS DOUBLE)) AS total FROM "c_%s" u JOIN "c_%s" o ON CAST(u.id AS BIGINT)=CAST(o.user_id AS BIGINT) GROUP BY u.name ORDER BY total DESC`, users.ID, orders.ID)
	result, err := s.SQLWithEngine(context.Background(), []string{users.ID, orders.ID}, query, "duckdb")
	if err != nil {
		t.Fatal(err)
	}
	if result.Engine != "DuckDB" || len(result.Rows) != 2 || result.Rows[0][0] != "Grace" {
		t.Fatalf("unexpected result: %+v", result)
	}
	nestedQuery := fmt.Sprintf(`SELECT json_extract_string(account,'$.region.city') AS city,"display.name" FROM "c_%s" WHERE CAST(id AS BIGINT)=1`, users.ID)
	nested, err := s.SQLWithEngine(context.Background(), []string{users.ID}, nestedQuery, "duckdb")
	if err != nil || len(nested.Rows) != 1 || nested.Rows[0][0] != "Berlin" || nested.Rows[0][1] != "Analyst" {
		t.Fatalf("nested values or quoted columns were changed: %+v %v", nested, err)
	}
	if _, err = s.SQLWithEngine(context.Background(), []string{users.ID}, `SELECT * FROM read_csv_auto('/etc/passwd')`, "duckdb"); err == nil {
		t.Fatal("file-reading SQL was accepted")
	}
	if _, err = s.SQLWithEngine(context.Background(), []string{users.ID}, `SELECT * FROM duckdb_settings()`, "duckdb"); err == nil {
		t.Fatal("DuckDB internals were exposed")
	}
	if _, err = s.SQLWithEngine(context.Background(), []string{users.ID}, `SELECT 'READ_CSV is a harmless string' AS note`, "duckdb"); err != nil {
		t.Fatalf("safe SQL string literal was rejected: %v", err)
	}
	for _, unsafeQuery := range []string{`SELECT * FROM data0.records`, `SELECT * FROM pg_tables`, `SELECT * FROM information_schema.tables`} {
		if _, err = s.SQLWithEngine(context.Background(), []string{users.ID}, unsafeQuery, "duckdb"); err == nil {
			t.Fatalf("internal relation was exposed: %s", unsafeQuery)
		}
	}
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Millisecond)
	defer cancel()
	if _, err = s.SQLWithEngine(ctx, []string{users.ID}, `SELECT SUM(i) FROM range(1000000000) AS t(i)`, "duckdb"); err == nil {
		t.Fatal("DuckDB query ignored request cancellation")
	}
}

func TestDuckDBCacheTracksImmutableIndexGeneration(t *testing.T) {
	s := testService(t)
	c, err := s.Import("rows.json", strings.NewReader(`[{"id":1}]`))
	if err != nil {
		t.Fatal(err)
	}
	query := fmt.Sprintf(`SELECT count(*) FROM "c_%s"`, c.ID)
	for i, expected := range []int64{1, 2} {
		result, runErr := s.SQLWithEngine(context.Background(), []string{c.ID}, query, "duckdb")
		if runErr != nil || len(result.Rows) != 1 || fmt.Sprint(result.Rows[0][0]) != fmt.Sprint(expected) {
			t.Fatalf("iteration %d: %+v %v", i, result, runErr)
		}
		if i == 0 {
			if writeErr := os.WriteFile(c.Path, []byte(`[{"id":1},{"id":2}]`), 0600); writeErr != nil {
				t.Fatal(writeErr)
			}
			if syncErr := s.SyncFile(c.Path); syncErr != nil {
				t.Fatal(syncErr)
			}
			c, err = s.Get(c.ID)
			if err != nil {
				t.Fatal(err)
			}
		}
	}
}

func TestSQLEngineAutoSelectsLargeCollections(t *testing.T) {
	s := testService(t)
	c, err := s.Import("rows.json", strings.NewReader(`[{"id":1}]`))
	if err != nil {
		t.Fatal(err)
	}
	if _, err = s.DB.Exec(`UPDATE collections SET rows=250000 WHERE id=?`, c.ID); err != nil {
		t.Fatal(err)
	}
	query := fmt.Sprintf(`SELECT count(*) FROM "c_%s"`, c.ID)
	simple, err := s.SQLWithEngine(context.Background(), []string{c.ID}, `SELECT 1`, "auto")
	if err != nil || simple.Engine != "SQLite" {
		t.Fatalf("simple query should stay native: %+v %v", simple, err)
	}
	result, err := s.SQLWithEngine(context.Background(), []string{c.ID}, query, "auto")
	if err != nil || result.Engine != "DuckDB" {
		t.Fatalf("warm large collection should use DuckDB: %+v %v", result, err)
	}
}

func TestDuckDBBoundedMemoryAnalyticalSort(t *testing.T) {
	s := testService(t)
	s.DuckDBMemoryBytes = 64 << 20
	s.DuckDBTempBytes = 1 << 30
	c, err := s.Import("rows.json", strings.NewReader(`[{"id":1}]`))
	if err != nil {
		t.Fatal(err)
	}
	result, err := s.SQLWithEngine(context.Background(), []string{c.ID}, `SELECT i FROM range(5000000) AS t(i) ORDER BY hash(i) LIMIT 10`, "duckdb")
	if err != nil || len(result.Rows) != 10 {
		t.Fatalf("bounded-memory sort failed: %+v %v", result, err)
	}
}

func BenchmarkSQLNativeAggregate(b *testing.B) {
	benchmarkSQL(b, "native")
}

func BenchmarkSQLDuckDBAggregate(b *testing.B) {
	benchmarkSQL(b, "duckdb")
}

// Run with -benchtime=1x to include DuckDB's first-use cache build.
func BenchmarkSQLDuckDBFirstQuery(b *testing.B) {
	s := testService(b)
	var input strings.Builder
	input.WriteByte('[')
	for i := 0; i < 250000; i++ {
		if i > 0 {
			input.WriteByte(',')
		}
		fmt.Fprintf(&input, `{"id":%d,"group":%d,"amount":%d}`, i, i%100, i%31)
	}
	input.WriteByte(']')
	c, err := s.Import("first-query.json", strings.NewReader(input.String()))
	if err != nil {
		b.Fatal(err)
	}
	query := fmt.Sprintf(`SELECT "group",SUM(CAST("amount" AS BIGINT)) AS total FROM "c_%s" GROUP BY "group" ORDER BY "group"`, c.ID)
	b.ResetTimer()
	if _, err = s.SQLWithEngine(context.Background(), []string{c.ID}, query, "duckdb"); err != nil {
		b.Fatal(err)
	}
}

func benchmarkSQL(b *testing.B, engine string) {
	b.Helper()
	s := testService(b)
	var input strings.Builder
	input.WriteByte('[')
	for i := 0; i < 250000; i++ {
		if i > 0 {
			input.WriteByte(',')
		}
		fmt.Fprintf(&input, `{"id":%d,"group":%d,"amount":%d}`, i, i%100, i%31)
	}
	input.WriteByte(']')
	c, err := s.Import("bench.json", strings.NewReader(input.String()))
	if err != nil {
		b.Fatal(err)
	}
	query := fmt.Sprintf(`SELECT "group",SUM(CAST("amount" AS BIGINT)) AS total FROM "c_%s" GROUP BY "group" ORDER BY "group"`, c.ID)
	if engine == "duckdb" {
		if _, err = s.duckDBCache(context.Background(), c); err != nil {
			b.Fatal(err)
		}
	}
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if _, err = s.SQLWithEngine(context.Background(), []string{c.ID}, query, engine); err != nil {
			b.Fatal(err)
		}
	}
}
