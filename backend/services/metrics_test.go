package services

import (
	"strconv"
	"strings"
	"testing"
	"time"

	"meerkat/config"
	"meerkat/metrics"
	"meerkat/models"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

func scrapeDefault() string {
	var sb strings.Builder
	metrics.Default.Write(&sb)
	return sb.String()
}

// series returns the value of the exact series line in the default registry (0 if absent).
func series(t *testing.T, name string) float64 {
	t.Helper()
	for _, l := range strings.Split(scrapeDefault(), "\n") {
		if v, ok := strings.CutPrefix(l, name+" "); ok {
			f, err := strconv.ParseFloat(v, 64)
			require.NoError(t, err)
			return f
		}
	}
	return 0
}

func TestJobMetricsReadJobExecutions(t *testing.T) {
	db := webhookDB(t)
	require.NoError(t, db.AutoMigrate(&models.JobExecution{}))
	reg := metrics.NewRegistry()
	RegisterJobMetrics(reg, db)

	at := time.Unix(1700000000, 0)
	ok := time.Unix(1600000000, 0)
	// started but never completed: LastRunAt is set, no success time
	require.NoError(t, db.Create(&models.JobExecution{JobName: models.JobNameDailyReminders, LastRunAt: at}).Error)
	locked := at.Add(time.Hour)
	require.NoError(t, db.Create(&models.JobExecution{JobName: models.JobNameCalendarSync, LastRunAt: at, LastSuccessAt: &ok, LockedAt: &locked}).Error)

	var sb strings.Builder
	reg.Write(&sb)
	out := sb.String()
	assert.Contains(t, out, `meerkat_job_last_success_timestamp_seconds{job="calendar-sync"} 1.6e+09`, "scheduler job names, success time not run time")
	assert.NotContains(t, out, `last_success_timestamp_seconds{job="reminders"}`, "no success yet, no sample")
	assert.Contains(t, out, `meerkat_job_running{job="calendar-sync"} 1`)
	assert.Contains(t, out, `meerkat_job_running{job="reminders"} 0`)
	assert.NotContains(t, out, "daily_reminders")
}

func TestJobMetricsQueryOncePerScrape(t *testing.T) {
	db := webhookDB(t)
	require.NoError(t, db.AutoMigrate(&models.JobExecution{}))
	reg := metrics.NewRegistry()
	RegisterJobMetrics(reg, db)
	var queries int
	require.NoError(t, db.Callback().Query().Before("gorm:query").Register("count_job_queries", func(tx *gorm.DB) {
		if tx.Statement.Table == "job_executions" {
			queries++
		}
	}))
	var sb strings.Builder
	reg.Write(&sb)
	assert.Equal(t, 1, queries)
}

func TestReleaseJobLockRecordsSuccessTime(t *testing.T) {
	db := webhookDB(t)
	require.NoError(t, db.AutoMigrate(&models.JobExecution{}))
	old := time.Now().Add(-48 * time.Hour)
	require.NoError(t, db.Create(&models.JobExecution{JobName: "j", LastRunAt: old, LockedBy: getInstanceID(), LockedAt: &old}).Error)

	require.NoError(t, releaseJobLock(db, "j", false))
	var j models.JobExecution
	require.NoError(t, db.Where("job_name = ?", "j").First(&j).Error)
	assert.Nil(t, j.LastSuccessAt, "a failed run is not a success")

	require.NoError(t, db.Model(&j).Updates(map[string]any{"locked_by": getInstanceID(), "locked_at": old}).Error)
	require.NoError(t, releaseJobLock(db, "j", true))
	require.NoError(t, db.Where("job_name = ?", "j").First(&j).Error)
	require.NotNil(t, j.LastSuccessAt)
	assert.WithinDuration(t, time.Now(), *j.LastSuccessAt, time.Minute)
}

func TestRateLimitedJobsCountSkippedNotSuccess(t *testing.T) {
	db := webhookDB(t)
	require.NoError(t, db.AutoMigrate(&models.JobExecution{}))
	// ran a minute ago: every wrapper must skip
	for _, name := range []string{models.JobNameDailyReminders, models.JobNameCalendarSync, models.JobNameContactSync} {
		require.NoError(t, db.Create(&models.JobExecution{JobName: name, LastRunAt: time.Now().Add(-time.Minute)}).Error)
	}
	cfg := config.Config{CalDAVSyncIntervalHours: 1, CardDAVSyncIntervalHours: 1}
	for job, run := range map[string]func(){
		JobReminders:    func() { _ = SendRemindersWithRateLimit(db, cfg) },
		JobCalendarSync: func() { SyncCalendarsWithRateLimit(db, cfg) },
		JobContactSync:  func() { SyncContactsWithRateLimit(db, cfg) },
	} {
		skipped := `meerkat_job_runs_total{job="` + job + `",result="skipped"}`
		success := `meerkat_job_runs_total{job="` + job + `",result="success"}`
		s0, ok0 := series(t, skipped), series(t, success)
		run()
		assert.Equal(t, s0+1, series(t, skipped), job)
		assert.Equal(t, ok0, series(t, success), job)
	}
}

func TestWebhookDeliveryCountsResults(t *testing.T) {
	db := webhookDB(t)
	fail := `meerkat_webhook_deliveries_total{result="failure"}`
	ok := `meerkat_webhook_deliveries_total{result="success"}`
	f0, s0 := series(t, fail), series(t, ok)

	msg := "boom"
	finishWebhookDelivery(db, models.WebhookDelivery{}, 1, nil, &msg, false)
	code := 500
	finishWebhookDelivery(db, models.WebhookDelivery{}, 2, &code, &msg, false)
	code = 204
	finishWebhookDelivery(db, models.WebhookDelivery{}, 1, &code, nil, false)

	assert.Equal(t, f0+2, series(t, fail))
	assert.Equal(t, s0+1, series(t, ok))
}

func TestSendEmailCountsResults(t *testing.T) {
	fail := `meerkat_emails_total{result="failure"}`
	ok := `meerkat_emails_total{result="success"}`
	f0, s0 := series(t, fail), series(t, ok)

	// Nothing listens on port 1, so the SMTP channel fails.
	cfg := config.Config{UseSMTP: true, SMTPHost: "127.0.0.1", SMTPPort: 1, SMTPFromEmail: "from@example.com"}
	require.Error(t, SendEmail(cfg, EmailMessage{To: "a@example.com", Subject: "s", HTML: "x"}))
	assert.Equal(t, f0+1, series(t, fail))

	// No channel configured: nothing was sent, nothing is counted.
	require.NoError(t, SendEmail(config.Config{}, EmailMessage{To: "a@example.com", Subject: "s"}))
	assert.Equal(t, f0+1, series(t, fail))
	assert.Equal(t, s0, series(t, ok))
}
