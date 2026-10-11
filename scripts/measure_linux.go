//go:build linux

// Standalone measurement tool: go build -o measure scripts/measure_linux.go
package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"time"
)

func rss(pid int) (uint64, uint64) {
	b, _ := os.ReadFile(fmt.Sprintf("/proc/%d/status", pid))
	values := map[string]uint64{}
	for _, line := range strings.Split(string(b), "\n") {
		parts := strings.Fields(line)
		if len(parts) > 1 && (parts[0] == "VmRSS:" || parts[0] == "VmHWM:") {
			n, _ := strconv.ParseUint(parts[1], 10, 64)
			values[parts[0]] = n * 1024
		}
	}
	return values["VmRSS:"], values["VmHWM:"]
}
func main() {
	if err := run(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
func run() error {
	binary := flag.String("binary", "dist/linux-amd64/fluxport", "release executable")
	report := flag.String("report", "artifacts/performance-linux.json", "report path")
	flag.Parse()
	abs, err := filepath.Abs(*binary)
	if err != nil {
		return err
	}
	root, err := os.MkdirTemp("", "fluxport-measure-")
	if err != nil {
		return err
	}
	defer os.RemoveAll(root)
	client := &http.Client{Timeout: 2 * time.Second}
	runs := []map[string]any{}
	assets := []map[string]any{}
	for iteration := 0; iteration < 5; iteration++ {
		listener, err := net.Listen("tcp", "127.0.0.1:0")
		if err != nil {
			return err
		}
		address := listener.Addr().String()
		listener.Close()
		cmd := exec.Command(abs, "serve")
		env := []string{}
		for _, v := range os.Environ() {
			if !strings.HasPrefix(v, "FLUXPORT_") {
				env = append(env, v)
			}
		}
		cmd.Env = append(env, "FLUXPORT_ADDRESS="+address, "FLUXPORT_DATA_DIR="+filepath.Join(root, fmt.Sprintf("data-%d", iteration)), "FLUXPORT_STATE_DIR="+filepath.Join(root, fmt.Sprintf("state-%d", iteration)))
		start := time.Now()
		if err = cmd.Start(); err != nil {
			return err
		}
		stop := func() { cmd.Process.Signal(syscall.SIGTERM); cmd.Wait() }
		ready := false
		for time.Since(start) < 10*time.Second {
			response, e := client.Get("http://" + address + "/healthz")
			if e == nil {
				response.Body.Close()
				if response.StatusCode == 200 {
					ready = true
					break
				}
			}
			time.Sleep(10 * time.Millisecond)
		}
		if !ready {
			stop()
			return fmt.Errorf("health timeout")
		}
		startup := time.Since(start)
		time.Sleep(time.Second)
		idle, peak := rss(cmd.Process.Pid)
		runs = append(runs, map[string]any{"startup_ms": float64(startup.Microseconds()) / 1000, "idle_rss_bytes": idle, "peak_rss_bytes": peak})
		if iteration == 4 {
			for _, path := range []string{"/", "/static/css/app.css", "/static/js/htmx.min.js", "/static/js/app.js", "/static/icons/icons.svg", "/static/icons/logo.svg"} {
				req, _ := http.NewRequest("GET", "http://"+address+path, nil)
				req.Header.Set("Accept-Encoding", "gzip")
				resp, e := client.Do(req)
				if e != nil {
					stop()
					return e
				}
				b, e := io.ReadAll(resp.Body)
				resp.Body.Close()
				if e != nil {
					stop()
					return e
				}
				assets = append(assets, map[string]any{"path": path, "transfer_bytes": len(b), "encoding": resp.Header.Get("Content-Encoding")})
			}
		}
		stop()
	}
	info, err := os.Stat(abs)
	if err != nil {
		return err
	}
	kernel, _ := exec.Command("uname", "-sr").Output()
	result := map[string]any{"measured_at": time.Now().UTC().Format(time.RFC3339), "platform": strings.TrimSpace(string(kernel)), "binary_bytes": info.Size(), "dataset": "Empty source folder, fresh metadata database each run", "memory_method": "Linux /proc VmRSS and VmHWM at one second after health", "startup_method": "Process launch to first HTTP health response; 10 ms retries", "runs": runs, "frontend_gzip": assets}
	b, err := json.MarshalIndent(result, "", "  ")
	if err != nil {
		return err
	}
	if err = os.MkdirAll(filepath.Dir(*report), 0700); err != nil {
		return err
	}
	if err = os.WriteFile(*report, b, 0600); err != nil {
		return err
	}
	fmt.Println(string(b))
	return nil
}
