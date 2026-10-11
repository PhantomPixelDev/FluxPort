package collections

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"
)

type Schedule struct {
	Pipeline string `json:"pipeline"`
	Interval int    `json:"interval_seconds"`
	Format   string `json:"format"`
	Enabled  bool   `json:"enabled"`
	NextRun  int64  `json:"next_run_unix"`
	LastJob  string `json:"last_job,omitempty"`
	Error    string `json:"error,omitempty"`
}

func (s *Service) Schedule(id string) (Schedule, error) {
	out := Schedule{Pipeline: id, Interval: 3600, Format: "ndjson"}
	err := s.DB.QueryRow(`SELECT interval_seconds,format,enabled,next_run,last_job,error FROM pipeline_schedules WHERE pipeline_id=?`, id).Scan(&out.Interval, &out.Format, &out.Enabled, &out.NextRun, &out.LastJob, &out.Error)
	if errors.Is(err, sql.ErrNoRows) {
		return out, nil
	}
	return out, err
}
func (s *Service) SaveSchedule(schedule Schedule) error {
	if _, err := s.GetPipeline(schedule.Pipeline); err != nil {
		return err
	}
	if schedule.Interval < 60 || schedule.Interval > 31536000 {
		return fmt.Errorf("schedule interval must be between 60 and 31536000 seconds")
	}
	if !datasetSupported(schedule.Format) {
		return fmt.Errorf("unsupported schedule output format")
	}
	_, err := s.DB.Exec(`INSERT INTO pipeline_schedules(pipeline_id,interval_seconds,format,enabled,next_run) VALUES(?,?,?,?,?) ON CONFLICT(pipeline_id) DO UPDATE SET interval_seconds=excluded.interval_seconds,format=excluded.format,enabled=excluded.enabled,next_run=excluded.next_run,error=''`, schedule.Pipeline, schedule.Interval, schedule.Format, schedule.Enabled, time.Now().Unix()+int64(schedule.Interval))
	return err
}

// Due schedules enqueue ordinary cancellable jobs. A compare-and-swap claim
// prevents duplicate dispatch; the prior run must have finished first.
func (s *Service) RunDueSchedules(now time.Time) error {
	rows, err := s.DB.Query(`SELECT pipeline_id FROM pipeline_schedules WHERE enabled=1 AND next_run<=? ORDER BY next_run LIMIT 20`, now.Unix())
	if err != nil {
		return err
	}
	ids := []string{}
	for rows.Next() {
		var id string
		if err = rows.Scan(&id); err != nil {
			rows.Close()
			return err
		}
		ids = append(ids, id)
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return err
	}
	for _, id := range ids {
		unlock := s.lock("schedule:" + id)
		err = func() error {
			schedule, e := s.Schedule(id)
			if e != nil {
				return e
			}
			if !schedule.Enabled || schedule.NextRun > now.Unix() {
				return nil
			}
			if schedule.LastJob != "" {
				job, e := s.Jobs.Get(schedule.LastJob)
				if e != nil && !errors.Is(e, sql.ErrNoRows) {
					return e
				}
				if e == nil && (job.State == "queued" || job.State == "running" || job.State == "staged") {
					return nil
				}
			}
			result, e := s.DB.Exec(`UPDATE pipeline_schedules SET next_run=? WHERE pipeline_id=? AND enabled=1 AND next_run=?`, now.Unix()+int64(schedule.Interval), id, schedule.NextRun)
			if e != nil {
				return e
			}
			n, _ := result.RowsAffected()
			if n != 1 {
				return nil
			}
			p, e := s.GetPipeline(id)
			if e != nil {
				s.DB.Exec(`UPDATE pipeline_schedules SET error=? WHERE pipeline_id=?`, e.Error(), id)
				return nil
			}
			job, e := s.PipelineJob(p, schedule.Format)
			if e != nil {
				_, err := s.DB.Exec(`UPDATE pipeline_schedules SET error=? WHERE pipeline_id=?`, e.Error(), id)
				return err
			}
			_, e = s.DB.Exec(`UPDATE pipeline_schedules SET last_job=?,error='' WHERE pipeline_id=?`, job.ID, id)
			return e
		}()
		unlock()
		if err != nil {
			return err
		}
	}
	return nil
}
func (s *Service) startScheduler(ctx context.Context) {
	if s.scheduleCancel != nil {
		return
	}
	ctx, s.scheduleCancel = context.WithCancel(ctx)
	s.scheduleDone = make(chan struct{})
	go func() {
		defer close(s.scheduleDone)
		ticker := time.NewTicker(15 * time.Second)
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case now := <-ticker.C:
				if err := s.RunDueSchedules(now); err != nil {
					_ = s.SetSetting("scheduler_error", err.Error())
				}
			}
		}
	}()
}
