package config

import (
	"encoding/json"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"runtime"
	"strconv"
)

type Config struct {
	PublicDemo           bool   `json:"public_demo"`
	Workers              int    `json:"workers"`
	MaxRecordBytes       int64  `json:"max_record_bytes"`
	MinFreeDiskBytes     uint64 `json:"min_free_disk_bytes"`
	MaxStreamBytes       int64  `json:"max_stream_bytes"`
	Address              string `json:"address"`
	DataDir              string `json:"data_dir"`
	StateDir             string `json:"state_dir"`
	AdminPassword        string `json:"admin_password"`
	APIKey               string `json:"api_key"`
	MaxFileBytes         int64  `json:"max_file_bytes"`
	DuckDBMemoryBytes    int64  `json:"duckdb_memory_bytes"`
	DuckDBTempBytes      uint64 `json:"duckdb_temp_bytes"`
	DuckDBTimeoutSeconds int    `json:"duckdb_timeout_seconds"`
	DuckDBTempDir        string `json:"duckdb_temp_dir"`
}

func Load() (Config, error) {
	c := Config{Workers: min(2, runtime.GOMAXPROCS(0)), MaxRecordBytes: 1 << 20, MinFreeDiskBytes: 256 << 20, Address: "127.0.0.1:8080", DataDir: "data", StateDir: ".fluxport", MaxFileBytes: 16 << 20, DuckDBMemoryBytes: 512 << 20, DuckDBTempBytes: 4 << 30, DuckDBTimeoutSeconds: 30}
	if file := os.Getenv("FLUXPORT_CONFIG"); file != "" {
		b, err := os.ReadFile(file)
		if err != nil {
			return c, err
		}
		if err = json.Unmarshal(b, &c); err != nil {
			return c, fmt.Errorf("config: %w", err)
		}
	}
	for key, dst := range map[string]*string{"FLUXPORT_ADDRESS": &c.Address, "FLUXPORT_DATA_DIR": &c.DataDir, "FLUXPORT_STATE_DIR": &c.StateDir, "FLUXPORT_ADMIN_PASSWORD": &c.AdminPassword, "FLUXPORT_API_KEY": &c.APIKey, "FLUXPORT_DUCKDB_TEMP_DIR": &c.DuckDBTempDir} {
		if value, ok := os.LookupEnv(key); ok {
			*dst = value
		}
	}
	if v := os.Getenv("FLUXPORT_MAX_FILE_BYTES"); v != "" {
		n, err := strconv.ParseInt(v, 10, 64)
		if err != nil {
			return c, fmt.Errorf("max file bytes: %w", err)
		}
		c.MaxFileBytes = n
	}
	if v, ok := os.LookupEnv("FLUXPORT_PUBLIC_DEMO"); ok {
		enabled, err := strconv.ParseBool(v)
		if err != nil {
			return c, fmt.Errorf("public demo: %w", err)
		}
		c.PublicDemo = enabled
	}
	if c.PublicDemo {
		c.APIKey = ""
	}
	for key, dst := range map[string]*int64{"FLUXPORT_MAX_RECORD_BYTES": &c.MaxRecordBytes, "FLUXPORT_MAX_STREAM_BYTES": &c.MaxStreamBytes} {
		if v := os.Getenv(key); v != "" {
			n, e := strconv.ParseInt(v, 10, 64)
			if e != nil {
				return c, e
			}
			*dst = n
		}
	}
	if v := os.Getenv("FLUXPORT_WORKERS"); v != "" {
		n, e := strconv.Atoi(v)
		if e != nil {
			return c, e
		}
		c.Workers = n
	}
	if v := os.Getenv("FLUXPORT_MIN_FREE_DISK_BYTES"); v != "" {
		n, e := strconv.ParseUint(v, 10, 64)
		if e != nil {
			return c, e
		}
		c.MinFreeDiskBytes = n
	}
	for key, target := range map[string]*int64{"FLUXPORT_DUCKDB_MEMORY_BYTES": &c.DuckDBMemoryBytes} {
		if value := os.Getenv(key); value != "" {
			n, e := strconv.ParseInt(value, 10, 64)
			if e != nil {
				return c, fmt.Errorf("%s: %w", key, e)
			}
			*target = n
		}
	}
	for key, target := range map[string]*uint64{"FLUXPORT_DUCKDB_TEMP_BYTES": &c.DuckDBTempBytes} {
		if value := os.Getenv(key); value != "" {
			n, e := strconv.ParseUint(value, 10, 64)
			if e != nil {
				return c, fmt.Errorf("%s: %w", key, e)
			}
			*target = n
		}
	}
	if value := os.Getenv("FLUXPORT_DUCKDB_TIMEOUT_SECONDS"); value != "" {
		n, e := strconv.Atoi(value)
		if e != nil {
			return c, fmt.Errorf("FLUXPORT_DUCKDB_TIMEOUT_SECONDS: %w", e)
		}
		c.DuckDBTimeoutSeconds = n
	}
	if c.Workers < 1 || c.Workers > 32 || c.MaxRecordBytes < 1024 || c.MaxRecordBytes > 16<<20 || c.MaxStreamBytes < 0 || c.MinFreeDiskBytes > 1<<50 {
		return c, fmt.Errorf("invalid worker or streaming limits")
	}
	if c.DuckDBMemoryBytes < 64<<20 || c.DuckDBMemoryBytes > 1<<40 || c.DuckDBTempBytes < 1<<30 || c.DuckDBTempBytes > 1<<50 || c.DuckDBTimeoutSeconds < 1 || c.DuckDBTimeoutSeconds > 900 {
		return c, fmt.Errorf("invalid DuckDB memory, temp disk, or timeout limit")
	}
	if c.MaxFileBytes < 1024 || c.MaxFileBytes > 256<<20 {
		return c, fmt.Errorf("max_file_bytes must be between 1024 and 268435456")
	}
	host, _, err := net.SplitHostPort(c.Address)
	if err != nil {
		return c, fmt.Errorf("address: %w", err)
	}
	ip := net.ParseIP(host)
	if (ip == nil || !ip.IsLoopback()) && c.AdminPassword == "" && !c.PublicDemo {
		return c, fmt.Errorf("set FLUXPORT_ADMIN_PASSWORD before binding to a non-loopback address")
	}
	if c.DataDir == "" || c.StateDir == "" {
		return c, fmt.Errorf("data and state directories must not be empty")
	}
	c.DataDir, err = filepath.Abs(c.DataDir)
	if err != nil {
		return c, err
	}
	c.StateDir, err = filepath.Abs(c.StateDir)
	if err != nil {
		return c, err
	}
	if c.DuckDBTempDir != "" {
		c.DuckDBTempDir, err = filepath.Abs(c.DuckDBTempDir)
		if err != nil {
			return c, err
		}
	}
	return c, nil
}
