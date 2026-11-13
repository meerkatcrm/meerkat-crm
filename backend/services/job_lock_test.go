package services

import (
	"fmt"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"meerkat/clock"
	"meerkat/config"
	"meerkat/database"
	"meerkat/models"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

// contentionRounds repeats each race: one round can pass by luck of scheduling,
// but without BEGIN IMMEDIATE roughly half of all attempts fail.
const contentionRounds = 10

// setupFileDB opens the database the way production does. The usual in-memory
// test database is limited to one connection, so it can never produce a lock error.
func setupFileDB(t *testing.T) *gorm.DB {
	t.Helper()
	db, err := database.InitDB(filepath.Join(t.TempDir(), "test.db"))
	require.NoError(t, err)
	t.Cleanup(func() {
		if sqlDB, err := db.DB(); err == nil {
			sqlDB.Close()
		}
	})
	return db
}

// raceAll runs every fn at once and waits for all of them.
func raceAll(fns ...func()) {
	var wg sync.WaitGroup
	start := make(chan struct{})
	for _, fn := range fns {
		wg.Add(1)
		go func() {
			defer wg.Done()
			<-start
			fn()
		}()
	}
	close(start)
	wg.Wait()
}

func TestAcquireJobLock_ConcurrentJobsAllAcquire(t *testing.T) {
	jobNames := []string{"job_a", "job_b", "job_c", "job_d", "job_e", "job_f"}

	cases := []struct {
		name string
		seed func(t *testing.T, db *gorm.DB)
	}{
		{name: "first run inserts the row", seed: func(*testing.T, *gorm.DB) {}},
		{name: "restart updates the existing row", seed: func(t *testing.T, db *gorm.DB) {
			for _, jobName := range jobNames {
				require.NoError(t, db.Create(&models.JobExecution{
					JobName:   jobName,
					LastRunAt: time.Now().Add(-48 * time.Hour),
				}).Error)
			}
		}},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			for round := 0; round < contentionRounds; round++ {
				db := setupFileDB(t)
				tc.seed(t, db)

				var fns []func()
				for _, jobName := range jobNames {
					fns = append(fns, func() {
						acquired, err := acquireJobLock(db, jobName, time.Hour, time.Now())
						assert.NoError(t, err, jobName)
						assert.True(t, acquired, jobName)
					})
				}
				raceAll(fns...)
			}
		})
	}
}

// A user-facing transaction that reads before it writes must wait for other
// writers rather than fail, and plain writes must not be affected by it.
func TestMergeContacts_SucceedsDuringConcurrentWrites(t *testing.T) {
	for round := 0; round < contentionRounds; round++ {
		db := setupFileDB(t)

		var ids []uint
		for i := 0; i < 8; i++ {
			contact := models.Contact{UserID: 1, Firstname: fmt.Sprintf("Contact%d", i), Lastname: "Test"}
			require.NoError(t, db.Create(&contact).Error)
			ids = append(ids, contact.ID)
		}

		var fns []func()
		for i := 0; i < 4; i++ {
			target, source := ids[i*2], ids[i*2+1]
			fns = append(fns,
				func() {
					_, err := MergeContacts(db, 1, target, source, nil)
					assert.NoError(t, err)
				},
				func() {
					assert.NoError(t, db.Create(&models.Note{UserID: 1, Content: "note", Date: time.Now()}).Error)
				},
			)
		}
		raceAll(fns...)
	}
}

func TestAcquireJobLock_ReportsDatabaseError(t *testing.T) {
	db := setupFileDB(t)
	require.NoError(t, db.Migrator().DropTable(&models.JobExecution{}))

	acquired, err := acquireJobLock(db, "job_a", time.Hour, time.Now())

	assert.Error(t, err)
	assert.False(t, acquired)
}

func TestAcquireJobLock_SkipIsNotAnError(t *testing.T) {
	db := setupFileDB(t)

	acquired, err := acquireJobLock(db, "job_a", time.Hour, time.Now())
	require.NoError(t, err)
	require.True(t, acquired)

	acquired, err = acquireJobLock(db, "job_a", time.Hour, time.Now())
	assert.NoError(t, err)
	assert.False(t, acquired)
}

func TestAcquireJobLock_SkipsWhileAnotherInstanceHoldsLock(t *testing.T) {
	db := setupFileDB(t)
	lockedAt := time.Now()
	require.NoError(t, db.Create(&models.JobExecution{
		JobName:   "job_a",
		LastRunAt: time.Now().Add(-48 * time.Hour),
		LockedAt:  &lockedAt,
		LockedBy:  "other-instance",
	}).Error)

	acquired, err := acquireJobLock(db, "job_a", time.Hour, time.Now())

	assert.NoError(t, err)
	assert.False(t, acquired)
}

// Job locks decide when a job runs, so they follow the wall clock even under a fake
// clock: a last_run_at stamped in the future would block the job once the clock changes.
func TestJobLockIgnoresFakeClock(t *testing.T) {
	db := setupFileDB(t)
	future := clock.NewFake(time.Date(2031, 12, 24, 12, 0, 0, 0, time.UTC))
	cfg := config.Config{ReminderTime: "12:00"}

	require.NoError(t, SendRemindersWithRateLimit(db, cfg, future))

	var job models.JobExecution
	require.NoError(t, db.Where("job_name = ?", models.JobNameDailyReminders).First(&job).Error)
	assert.WithinDuration(t, time.Now(), job.LastRunAt, time.Minute)
}
