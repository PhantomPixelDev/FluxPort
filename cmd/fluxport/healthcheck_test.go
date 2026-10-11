package main

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestHealthcheck(t *testing.T) {
	for _, status := range []int{http.StatusOK, http.StatusServiceUnavailable, http.StatusFound} {
		t.Run(http.StatusText(status), func(t *testing.T) {
			s := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.Header().Set("Location", "/healthz")
				w.WriteHeader(status)
			}))
			defer s.Close()
			if err := healthcheckCommand([]string{"--url", s.URL}); (err == nil) != (status == http.StatusOK) {
				t.Fatalf("status %d: %v", status, err)
			}
		})
	}
	if err := healthcheckCommand([]string{"extra"}); err == nil {
		t.Fatal("unexpected argument accepted")
	}
}
