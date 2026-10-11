package web

import (
	"net/url"
	"strings"
	"testing"
)

func TestConverterInspection(t *testing.T) {
	app, _, _ := setup(t, "", "")
	response, body := call(t, app, "GET", "/converter", "", nil, nil)
	token := csrf(t, body)
	cookies := response.Cookies()
	headers := map[string]string{"Content-Type": "application/x-www-form-urlencoded", "HX-Request": "true", "X-CSRF-Token": token}
	form := url.Values{"input": {`{"id":184467440737095516160,"nested":{"value":"hello"}}`}, "from": {"json"}, "to": {"yaml"}, "roundtrip": {"on"}}
	response, body = call(t, app, "POST", "/converter", form.Encode(), headers, cookies)
	for _, expected := range []string{"Conversion report", "normalized values and types match", "Explore output structure", "184467440737095516160", `hx-swap-oob="innerHTML"`} {
		if response.StatusCode != 200 || !strings.Contains(body, expected) {
			t.Fatal(expected, response.StatusCode, body)
		}
	}
	form.Set("to", "csv")
	response, body = call(t, app, "POST", "/converter", form.Encode(), headers, cookies)
	if response.StatusCode != 200 || !strings.Contains(body, "Round-trip differences") || !strings.Contains(body, "nested cells become JSON text") {
		t.Fatal(body)
	}
	form = url.Values{"input": {"id,price\n00123,2.50\n"}, "from": {"csv"}, "to": {"json"}, "column_types": {"id=text\nprice=number"}}
	response, body = call(t, app, "POST", "/converter", form.Encode(), headers, cookies)
	if response.StatusCode != 200 || !strings.Contains(body, "2.50") {
		t.Fatal(body)
	}
	form.Set("column_types", "id=wrong")
	response, body = call(t, app, "POST", "/converter", form.Encode(), headers, cookies)
	if response.StatusCode != 422 || !strings.Contains(body, "choose text") {
		t.Fatal(body)
	}
	form = url.Values{"input": {"{\n \"x\": ]\n}"}, "from": {"json"}, "to": {"yaml"}}
	response, body = call(t, app, "POST", "/converter", form.Encode(), headers, cookies)
	if response.StatusCode != 422 || !strings.Contains(body, "Line 2") || !strings.Contains(body, "double-quoted") {
		t.Fatal(body)
	}
}
