package services

import (
	"sync"

	"meerkat/logger"
	"meerkat/metrics"
	"meerkat/models"

	"gorm.io/gorm"
)

// Job names used in metric labels.
const (
	JobReminders    = "reminders"
	JobCalendarSync = "calendar-sync"
	JobContactSync  = "contact-sync"
)

// jobMetricName maps the names used in the job_executions table to the scheduler's job
// names, so every job-labelled series uses one scheme.
var jobMetricName = map[string]string{
	models.JobNameDailyReminders: JobReminders,
	models.JobNameCalendarSync:   JobCalendarSync,
	models.JobNameContactSync:    JobContactSync,
}

// RegisterJobMetrics adds gauges built from the job_executions table: the last successful
// run of each job that keeps a row there (reminders, calendar-sync, contact-sync) and
// whether it holds its lock. The table is read once per scrape. Alert on a timestamp that is
// too old. The "running" value follows the lock: a process that died mid-run leaves its lock
// until the next run takes it over after the 5 minute lock timeout, so a crashed run can
// read 1 for a while.
func RegisterJobMetrics(r *metrics.Registry, db *gorm.DB) {
	var (
		mu   sync.Mutex
		jobs []models.JobExecution
	)
	r.OnScrape(func() {
		var rows []models.JobExecution
		if err := db.Order("job_name").Find(&rows).Error; err != nil {
			logger.Warn().Err(err).Msg("Could not read job executions for metrics")
			rows = nil
		}
		mu.Lock()
		jobs = rows
		mu.Unlock()
	})
	snapshot := func() []models.JobExecution {
		mu.Lock()
		defer mu.Unlock()
		return jobs
	}
	name := func(j models.JobExecution) string {
		if n, ok := jobMetricName[j.JobName]; ok {
			return n
		}
		return j.JobName
	}
	r.GaugeFunc("meerkat_job_last_success_timestamp_seconds",
		"Unix time of the last completed run of a job tracked in job_executions (absent until one completed).", []string{"job"},
		func() []metrics.Sample {
			var out []metrics.Sample
			for _, j := range snapshot() {
				if j.LastSuccessAt != nil {
					out = append(out, metrics.Sample{Labels: []string{name(j)}, Value: float64(j.LastSuccessAt.Unix())})
				}
			}
			return out
		})
	r.GaugeFunc("meerkat_job_running",
		"1 while a job tracked in job_executions holds its lock (a crashed run keeps it until taken over, up to 5 minutes).", []string{"job"},
		func() []metrics.Sample {
			var out []metrics.Sample
			for _, j := range snapshot() {
				v := 0.0
				if j.LockedAt != nil {
					v = 1
				}
				out = append(out, metrics.Sample{Labels: []string{name(j)}, Value: v})
			}
			return out
		})
}
