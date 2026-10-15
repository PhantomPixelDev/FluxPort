package config

import (
	"os"
	"path/filepath"
	"testing"
)

func TestEnvironmentAndRemoteAuth(t *testing.T) {
	for _, key := range []string{"FLUXPORT_CONFIG", "FLUXPORT_ADDRESS", "FLUXPORT_DATA_DIR", "FLUXPORT_STATE_DIR", "FLUXPORT_ADMIN_PASSWORD", "FLUXPORT_API_KEY", "FLUXPORT_MAX_FILE_BYTES", "FLUXPORT_DUCKDB_MEMORY_BYTES", "FLUXPORT_DUCKDB_TEMP_BYTES", "FLUXPORT_DUCKDB_TIMEOUT_SECONDS", "FLUXPORT_DUCKDB_TEMP_DIR"} {
		t.Setenv(key, "")
	}
	t.Setenv("FLUXPORT_ADDRESS", "0.0.0.0:8080")
	if _, err := Load(); err == nil {
		t.Fatal("remote dashboard allowed without password")
	}
	t.Setenv("FLUXPORT_ADMIN_PASSWORD", "secret")
	t.Setenv("FLUXPORT_DATA_DIR", t.TempDir())
	t.Setenv("FLUXPORT_STATE_DIR", t.TempDir())
	if _, err := Load(); err != nil {
		t.Fatal(err)
	}
	t.Setenv("FLUXPORT_MAX_FILE_BYTES", "-1")
	if _, err := Load(); err == nil {
		t.Fatal("invalid limit accepted")
	}
}
func TestConfigFile(t *testing.T) {
	for _, key := range []string{"FLUXPORT_ADDRESS", "FLUXPORT_DATA_DIR", "FLUXPORT_STATE_DIR", "FLUXPORT_ADMIN_PASSWORD", "FLUXPORT_API_KEY", "FLUXPORT_MAX_FILE_BYTES", "FLUXPORT_DUCKDB_MEMORY_BYTES", "FLUXPORT_DUCKDB_TEMP_BYTES", "FLUXPORT_DUCKDB_TIMEOUT_SECONDS", "FLUXPORT_DUCKDB_TEMP_DIR"} {
		t.Setenv(key, "")
	}
	path := filepath.Join(t.TempDir(), "config.json")
	if err := os.WriteFile(path, []byte(`{"address":"127.0.0.1:9999","max_file_bytes":4096}`), 0600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("FLUXPORT_CONFIG", path)
	t.Setenv("FLUXPORT_DATA_DIR", t.TempDir())
	t.Setenv("FLUXPORT_STATE_DIR", t.TempDir())
	t.Setenv("FLUXPORT_ADDRESS", "127.0.0.1:8081")
	c, err := Load()
	if err != nil {
		t.Fatal(err)
	}
	if c.Address != "127.0.0.1:8081" || c.MaxFileBytes != 4096 {
		t.Fatal(c)
	}
}

func TestTrustedProxyConfiguration(t *testing.T) {
	t.Setenv("FLUXPORT_CONFIG", "")
	t.Setenv("FLUXPORT_ADDRESS", "127.0.0.1:8080")
	t.Setenv("FLUXPORT_TRUSTED_PROXIES", "127.0.0.1,10.0.0.0/8")
	c, err := Load()
	if err != nil {
		t.Fatal(err)
	}
	if len(c.TrustedProxies) != 2 {
		t.Fatal(c.TrustedProxies)
	}
	t.Setenv("FLUXPORT_TRUSTED_PROXIES", "not-an-address")
	if _, err := Load(); err == nil {
		t.Fatal("invalid trusted proxy accepted")
	}
}

func TestDuckDBLimitsConfiguration(t *testing.T) {
	for _, key := range []string{"FLUXPORT_CONFIG", "FLUXPORT_ADDRESS", "FLUXPORT_DATA_DIR", "FLUXPORT_STATE_DIR", "FLUXPORT_ADMIN_PASSWORD", "FLUXPORT_API_KEY", "FLUXPORT_DUCKDB_MEMORY_BYTES", "FLUXPORT_DUCKDB_TEMP_BYTES", "FLUXPORT_DUCKDB_TIMEOUT_SECONDS", "FLUXPORT_DUCKDB_TEMP_DIR"} {
		t.Setenv(key, "")
	}
	t.Setenv("FLUXPORT_DATA_DIR", t.TempDir())
	t.Setenv("FLUXPORT_STATE_DIR", t.TempDir())
	t.Setenv("FLUXPORT_ADDRESS", "127.0.0.1:8080")
	t.Setenv("FLUXPORT_DUCKDB_MEMORY_BYTES", "1073741824")
	t.Setenv("FLUXPORT_DUCKDB_TEMP_BYTES", "8589934592")
	t.Setenv("FLUXPORT_DUCKDB_TIMEOUT_SECONDS", "60")
	t.Setenv("FLUXPORT_DUCKDB_TEMP_DIR", t.TempDir())
	c, err := Load()
	if err != nil {
		t.Fatal(err)
	}
	if c.DuckDBMemoryBytes != 1<<30 || c.DuckDBTempBytes != 8<<30 || c.DuckDBTimeoutSeconds != 60 || c.DuckDBTempDir == "" {
		t.Fatalf("DuckDB limits were not applied: %+v", c)
	}
	t.Setenv("FLUXPORT_DUCKDB_MEMORY_BYTES", "1024")
	if _, err = Load(); err == nil {
		t.Fatal("unsafe DuckDB memory limit accepted")
	}
}
