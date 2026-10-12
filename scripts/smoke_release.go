//go:build ignore

// Exercise the packaged executable, including its bundled analytical engine.
package main

import (
	"bytes"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"time"
)

func main() {
	if err := run(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
func run() error {
	binary := flag.String("binary", "", "executable to verify")
	version := flag.String("version", "", "expected version")
	flag.Parse()
	path, err := filepath.Abs(*binary)
	if err != nil || *binary == "" {
		return fmt.Errorf("provide --binary")
	}
	root, err := os.MkdirTemp("", "fluxport-release-")
	if err != nil {
		return err
	}
	defer os.RemoveAll(root)
	env := []string{}
	for _, value := range os.Environ() {
		if !strings.HasPrefix(strings.ToUpper(value), "FLUXPORT_") {
			env = append(env, value)
		}
	}
	if runtime.GOOS == "windows" {
		clean := []string{}
		for _, value := range env {
			if !strings.HasPrefix(strings.ToUpper(value), "PATH=") {
				clean = append(clean, value)
			}
		}
		env = append(clean, "PATH="+filepath.Join(os.Getenv("SystemRoot"), "System32")+";"+os.Getenv("SystemRoot"))
	}
	command := func(args ...string) *exec.Cmd { c := exec.Command(path, args...); c.Dir = root; c.Env = env; return c }
	output, err := command("version").CombinedOutput()
	if err != nil {
		return fmt.Errorf("packaged executable failed: %w: %s", err, output)
	}
	if *version != "" && !strings.Contains(string(output), "FluxPort "+*version) {
		return fmt.Errorf("unexpected version: %s", output)
	}
	if output, err = command("fixtures", "--suite", "smoke", "--out", filepath.Join(root, "data"), "--seed", "42").CombinedOutput(); err != nil {
		return fmt.Errorf("fixtures: %w: %s", err, output)
	}
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return err
	}
	address := listener.Addr().String()
	listener.Close()
	env = append(env, "FLUXPORT_ADDRESS="+address, "FLUXPORT_DATA_DIR="+filepath.Join(root, "data"), "FLUXPORT_STATE_DIR="+filepath.Join(root, "state"), "FLUXPORT_ADMIN_PASSWORD=", "FLUXPORT_API_KEY=release-smoke-key", "FLUXPORT_PUBLIC_DEMO=false")
	server := command("serve")
	server.Stdout = io.Discard
	server.Stderr = io.Discard
	if err = server.Start(); err != nil {
		return err
	}
	defer func() { server.Process.Kill(); server.Wait() }()
	client := &http.Client{Timeout: 10 * time.Second}
	base := "http://" + address
	deadline := time.Now().Add(60 * time.Second)
	var id string
	var count int64
	for time.Now().Before(deadline) {
		req, _ := http.NewRequest("GET", base+"/api/collections", nil)
		req.Header.Set("Authorization", "Bearer release-smoke-key")
		resp, e := client.Do(req)
		if e == nil {
			var data struct {
				Collections []struct {
					ID, Format, Status string
					Rows               int64
				}
			}
			json.NewDecoder(resp.Body).Decode(&data)
			resp.Body.Close()
			for _, col := range data.Collections {
				if col.Format == "csv" && col.Status == "ready" && col.Rows == 100 {
					id, count = col.ID, col.Rows
					break
				}
			}
		}
		if id != "" {
			break
		}
		time.Sleep(100 * time.Millisecond)
	}
	if id == "" {
		return fmt.Errorf("server did not index release fixtures")
	}
	if output, err = command("healthcheck", "--url", base+"/healthz").CombinedOutput(); err != nil {
		return fmt.Errorf("healthcheck: %w: %s", err, output)
	}
	body, _ := json.Marshal(map[string]any{"collections": []string{id}, "engine": "duckdb", "sql": "SELECT COUNT(*) FROM \"c_" + id + "\""})
	req, _ := http.NewRequest("POST", base+"/api/sql", bytes.NewReader(body))
	req.Header.Set("Authorization", "Bearer release-smoke-key")
	req.Header.Set("Content-Type", "application/json")
	resp, err := client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	var result struct {
		Engine string
		Rows   [][]json.Number
	}
	if err = json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return fmt.Errorf("DuckDB response: %w", err)
	}
	if resp.StatusCode != 200 || result.Engine != "DuckDB" || len(result.Rows) != 1 || len(result.Rows[0]) != 1 || result.Rows[0][0].String() != fmt.Sprint(count) {
		return fmt.Errorf("DuckDB release verification failed: HTTP %d, %+v", resp.StatusCode, result)
	}
	fmt.Printf("Release smoke passed: version, fixtures, startup, health and DuckDB count (%d rows).\n", count)
	return nil
}
