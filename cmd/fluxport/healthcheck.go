package main

import (
	"flag"
	"fmt"
	"io"
	"net/http"
	"time"
)

func healthcheckCommand(args []string) error {
	f := flag.NewFlagSet("healthcheck", flag.ContinueOnError)
	url := f.String("url", "http://127.0.0.1:8080/healthz", "health endpoint")
	if err := f.Parse(args); err != nil {
		return err
	}
	if f.NArg() != 0 {
		return fmt.Errorf("unexpected healthcheck arguments")
	}
	client := &http.Client{Timeout: 3 * time.Second, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
	resp, err := client.Get(*url)
	if err != nil {
		return fmt.Errorf("healthcheck: %w", err)
	}
	defer resp.Body.Close()
	_, _ = io.Copy(io.Discard, io.LimitReader(resp.Body, 4096))
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("healthcheck: HTTP %d", resp.StatusCode)
	}
	return nil
}
