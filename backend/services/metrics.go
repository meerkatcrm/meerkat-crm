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

// RegisterOutboxMetrics adds gauges read on every scrape: how many outbox entries wait for
// their (next) try and how many are dead, per kind. A growing dead count means a receiver
// or the mail server has been failing for longer than the retries cover.
func RegisterOutboxMetrics(r *metrics.Registry, db *gorm.DB) {
	count := func(table, status string) float64 {
		var n int64
		q := db.Table(table).Where(table+".status = ?", status)
		if table == "webhook_deliveries" {
			// Only what somebody can still act on: not soft-deleted rows, not deliveries
			// of deleted or disabled webhooks (they are purged later).
			q = q.Joins("JOIN webhooks ON webhooks.id = webhook_deliveries.webhook_id").
				Where("webhook_deliveries.deleted_at IS NULL AND webhooks.deleted_at IS NULL AND webhooks.is_active = ?", true)
		}
		if err := q.Count(&n).Error; err != nil {
			logger.Warn().Err(err).Str("table", table).Msg("Could not count outbox entries for metrics")
		}
		return float64(n)
	}
	for _, g := range []struct{ name, help, status string }{
		{"meerkat_outbox_pending", "Outbox entries waiting for their first try or a retry.", models.OutboxPending},
		{"meerkat_outbox_dead", "Outbox entries that gave up (dead letter) and are still stored.", models.OutboxDead},
	} {
		g := g
		r.GaugeFunc(g.name, g.help, []string{"kind"}, func() []metrics.Sample {
			return []metrics.Sample{
				{Labels: []string{"webhook"}, Value: count("webhook_deliveries", g.status)},
				{Labels: []string{"email"}, Value: count("email_outbox", g.status)},
			}
		})
	}
}
