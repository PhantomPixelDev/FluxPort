package web

import (
	"bytes"
	"context"
	"io"
	"mime/multipart"
	"net/url"
	"os"
	"strings"
	"testing"
	"time"
)

func TestCollectionConverterAndWorkflow(t *testing.T) {
	app, s, _ := setup(t, "", "")
	col, err := s.Import("nested.json", strings.NewReader(`{"customer":{"address":{"city":"Berlin"}},"orders":[{"id":1}]}`))
	if err != nil {
		t.Fatal(err)
	}
	resp, body := call(t, app, "GET", "/converter?collection="+col.ID, "", nil, nil)
	if resp.StatusCode != 200 || !strings.Contains(body, `value="`+col.ID+`" data-format="json"`) {
		t.Fatal(resp.StatusCode, body)
	}
	headers := map[string]string{"Content-Type": "application/x-www-form-urlencoded", "HX-Request": "true", "X-CSRF-Token": csrf(t, body)}
	form := url.Values{"input_mode": {"collection"}, "collection_id": {col.ID}, "from": {"csv"}, "to": {"yaml"}, "roundtrip": {"on"}}
	resp, body = call(t, app, "POST", "/converter", form.Encode(), headers, resp.Cookies())
	if resp.StatusCode != 200 || !strings.Contains(body, "city: Berlin") || !strings.Contains(body, "values and types match") {
		t.Fatal(resp.StatusCode, body)
	}
	resp, body = call(t, app, "GET", "/workflow?collection="+col.ID, "", nil, nil)
	if resp.StatusCode != 200 || !strings.Contains(body, "/converter?collection="+col.ID) {
		t.Fatal(resp.StatusCode, body)
	}
	resp, body = call(t, app, "GET", "/converter/collections?collection_search=nested", "", nil, nil)
	if resp.StatusCode != 200 || !strings.Contains(body, "nested.json") || strings.Contains(body, "people.json") {
		t.Fatal(resp.StatusCode, body)
	}
}

func TestMultipartLargeCollectionConversion(t *testing.T) {
	app, s, _ := setup(t, "", "")
	input := `[{"note":"` + strings.Repeat("x", 6000) + `","nested":{"value":42}}]`
	col, err := s.Import("large.json", strings.NewReader(input))
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
	indexDeadline := time.Now().Add(5 * time.Second)
	for {
		col, err = s.Get(col.ID)
		if err != nil {
			t.Fatal(err)
		}
		if col.Status == "ready" {
			break
		}
		if time.Now().After(indexDeadline) {
			t.Fatal("index did not finish", col.Status, col.Error)
		}
		time.Sleep(10 * time.Millisecond)
	}
	resp, body := call(t, app, "GET", "/converter?collection="+col.ID, "", nil, nil)
	cookies, token := resp.Cookies(), csrf(t, body)
	var buf bytes.Buffer
	w := multipart.NewWriter(&buf)
	for key, value := range map[string]string{"input_mode": "collection", "collection_id": col.ID, "to": "ndjson", "from": "toml"} {
		w.WriteField(key, value)
	}
	w.Close()
	resp, body = call(t, app, "POST", "/converter", buf.String(), map[string]string{"Content-Type": w.FormDataContentType(), "HX-Request": "true", "X-CSRF-Token": token}, cookies)
	if resp.StatusCode != 200 || !strings.Contains(body, "background") {
		t.Fatal(resp.StatusCode, body)
	}
	jobs, err := s.Jobs.List()
	if err != nil {
		t.Fatal(err)
	}
	for _, job := range jobs {
		if job.Kind != "convert" {
			continue
		}
		deadline := time.Now().Add(5 * time.Second)
		for job.State == "queued" || job.State == "running" {
			if time.Now().After(deadline) {
				t.Fatal("conversion did not finish")
			}
			time.Sleep(10 * time.Millisecond)
			job, err = s.Jobs.Get(job.ID)
			if err != nil {
				t.Fatal(err)
			}
		}
		if job.State != "ready" {
			t.Fatal(job.State, job.Error)
		}
		path, err := s.Download(job.ID)
		if err != nil {
			t.Fatal(err)
		}
		f, err := os.Open(path)
		if err != nil {
			t.Fatal(err)
		}
		data, err := io.ReadAll(f)
		f.Close()
		if err != nil || !bytes.Contains(data, []byte(`"nested":{"value":42}`)) {
			t.Fatal("nested result lost", err)
		}
		return
	}
	t.Fatal("conversion job missing")
}

func TestReadablePreviewSearchAndPageSize(t *testing.T) {
	app, s, _ := setup(t, "", "")
	var b strings.Builder
	b.WriteString("[")
	for i := 0; i < 65; i++ {
		if i > 0 {
			b.WriteByte(',')
		}
		b.WriteString(`{"group":"match","value":1}`)
	}
	b.WriteString("]")
	col, err := s.Import("many.json", strings.NewReader(b.String()))
	if err != nil {
		t.Fatal(err)
	}
	resp, body := call(t, app, "GET", "/collections/"+col.ID+"?q=match&limit=50&page=2", "", map[string]string{"HX-Request": "true"}, nil)
	if resp.StatusCode != 200 || !strings.Contains(body, "65 matching records") || !strings.Contains(body,"Page 2 of 2") || strings.Count(body, `<td data-sheet-cell=`) != 30 || !strings.Contains(body, `value="match"`) {
		t.Fatal(resp.StatusCode, body)
	}
	resp, body = call(t, app, "GET", "/collections/"+col.ID+"?q=absent", "", nil, nil)
	if resp.StatusCode != 200 || !strings.Contains(body, "No matching records") {
		t.Fatal(resp.StatusCode, body)
	}
	// Deadline cancellation must still apply to filtered scans.
	ctx, cancel := context.WithTimeout(context.Background(), time.Nanosecond)
	defer cancel()
	if _, _, err = s.Page(ctx, col, 1, 20, false, "match"); err == nil {
		t.Fatal("expired search context ignored")
	}
}
