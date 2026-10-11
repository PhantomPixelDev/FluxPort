package collections

import (
	"context"
	"database/sql"
	"encoding/base64"
	"fmt"
	"math"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"time"
	"unicode/utf8"

	duckdb "github.com/duckdb/duckdb-go/v2"
)

func duckDBAvailable() bool { return true }

func (s *Service) duckDBSQL(parent context.Context, ids []string, text string) (out SQLResult, err error) {
	start := time.Now()
	defer func() { out.DurationMS = time.Since(start).Milliseconds() }()
	out.Rows = [][]any{}
	if err = ValidateSQL(text); err != nil {
		return out, err
	}
	if err = validateDuckDBSQL(text); err != nil {
		return out, err
	}
	if len(ids) == 0 || len(ids) > 8 {
		return out, fmt.Errorf("choose between 1 and 8 collections")
	}
	timeout := s.DuckDBTimeout
	if timeout <= 0 {
		timeout = 30 * time.Second
	}
	ctx, cancel := context.WithTimeout(parent, timeout)
	defer cancel()
	release, err := s.acquireDuckDB(ctx)
	if err != nil {
		return out, err
	}
	defer release()
	if err = s.DiskCheck(s.IndexDir, s.Reserve); err != nil {
		return out, err
	}
	if err = os.MkdirAll(s.DuckDBTempDir, 0700); err != nil {
		return out, err
	}
	workDir, err := os.MkdirTemp(s.DuckDBTempDir, ".duckdb-query-")
	if err != nil {
		return out, err
	}
	defer os.RemoveAll(workDir)
	if err = os.Chmod(workDir, 0700); err != nil {
		return out, err
	}
	tempDir := filepath.Join(workDir, "spill")
	if err = os.Mkdir(tempDir, 0700); err != nil {
		return out, err
	}
	diskErrors := make(chan error, 1)
	diskMonitorDone := make(chan struct{})
	go func() {
		defer close(diskMonitorDone)
		ticker := time.NewTicker(500 * time.Millisecond)
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				if checkErr := s.DiskCheck(workDir, s.Reserve); checkErr != nil {
					diskErrors <- checkErr
					cancel()
					return
				}
			}
		}
	}()
	defer func() {
		cancel()
		<-diskMonitorDone
		select {
		case diskErr := <-diskErrors:
			err = diskErr
		default:
		}
	}()
	db, err := sql.Open("duckdb", s.duckDBMemoryDSN(tempDir))
	if err != nil {
		return out, err
	}
	defer db.Close()
	db.SetMaxOpenConns(1)
	conn, err := db.Conn(ctx)
	if err != nil {
		return out, err
	}
	defer conn.Close()
	seen := map[string]bool{}
	for i, id := range ids {
		if seen[id] {
			return out, fmt.Errorf("collection selected twice")
		}
		seen[id] = true
		c, e := s.Get(id)
		if e != nil {
			return out, e
		}
		c, e = s.ready(c)
		if e != nil {
			return out, e
		}
		unlock := s.lock(c.ID + "-generation")
		c, e = s.Get(id)
		if e != nil || c.Status != "ready" || c.Generation == "" || filepath.Base(c.Generation) != c.Generation {
			unlock()
			if e != nil {
				return out, e
			}
			return out, ErrPending
		}
		s.retainGeneration(c.Generation)
		generation := c.Generation
		out.Datasets = append(out.Datasets, SQLDataset{ID: id, Name: c.Name, Table: "c_" + id})
		unlock()
		defer s.releaseGeneration(c.ID, generation)
		cachePath, e := s.duckDBCache(ctx, c)
		if e != nil {
			return out, e
		}
		alias := fmt.Sprintf("data%d", i)
		if _, e = conn.ExecContext(ctx, `ATTACH `+quoteSQL(cachePath)+` AS `+quoteIdentifier(alias)+` (READ_ONLY)`); e != nil {
			return out, e
		}
		fields, e := s.Fields(c)
		if e != nil {
			return out, e
		}
		if len(fields) > 128 {
			return out, fmt.Errorf("SQL supports up to 128 fields per collection")
		}
		dataset := &out.Datasets[len(out.Datasets)-1]
		dataset.Fields = []string{"_record_id", "_ordinal", "_json"}
		columns := []string{`record_id AS "_record_id"`, `ordinal AS "_ordinal"`, `payload AS "_json"`}
		for _, field := range fields {
			if field.Name == "_record_id" || field.Name == "_ordinal" || field.Name == "_json" {
				continue
			}
			path, pe := jsonPath("/" + strings.ReplaceAll(strings.ReplaceAll(field.Name, "~", "~0"), "/", "~1"))
			if pe != nil {
				continue
			}
			columns = append(columns, `json_extract(payload,`+quoteSQL(path)+`) AS `+quoteIdentifier(field.Name))
			dataset.Fields = append(dataset.Fields, field.Name)
		}
		_, e = conn.ExecContext(ctx, `CREATE TEMP VIEW `+quoteIdentifier(dataset.Table)+` AS SELECT `+strings.Join(columns, ",")+` FROM `+quoteIdentifier(alias)+`.records`)
		if e != nil {
			return out, e
		}
	}
	// Only generation-keyed cache files were attached above. From this point,
	// user SQL cannot access files or load extensions.
	if _, err = conn.ExecContext(ctx, `SET enable_external_access=false`); err != nil {
		return out, err
	}
	rows, err := conn.QueryContext(ctx, text)
	if err != nil {
		return out, err
	}
	defer rows.Close()
	if out.Columns, err = rows.Columns(); err != nil {
		return out, err
	}
	if len(out.Columns) > 128 {
		return out, fmt.Errorf("result exceeds 128 columns")
	}
	bytes, count := 0, 0
	for rows.Next() {
		if err = ctx.Err(); err != nil {
			return out, err
		}
		if count >= 1000 {
			out.Truncated = true
			break
		}
		row := make([]any, len(out.Columns))
		ptrs := make([]any, len(row))
		for i := range row {
			ptrs[i] = &row[i]
		}
		if err = rows.Scan(ptrs...); err != nil {
			return out, err
		}
		for i, value := range row {
			if b, ok := value.([]byte); ok {
				row[i] = base64.StdEncoding.EncodeToString(b)
			}
			if v, ok := row[i].(string); ok && !utf8.ValidString(v) {
				return out, fmt.Errorf("SQL result contains invalid UTF-8 text")
			}
			if n, ok := row[i].(float64); ok && (math.IsInf(n, 0) || math.IsNaN(n)) {
				return out, fmt.Errorf("SQL result contains a non-finite number")
			}
			bytes += len(fmt.Sprint(row[i]))
			if bytes > 8<<20 {
				return out, fmt.Errorf("SQL results exceed 8 MiB; select fewer columns or use a narrower filter")
			}
		}
		out.Rows = append(out.Rows, row)
		count++
	}
	return out, rows.Err()
}

func (s *Service) acquireDuckDB(ctx context.Context) (func(), error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	select {
	case s.duckSlots <- struct{}{}:
		return func() { <-s.duckSlots }, nil
	case <-ctx.Done():
		return nil, ctx.Err()
	}
}

func (s *Service) duckDBMemoryDSN(tempDir string) string {
	values := url.Values{}
	values.Set("memory_limit", fmt.Sprintf("%dB", s.DuckDBMemoryBytes))
	values.Set("threads", "2")
	values.Set("temp_directory", tempDir)
	values.Set("max_temp_directory_size", fmt.Sprintf("%dB", s.DuckDBTempBytes))
	return "?" + values.Encode()
}

func (s *Service) duckDBCache(ctx context.Context, c Collection) (string, error) {
	if c.Generation == "" || filepath.Base(c.Generation) != c.Generation {
		return "", ErrConflict
	}
	unlock := s.lock("duckdb-cache:" + c.Generation)
	defer unlock()
	dir := filepath.Join(s.IndexDir, "duckdb")
	if err := os.MkdirAll(dir, 0700); err != nil {
		return "", err
	}
	path := filepath.Join(dir, strings.TrimSuffix(c.Generation, ".db")+".duckdb")
	if validDuckDBCache(ctx, path, c.Generation) {
		return path, nil
	}
	if err := s.DiskCheck(dir, s.Reserve); err != nil {
		return "", err
	}
	tmp := path + ".tmp"
	_ = os.Remove(tmp)
	defer os.Remove(tmp)
	if err := os.MkdirAll(s.DuckDBTempDir, 0700); err != nil {
		return "", err
	}
	if err := s.DiskCheck(s.DuckDBTempDir, s.Reserve); err != nil {
		return "", err
	}
	cacheTemp, err := os.MkdirTemp(s.DuckDBTempDir, ".duckdb-cache-")
	if err != nil {
		return "", err
	}
	defer os.RemoveAll(cacheTemp)
	if err = os.Mkdir(filepath.Join(cacheTemp, "spill"), 0700); err != nil {
		return "", err
	}
	connector, err := duckdb.NewConnector(tmp+s.duckDBOptions(filepath.Join(cacheTemp, "spill")), nil)
	if err != nil {
		return "", err
	}
	connectorClosed := false
	defer func() {
		if !connectorClosed {
			_ = connector.Close()
		}
	}()
	db := sql.OpenDB(connector)
	db.SetMaxOpenConns(1)
	if _, err = db.ExecContext(ctx, `CREATE TABLE records (ordinal BIGINT,record_id VARCHAR,payload VARCHAR,identity_key VARCHAR,occurrence BIGINT)`); err != nil {
		db.Close()
		return "", err
	}
	if _, err = db.ExecContext(ctx, `CREATE TABLE fluxport_meta (generation VARCHAR)`); err != nil {
		db.Close()
		return "", err
	}
	if _, err = db.ExecContext(ctx, `INSERT INTO fluxport_meta VALUES (?)`, c.Generation); err != nil {
		db.Close()
		return "", err
	}
	sourcePath := filepath.Join(s.IndexDir, c.Generation)
	if filepath.Base(c.Generation) != c.Generation {
		db.Close()
		return "", ErrConflict
	}
	source, err := sql.Open("sqlite", sqliteIndexURI(sourcePath))
	if err != nil {
		db.Close()
		return "", err
	}
	defer source.Close()
	rows, err := source.QueryContext(ctx, `SELECT ordinal,record_id,payload,identity_key,occurrence FROM records ORDER BY ordinal`)
	if err != nil {
		db.Close()
		return "", err
	}
	conn, err := connector.Connect(ctx)
	if err != nil {
		rows.Close()
		db.Close()
		return "", err
	}
	connClosed := false
	defer func() {
		if !connClosed {
			_ = conn.Close()
		}
	}()
	appender, err := duckdb.NewAppenderFromConn(conn, "", "records")
	if err != nil {
		rows.Close()
		db.Close()
		return "", err
	}
	defer func() {
		if appender != nil {
			_ = appender.Close()
		}
	}()
	var copiedBytes int64
	for rows.Next() {
		var ordinal, occurrence int64
		var id, payload, key string
		if err = rows.Scan(&ordinal, &id, &payload, &key, &occurrence); err != nil {
			break
		}
		if err = ctx.Err(); err != nil {
			break
		}
		if err = appender.AppendRow(ordinal, id, payload, key, occurrence); err != nil {
			break
		}
		copiedBytes += int64(len(payload))
		if copiedBytes >= 16<<20 {
			if err = s.DiskCheck(dir, s.Reserve); err == nil {
				err = s.DiskCheck(cacheTemp, s.Reserve)
			}
			if err != nil {
				break
			}
			copiedBytes = 0
		}
	}
	if err == nil {
		err = rows.Err()
	}
	rows.Close()
	if err != nil {
		_ = appender.Close()
		appender = nil
		db.Close()
		return "", err
	}
	if err = appender.Close(); err != nil {
		appender = nil
		db.Close()
		return "", err
	}
	appender = nil
	if err = conn.Close(); err != nil {
		connClosed = true
		db.Close()
		return "", err
	}
	connClosed = true
	if err = db.Close(); err != nil {
		return "", err
	}
	if err = connector.Close(); err != nil {
		connectorClosed = true
		return "", err
	}
	connectorClosed = true
	if _, statErr := os.Stat(path); statErr == nil {
		if err = os.Remove(path); err != nil {
			return "", err
		}
	}
	if err = os.Rename(tmp, path); err != nil {
		return "", err
	}
	return path, nil
}

func (s *Service) duckDBOptions(tempDir string) string {
	values := url.Values{}
	values.Set("memory_limit", fmt.Sprintf("%dB", s.DuckDBMemoryBytes))
	values.Set("threads", "2")
	values.Set("temp_directory", tempDir)
	values.Set("max_temp_directory_size", fmt.Sprintf("%dB", s.DuckDBTempBytes))
	return "?" + values.Encode()
}

// Cache cleanup is filesystem-only so the optional engine is still lazy at
// startup. Each cache remains tied to a currently published SQLite generation.
func (s *Service) cleanupDuckDBCaches() {
	rows, err := s.DB.Query(`SELECT generation FROM collections WHERE generation<>''`)
	if err != nil {
		return
	}
	keep := map[string]bool{}
	for rows.Next() {
		var generation string
		if rows.Scan(&generation) == nil && filepath.Base(generation) == generation {
			keep[strings.TrimSuffix(generation, ".db")+".duckdb"] = true
		}
	}
	rows.Close()
	dir := filepath.Join(s.IndexDir, "duckdb")
	entries, err := os.ReadDir(dir)
	if err == nil {
		for _, entry := range entries {
			name := entry.Name()
			if entry.IsDir() || !strings.Contains(name, ".duckdb") {
				continue
			}
			valid := false
			for cache := range keep {
				if name == cache || strings.HasPrefix(name, cache+".") || strings.HasPrefix(name, cache+"-") {
					valid = true
					break
				}
			}
			if !valid {
				_ = os.Remove(filepath.Join(dir, name))
			}
		}
	}
	if tempEntries, tempErr := os.ReadDir(s.DuckDBTempDir); tempErr == nil {
		for _, entry := range tempEntries {
			if entry.IsDir() && (strings.HasPrefix(entry.Name(), ".duckdb-query-") || strings.HasPrefix(entry.Name(), ".duckdb-cache-")) {
				_ = os.RemoveAll(filepath.Join(s.DuckDBTempDir, entry.Name()))
			}
		}
	}
}

func validDuckDBCache(ctx context.Context, path, generation string) bool {
	if _, err := os.Stat(path); err != nil {
		return false
	}
	db, err := sql.Open("duckdb", path+"?access_mode=read_only&threads=1")
	if err != nil {
		return false
	}
	defer db.Close()
	var actual string
	return db.QueryRowContext(ctx, `SELECT generation FROM fluxport_meta LIMIT 1`).Scan(&actual) == nil && actual == generation
}

func sqliteIndexURI(path string) string {
	path = filepath.ToSlash(path)
	if len(path) > 1 && path[1] == ':' {
		path = "/" + path
	}
	return (&url.URL{Scheme: "file", Path: path, RawQuery: "mode=ro&immutable=1"}).String()
}

func validateDuckDBSQL(query string) error {
	for i := 0; i < len(query); {
		c := query[i]
		if c == '-' && i+1 < len(query) && query[i+1] == '-' {
			for i < len(query) && query[i] != '\n' {
				i++
			}
			continue
		}
		if c == '/' && i+1 < len(query) && query[i+1] == '*' {
			if end := strings.Index(query[i+2:], "*/"); end >= 0 {
				i += end + 4
				continue
			}
			return fmt.Errorf("unterminated SQL comment")
		}
		if c == '\'' {
			i++
			for i < len(query) {
				if query[i] == '\'' {
					if i+1 < len(query) && query[i+1] == '\'' {
						i += 2
						continue
					}
					i++
					break
				}
				i++
			}
			continue
		}
		if c == '"' || c == '`' || c == '[' {
			end := c
			if c == '[' {
				end = ']'
			}
			i++
			start := i
			for i < len(query) {
				if query[i] == end {
					if i+1 < len(query) && query[i+1] == end && end != ']' {
						i += 2
						continue
					}
					break
				}
				i++
			}
			if i >= len(query) {
				return fmt.Errorf("unterminated SQL identifier")
			}
			identifier := strings.ReplaceAll(query[start:i], string([]byte{end, end}), string(end))
			if err := validateDuckDBIdentifier(identifier); err != nil {
				return err
			}
			i++
			continue
		}
		if (c >= 'a' && c <= 'z') || (c >= 'A' && c <= 'Z') || c == '_' {
			start := i
			i++
			for i < len(query) && ((query[i] >= 'a' && query[i] <= 'z') || (query[i] >= 'A' && query[i] <= 'Z') || (query[i] >= '0' && query[i] <= '9') || query[i] == '_') {
				i++
			}
			if err := validateDuckDBIdentifier(query[start:i]); err != nil {
				return err
			}
			continue
		}
		i++
	}
	return nil
}

func validateDuckDBIdentifier(identifier string) error {
	token := strings.ToUpper(identifier)
	blocked := token == "INFORMATION_SCHEMA" || token == "FLUXPORT_META" || strings.HasPrefix(token, "DUCKDB_") || strings.HasPrefix(token, "PG_") || strings.HasPrefix(token, "DATA0") || strings.HasPrefix(token, "DATA1") || strings.HasPrefix(token, "DATA2") || strings.HasPrefix(token, "DATA3") || strings.HasPrefix(token, "DATA4") || strings.HasPrefix(token, "DATA5") || strings.HasPrefix(token, "DATA6") || strings.HasPrefix(token, "DATA7")
	for _, prefix := range []string{"READ_CSV", "READ_JSON", "READ_PARQUET", "PARQUET_SCAN", "READ_BLOB", "READ_TEXT", "READ_FILE", "GLOB", "QUERY_TABLE", "SQLITE_SCAN", "POSTGRES_SCAN", "MYSQL_SCAN"} {
		blocked = blocked || strings.HasPrefix(token, prefix)
	}
	for _, name := range []string{"QUERY", "INSTALL", "LOAD", "COPY", "EXPORT", "IMPORT", "CURRENT_SETTING", "SET_CONFIG", "CALL"} {
		blocked = blocked || token == name
	}
	if blocked {
		return fmt.Errorf("DuckDB identifier %s is not allowed in the isolated SQL playground", identifier)
	}
	return nil
}
