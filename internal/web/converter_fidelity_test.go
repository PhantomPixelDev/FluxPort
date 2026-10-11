package web

import (
	"encoding/json"
	"html"
	"net/url"
	"regexp"
	"strings"
	"testing"
)

func TestConverterOptionsAndNativeInput(t *testing.T) {
	app, _, _ := setup(t, "", "")
	resp, body := call(t, app, "GET", "/converter", "", nil, nil)
	cookies := resp.Cookies()
	token := csrf(t, body)
	headers := map[string]string{"Content-Type": "application/x-www-form-urlencoded", "HX-Request": "true", "X-CSRF-Token": token}
	extract := func(body string) string {
		t.Helper()
		m := regexp.MustCompile(`(?s)<textarea id="convert-output"[^>]*>(.*?)</textarea>`).FindStringSubmatch(body)
		if len(m) != 2 {
			t.Fatal("missing output", body)
		}
		return html.UnescapeString(m[1])
	}
	form := url.Values{"input": {"id;price;active\n00123;2.50;true\n"}, "from": {"csv"}, "to": {"json"}, "infer_types": {"on"}, "csv_delimiter": {"auto"}}
	resp, body = call(t, app, "POST", "/converter", form.Encode(), headers, cookies)
	if resp.StatusCode != 200 {
		t.Fatal(resp.StatusCode, body)
	}
	var got []map[string]any
	d := json.NewDecoder(strings.NewReader(extract(body)))
	d.UseNumber()
	if e := d.Decode(&got); e != nil {
		t.Fatal(e)
	}
	if got[0]["id"] != "00123" || got[0]["price"] != json.Number("2.50") || got[0]["active"] != true {
		t.Fatal(got)
	}
	form = url.Values{"input": {`{"nested":{"note":"=SUM(1)","list":[1,{"x":2}]},"text":"=SUM(1)"}`}, "from": {"json"}, "to": {"csv"}, "spreadsheet_safe": {"on"}}
	resp, body = call(t, app, "POST", "/converter", form.Encode(), headers, cookies)
	if resp.StatusCode != 200 || !strings.Contains(extract(body), "'=SUM(1)") || !strings.Contains(body, "flat text format") {
		t.Fatal(body)
	}
	form = url.Values{"input": {"[]"}, "from": {"json"}, "to": {"ndjson"}}
	resp, body = call(t, app, "POST", "/converter", form.Encode(), headers, cookies)
	if resp.StatusCode != 200 || !strings.Contains(body, `data-ready="true"`) || extract(body) != "" {
		t.Fatal(body)
	}
	native := map[string]string{"Content-Type": "application/x-www-form-urlencoded"}
	form = url.Values{"input": {"date: 2024-01-01"}, "from": {"yaml"}, "to": {"json"}, "csrf": {token}}
	resp, body = call(t, app, "POST", "/converter", form.Encode(), native, cookies)
	if resp.StatusCode != 200 || !strings.Contains(body, "date: 2024-01-01") || !strings.Contains(body, `value="yaml" selected`) {
		t.Fatal(body)
	}
	form.Set("input", "date: [")
	resp, body = call(t, app, "POST", "/converter", form.Encode(), native, cookies)
	if resp.StatusCode != 422 || !strings.Contains(body, "date: [") || !strings.Contains(body, "Parsing error") {
		t.Fatal(body)
	}
}
