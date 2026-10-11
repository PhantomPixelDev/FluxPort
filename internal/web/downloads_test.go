package web

import (
	"context"
	"net/url"
	"os"
	"strings"
	"testing"
	"time"

	"fluxport/internal/collections"
)

func TestOriginalDownloadExactBytesAndPending(t *testing.T) {
	app, s, _ := setup(t, "", "")
	input := "[\n  {\"big\": 9007199254740993, \"name\": \"山田\", \"empty\":null}\n]\n"
	col, err := s.Import("original.json", strings.NewReader(input))
	if err != nil {
		t.Fatal(err)
	}
	page, html := call(t, app, "GET", "/collections/"+col.ID, "", nil, nil)
	resp, body := call(t, app, "GET", "/collections/"+col.ID+"/export?original=1", "", nil, page.Cookies())
	if resp.StatusCode != 200 || body != input {
		t.Fatalf("original changed: %d %q", resp.StatusCode, body)
	}
	resp, body = call(t, app, "POST", "/collections/"+col.ID+"/export", url.Values{"csrf": {csrf(t, html)}, "download_view": {"compact"}, "format": {"original"}}.Encode(), map[string]string{"Content-Type": "application/x-www-form-urlencoded"}, page.Cookies())
	if resp.StatusCode != 200 || body != input {
		t.Fatal("native original download failed", resp.StatusCode)
	}
	if _, err = s.DB.Exec(`UPDATE collections SET status='pending' WHERE id=?`, col.ID); err != nil {
		t.Fatal(err)
	}
	resp, body = call(t, app, "GET", "/collections/"+col.ID+"/export?original=1", "", nil, nil)
	if resp.StatusCode != 200 || body != input {
		t.Fatal("pending original unavailable")
	}
	_, html = call(t, app, "GET", "/collections/"+col.ID, "", nil, nil)
	if !strings.Contains(html, "Other formats will be available") {
		t.Fatal("pending state unclear")
	}
}

func TestCompactDownloadCancellationRetryAndNativeNavigation(t *testing.T) {
	app, s, col := setup(t, "", "")
	page, html := call(t, app, "GET", "/collections/"+col.ID, "", nil, nil)
	values := url.Values{"csrf": {csrf(t, html)}, "download_view": {"compact"}, "format": {"xlsx"}}
	headers := map[string]string{"Content-Type": "application/x-www-form-urlencoded", "HX-Request": "true"}
	resp, body := call(t, app, "POST", "/collections/"+col.ID+"/export", values.Encode(), headers, page.Cookies())
	list, _ := s.Jobs.List()
	var id string
	for _, j := range list {
		if j.Kind == "convert" {
			id = j.ID
			break
		}
	}
	if resp.StatusCode != 200 || !strings.Contains(body, `data-download-job="`+id+`"`) || strings.Contains(body, "File activity") {
		t.Fatal("not a compact job", body)
	}
	resp, body = call(t, app, "POST", "/jobs/"+id+"/cancel", values.Encode(), headers, page.Cookies())
	if resp.StatusCode != 200 || !strings.Contains(body, "Download cancelled") {
		t.Fatal("cancel failed", body)
	}
	resp, body = call(t, app, "POST", "/jobs/"+id+"/retry", values.Encode(), headers, page.Cookies())
	if resp.StatusCode != 200 || !strings.Contains(body, "Preparing your download") {
		t.Fatal("retry failed", body)
	}
	delete(headers, "HX-Request")
	resp, _ = call(t, app, "POST", "/collections/"+col.ID+"/export", values.Encode(), headers, page.Cookies())
	if resp.StatusCode != 303 || !strings.Contains(resp.Header.Get("Location"), "download_job=") {
		t.Fatal("native export did not redirect", resp.StatusCode)
	}
	resp, body = call(t, app, "GET", resp.Header.Get("Location"), "", nil, page.Cookies())
	if resp.StatusCode != 200 || !strings.Contains(body, "Refresh download status") {
		t.Fatal("native status page missing")
	}
	values.Set("format", "invalid")
	resp, body = call(t, app, "POST", "/collections/"+col.ID+"/export", values.Encode(), headers, page.Cookies())
	if resp.StatusCode != 200 || !strings.Contains(body, "unsupported format") || !strings.Contains(body, "collection-download-status") {
		t.Fatal("native error page missing", resp.StatusCode)
	}
}

func TestConvertedDownloadsAllFormatsAndErrors(t *testing.T) {
	app, s, _ := setup(t, "", "")
	col, err := s.Import("formats.json", strings.NewReader(`[{"id":1,"name":"山田","nested":{"active":true}}]`))
	if err != nil {
		t.Fatal(err)
	}
	if err = s.Start(context.Background(), 1); err != nil {
		t.Fatal(err)
	}
	defer s.Stop()
	if err = s.Sync(); err != nil {
		t.Fatal(err)
	}
	waitDownloadCollection(t, s, col.ID)
	page, html := call(t, app, "GET", "/collections/"+col.ID, "", nil, nil)
	headers := map[string]string{"Content-Type": "application/x-www-form-urlencoded", "HX-Request": "true"}
	for _, format := range collections.DatasetFormats() {
		t.Run(format, func(t *testing.T) {
			values := url.Values{"csrf": {csrf(t, html)}, "download_view": {"compact"}, "format": {format}}
			resp, body := call(t, app, "POST", "/collections/"+col.ID+"/export", values.Encode(), headers, page.Cookies())
			if resp.StatusCode != 200 {
				t.Fatal(resp.StatusCode, body)
			}
			list, _ := s.Jobs.List()
			var id string
			for _, j := range list {
				if j.Kind == "convert" && strings.HasSuffix(j.Name, " → "+format) {
					id = j.ID
					break
				}
			}
			deadline := time.Now().Add(10 * time.Second)
			for {
				j, e := s.Jobs.Get(id)
				if e != nil {
					t.Fatal(e)
				}
				if j.State == "failed" {
					t.Fatal(j.Error)
				}
				if j.State == "ready" {
					break
				}
				if time.Now().After(deadline) {
					t.Fatal("export timed out")
				}
				time.Sleep(10 * time.Millisecond)
			}
			resp, body = call(t, app, "GET", "/jobs?download_view=compact&id="+id, "", map[string]string{"HX-Request": "true"}, page.Cookies())
			if resp.StatusCode != 200 || !strings.Contains(body, "Your download is ready") {
				t.Fatal("ready status missing", body)
			}
			resp, body = call(t, app, "GET", "/jobs/"+id+"/download", "", nil, page.Cookies())
			if resp.StatusCode != 200 || len(body) == 0 {
				t.Fatal("empty output", resp.StatusCode)
			}
		})
	}
	bad, err := s.Import("nulls.json", strings.NewReader(`[{"id":1,"value":null}]`))
	if err != nil {
		t.Fatal(err)
	}
	waitDownloadCollection(t, s, bad.ID)
	j, err := s.ExportJob(bad.ID, "toml")
	if err != nil {
		t.Fatal(err)
	}
	deadline := time.Now().Add(5 * time.Second)
	for {
		j, _ = s.Jobs.Get(j.ID)
		if j.State == "failed" {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("incompatible conversion accepted")
		}
		time.Sleep(10 * time.Millisecond)
	}
	_, body := call(t, app, "GET", "/jobs?download_view=compact&id="+j.ID, "", map[string]string{"HX-Request": "true"}, page.Cookies())
	if !strings.Contains(body, "Try again") || !strings.Contains(body, "Unable to prepare download") {
		t.Fatal("failure not actionable")
	}
	if j.Result != "" {
		if _, e := os.Stat(j.Result); e == nil {
			t.Fatal("failed output published")
		}
	}
}

func TestOriginalDownloadRequiresAuthentication(t *testing.T) {
	app, _, col := setup(t, "secret-password", "")
	resp, _ := call(t, app, "GET", "/collections/"+col.ID+"/export?original=1", "", nil, nil)
	if resp.StatusCode == 200 {
		t.Fatal("original bypassed authentication")
	}
}

func waitDownloadCollection(t *testing.T, s *collections.Service, id string) {
	t.Helper()
	deadline := time.Now().Add(10 * time.Second)
	for {
		c, err := s.Get(id)
		if err != nil {
			t.Fatal(err)
		}
		if c.Status == "ready" {
			return
		}
		if c.Status == "failed" || time.Now().After(deadline) {
			t.Fatal("index did not finish", c.Error)
		}
		time.Sleep(10 * time.Millisecond)
	}
}
