package collections

import (
	"strings"
	"testing"
	"time"
)

func TestSchedulePersistenceNoOverlapAndDisabled(t *testing.T) {
	s := testService(t)
	c, err := s.Import("scheduled.json", strings.NewReader(`[{"id":1}]`))
	if err != nil {
		t.Fatal(err)
	}
	p, err := s.SavePipeline(Pipeline{Name: "Scheduled", Source: c.ID})
	if err != nil {
		t.Fatal(err)
	}
	if err = s.SaveSchedule(Schedule{Pipeline: p.ID, Interval: 60, Format: "json", Enabled: true}); err != nil {
		t.Fatal(err)
	}
	now := time.Now().Add(61 * time.Second)
	if err = s.RunDueSchedules(now); err != nil {
		t.Fatal(err)
	}
	first, err := s.Schedule(p.ID)
	if err != nil || first.LastJob == "" {
		t.Fatal(first, err)
	}
	if err = s.RunDueSchedules(now.Add(time.Hour)); err != nil {
		t.Fatal(err)
	}
	again, _ := s.Schedule(p.ID)
	if again.LastJob != first.LastJob {
		t.Fatal("overlapping run queued", again)
	}
	if err = s.Jobs.Cancel(first.LastJob); err != nil {
		t.Fatal(err)
	}
	if err = s.RunDueSchedules(now.Add(time.Hour)); err != nil {
		t.Fatal(err)
	}
	next, _ := s.Schedule(p.ID)
	if next.LastJob == first.LastJob {
		t.Fatal("completed run prevented next dispatch")
	}
	if err = s.SaveSchedule(Schedule{Pipeline: p.ID, Interval: 60, Format: "json", Enabled: false}); err != nil {
		t.Fatal(err)
	}
	if err = s.RunDueSchedules(now.Add(2 * time.Hour)); err != nil {
		t.Fatal(err)
	}
	disabled, _ := s.Schedule(p.ID)
	if disabled.Enabled || disabled.LastJob != next.LastJob {
		t.Fatal(disabled)
	}
	if err = s.SaveSchedule(Schedule{Pipeline: p.ID, Interval: 1, Format: "json"}); err == nil {
		t.Fatal("invalid interval accepted")
	}
	if err = s.DeletePipeline(p.ID); err != nil {
		t.Fatal(err)
	}
	var count int
	s.DB.QueryRow(`SELECT COUNT(*) FROM pipeline_schedules`).Scan(&count)
	if count != 0 {
		t.Fatal("schedule survived recipe deletion")
	}
}
