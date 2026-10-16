package transformations

import "testing"

func TestDateExplicitTimezones(t *testing.T) {
	op := Operation{Kind: "date", Field: "at", Layout: "2006-01-02 15:04", OutputLayout: "2006-01-02T15:04:05Z07:00", Timezone: "Europe/Berlin", OutputTimezone: "UTC"}
	out, err := ApplyOperation(map[string]any{"at": "2024-07-01 12:30"}, op)
	if err != nil || out.(map[string]any)["at"] != "2024-07-01T10:30:00Z" {
		t.Fatal(out, err)
	}
	op.Timezone = "invalid/timezone"
	if err = op.Validate(); err == nil {
		t.Fatal("invalid timezone accepted")
	}
}
