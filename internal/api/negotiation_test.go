package api

import (
	"net/http/httptest"
	"testing"

	"github.com/gofiber/fiber/v3"
)

func TestAcceptQualityAndExclusions(t *testing.T) {
	app := fiber.New()
	app.Get("/", func(c fiber.Ctx) error {
		f, err := negotiate(c)
		if err != nil {
			return err
		}
		return c.SendString(f)
	})
	for _, tc := range []struct {
		accept, want string
		status       int
	}{{"application/json;q=0, */*;q=0.5", "csv", 200}, {"application/xml;q=0.7,application/json;q=0.8", "json", 200}, {"application/json;q=0", "", 406}, {"application/yaml", "yaml", 200}, {"text/*", "csv", 200}} {
		req := httptest.NewRequest("GET", "http://localhost/", nil)
		req.Header.Set("Accept", tc.accept)
		resp, err := app.Test(req)
		if err != nil {
			t.Fatal(err)
		}
		defer resp.Body.Close()
		if resp.StatusCode != tc.status {
			t.Fatalf("%s: %d", tc.accept, resp.StatusCode)
		}
		if tc.status == 200 {
			b := make([]byte, len(tc.want))
			resp.Body.Read(b)
			if string(b) != tc.want {
				t.Fatalf("%s: %s", tc.accept, b)
			}
		}
	}
}
