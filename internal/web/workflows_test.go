package web

import (
	"bytes"
	"context"
	"io"
	"mime/multipart"
	"net"
	"net/http"
	"strings"
	"testing"
	"time"
)

func TestBatchPartialFailureAndNativeCSRF(t *testing.T) {
	app, s, _ := setup(t, "", "")
	resp, body := call(t, app, "GET", "/collections", "", nil, nil)
	cookies := resp.Cookies()
	token := csrf(t, body)
	var buf bytes.Buffer
	w := multipart.NewWriter(&buf)
	w.WriteField("csrf", token)
	for _, file := range []struct{ name, data string }{{"good.json", `[{"id":1}]`}, {"broken.json", `[oops`}, {"second.csv", "name\nGrace\n"}} {
		p, _ := w.CreateFormFile("file", file.name)
		io.WriteString(p, file.data)
	}
	w.Close()
	resp, _ = call(t, app, "POST", "/collections/import", buf.String(), map[string]string{"Content-Type": w.FormDataContentType()}, cookies)
	if resp.StatusCode != 303 {
		t.Fatal(resp.StatusCode)
	}
	_, total, e := s.List("", 1, 20)
	if e != nil || total != 3 {
		t.Fatal("successful files lost", total, e)
	}
	jobs, _ := s.Jobs.List()
	fail := 0
	for _, job := range jobs {
		if job.Kind == "import" && job.State == "failed" {
			fail++
		}
	}
	if fail != 1 {
		t.Fatal("missing failed-file result", jobs)
	}
}

func TestStreamUploadBeyondRequestBufferLimit(t *testing.T) {
	app, s, _ := setup(t, "", "")
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	if e := s.Start(ctx, 1); e != nil {
		t.Fatal(e)
	}
	defer s.Stop()
	listener, e := net.Listen("tcp", "127.0.0.1:0")
	if e != nil {
		t.Fatal(e)
	}
	go app.Listener(listener)
	defer app.Shutdown()
	base := "http://" + listener.Addr().String()
	client := &http.Client{Timeout: 20 * time.Second}
	resp, e := client.Get(base + "/collections")
	if e != nil {
		t.Fatal(e)
	}
	cookies := resp.Cookies()
	body, _ := io.ReadAll(resp.Body)
	resp.Body.Close()
	token := csrf(t, string(body))
	r, w := io.Pipe()
	multipartWriter := multipart.NewWriter(w)
	contentType := multipartWriter.FormDataContentType()
	row := `{"payload":"` + strings.Repeat("x", 1000) + `"}` + "\n"
	go func() {
		p, e := multipartWriter.CreateFormFile("file", "stream.ndjson")
		if e != nil {
			w.CloseWithError(e)
			return
		}
		for i := 0; i < 12000; i++ {
			if _, e = io.WriteString(p, row); e != nil {
				w.CloseWithError(e)
				return
			}
		}
		multipartWriter.Close()
		w.Close()
	}()
	req, _ := http.NewRequest("POST", base+"/collections/import", r)
	prefix := "--" + multipartWriter.Boundary() + "\r\nContent-Disposition: form-data; name=\"file\"; filename=\"stream.ndjson\"\r\nContent-Type: application/octet-stream\r\n\r\n"
	suffix := "\r\n--" + multipartWriter.Boundary() + "--\r\n"
	req.ContentLength = int64(len(prefix) + len(suffix) + 12000*len(row))
	req.Header.Set("Content-Type", contentType)
	req.Header.Set("X-CSRF-Token", token)
	req.Header.Set("HX-Request", "true")
	for _, c := range cookies {
		req.AddCookie(c)
	}
	resp, e = client.Do(req)
	if e != nil {
		t.Fatal(e)
	}
	b, _ := io.ReadAll(resp.Body)
	resp.Body.Close()
	if resp.StatusCode != 200 {
		t.Fatalf("stream rejected: %d %s", resp.StatusCode, string(b))
	}
	deadline := time.Now().Add(15 * time.Second)
	for time.Now().Before(deadline) {
		cols, _, _ := s.List("stream", 1, 20)
		if len(cols) == 1 && cols[0].Status == "ready" {
			if cols[0].Rows != 12000 {
				t.Fatal(cols[0].Rows)
			}
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatal("stream index did not finish")
}

func TestPendingAPIAndAuthenticatedJobs(t *testing.T) {
	app, s, col := setup(t, "admin", "secret")
	s.DB.Exec(`UPDATE collections SET status='pending' WHERE id=?`, col.ID)
	resp, body := call(t, app, "GET", "/api/collections/"+col.ID, "", map[string]string{"Authorization": "Bearer secret"}, nil)
	if resp.StatusCode != 503 || resp.Header.Get("Retry-After") != "2" || !strings.Contains(body, "indexed") {
		t.Fatalf("%d %s", resp.StatusCode, body)
	}
	resp, _ = call(t, app, "GET", "/api/jobs", "", nil, nil)
	if resp.StatusCode != 401 {
		t.Fatal("jobs API has no authentication")
	}
	resp, body = call(t, app, "GET", "/api/jobs", "", map[string]string{"Authorization": "Bearer secret"}, nil)
	if resp.StatusCode != 200 || !strings.Contains(body, "jobs") {
		t.Fatal(resp.StatusCode, body)
	}
}
