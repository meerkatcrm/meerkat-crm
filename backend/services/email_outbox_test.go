package services

import (
	"errors"
	"testing"
	"time"

	"meerkat/config"
	"meerkat/models"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

var emailCfg = config.Config{UseSMTP: true, SMTPHost: "h", SMTPFromEmail: "from@example.com"}

func emailDB(t *testing.T) *gorm.DB {
	t.Helper()
	db := webhookDB(t)
	require.NoError(t, db.AutoMigrate(&models.EmailOutbox{}))
	return db
}

// fakeSender replaces the SMTP/Resend call; it returns errs in order, then nil.
func fakeSender(t *testing.T, errs ...error) *[]EmailMessage {
	t.Helper()
	var sent []EmailMessage
	old := emailSender
	emailSender = func(_ config.Config, msg EmailMessage) error {
		sent = append(sent, msg)
		if len(errs) >= len(sent) {
			return errs[len(sent)-1]
		}
		return nil
	}
	t.Cleanup(func() { emailSender = old })
	return &sent
}

func noKick(t *testing.T) {
	t.Helper()
	old := startEmailDelivery
	startEmailDelivery = func(func()) {}
	t.Cleanup(func() { startEmailDelivery = old })
}

func TestQueueEmailWritesRowBeforeSending(t *testing.T) {
	db := emailDB(t)
	noKick(t)
	uid := uint(7)
	require.NoError(t, QueueEmail(db, emailCfg, &uid, EmailMessage{To: "a@example.com", Subject: "s", HTML: "<p>x</p>"}))

	var rows []models.EmailOutbox
	require.NoError(t, db.Find(&rows).Error)
	require.Len(t, rows, 1)
	assert.Equal(t, models.OutboxPending, rows[0].Status)
	assert.Equal(t, "a@example.com", rows[0].ToAddr)
	assert.Equal(t, "<p>x</p>", rows[0].HTML)
	assert.Equal(t, &uid, rows[0].UserID)
	assert.Zero(t, rows[0].Attempts)
}

func TestQueueEmailNoopWithoutChannelOrRecipient(t *testing.T) {
	db := emailDB(t)
	noKick(t)
	require.NoError(t, QueueEmail(db, config.Config{}, nil, EmailMessage{To: "a@example.com", Subject: "s"}))
	require.NoError(t, QueueEmail(db, emailCfg, nil, EmailMessage{To: "", Subject: "s"}))
	var n int64
	db.Model(&models.EmailOutbox{}).Count(&n)
	assert.Zero(t, n)
}

func TestQueueEmailRefusesLineBreaks(t *testing.T) {
	db := emailDB(t)
	noKick(t)
	assert.Error(t, QueueEmail(db, emailCfg, nil, EmailMessage{To: "a@example.com\r\nBcc: x@y.z", Subject: "s"}))
	assert.Error(t, QueueEmail(db, emailCfg, nil, EmailMessage{To: "a@example.com", Subject: "s\nBcc: x"}))
	var n int64
	db.Model(&models.EmailOutbox{}).Count(&n)
	assert.Zero(t, n)
}

func TestProcessEmailOutboxSends(t *testing.T) {
	db := emailDB(t)
	noKick(t)
	sent := fakeSender(t)
	require.NoError(t, QueueEmail(db, emailCfg, nil, EmailMessage{To: "a@example.com", Subject: "s", HTML: "h"}))

	ProcessEmailOutbox(db, emailCfg)

	require.Len(t, *sent, 1)
	assert.Equal(t, EmailMessage{To: "a@example.com", Subject: "s", HTML: "h"}, (*sent)[0])
	var row models.EmailOutbox
	require.NoError(t, db.First(&row).Error)
	assert.Equal(t, models.OutboxDelivered, row.Status)
	assert.Equal(t, 1, row.Attempts)
	assert.NotNil(t, row.SentAt)
	assert.Nil(t, row.LockedAt)

	ProcessEmailOutbox(db, emailCfg)
	assert.Len(t, *sent, 1, "a sent email is never sent again")
}

func TestProcessEmailOutboxBackoffThenDead(t *testing.T) {
	db := emailDB(t)
	noKick(t)
	boom := errors.New("smtp down")
	errs := make([]error, maxOutboxAttempts)
	for i := range errs {
		errs[i] = boom
	}
	sent := fakeSender(t, errs...)
	require.NoError(t, QueueEmail(db, emailCfg, nil, EmailMessage{To: "a@example.com", Subject: "s", HTML: "h"}))

	for n := 1; n <= maxOutboxAttempts; n++ {
		ProcessEmailOutbox(db, emailCfg)
		var row models.EmailOutbox
		require.NoError(t, db.First(&row).Error)
		assert.Equal(t, n, row.Attempts)
		assert.Equal(t, "smtp down", row.LastError)
		if n < maxOutboxAttempts {
			assert.Equal(t, models.OutboxPending, row.Status)
			assert.WithinDuration(t, time.Now().Add(outboxBackoff[n-1]), row.NextAttemptAt, time.Minute)
			// not due yet: nothing happens
			ProcessEmailOutbox(db, emailCfg)
			assert.Len(t, *sent, n)
			require.NoError(t, db.Model(&row).Update("next_attempt_at", time.Now().Add(-time.Second)).Error)
		} else {
			assert.Equal(t, models.OutboxDead, row.Status)
		}
	}
	ProcessEmailOutbox(db, emailCfg)
	assert.Len(t, *sent, maxOutboxAttempts, "a dead email is not tried again")
}

func TestProcessEmailOutboxWaitsWhileEmailIsNotConfigured(t *testing.T) {
	db := emailDB(t)
	noKick(t)
	sent := fakeSender(t)
	require.NoError(t, QueueEmail(db, emailCfg, nil, EmailMessage{To: "a@example.com", Subject: "s", HTML: "h"}))

	ProcessEmailOutbox(db, config.Config{})
	assert.Empty(t, *sent)
	var row models.EmailOutbox
	require.NoError(t, db.First(&row).Error)
	assert.Equal(t, models.OutboxPending, row.Status)
	assert.Zero(t, row.Attempts, "not counted as a try")
}

func TestProcessEmailOutboxResumesAfterCrashButSkipsFreshClaims(t *testing.T) {
	db := emailDB(t)
	noKick(t)
	sent := fakeSender(t)
	past := time.Now().Add(-time.Minute)
	fresh, stale := time.Now(), time.Now().Add(-time.Hour)
	for _, locked := range []*time.Time{&fresh, &stale} {
		require.NoError(t, db.Create(&models.EmailOutbox{ToAddr: "a@example.com", Subject: "s", HTML: "h", Status: models.OutboxPending, NextAttemptAt: past, LockedAt: locked}).Error)
	}
	ProcessEmailOutbox(db, emailCfg)
	assert.Len(t, *sent, 1)
}

func TestClaimEmailIsExclusive(t *testing.T) {
	db := emailDB(t)
	e := models.EmailOutbox{ToAddr: "a", Subject: "s", HTML: "h", Status: models.OutboxPending, NextAttemptAt: time.Now()}
	require.NoError(t, db.Create(&e).Error)
	assert.True(t, claimEmail(db, e))
	assert.False(t, claimEmail(db, e))
}

// The job loaded the row (attempts 0) before the immediate send failed and released it; its
// claim on the old copy must fail, also once the backoff has passed.
func TestClaimEmailRejectsAStaleCopyAfterARelease(t *testing.T) {
	db := emailDB(t)
	noKick(t)
	fakeSender(t, errors.New("down"))
	require.NoError(t, QueueEmail(db, emailCfg, nil, EmailMessage{To: "a@example.com", Subject: "s", HTML: "h"}))
	var jobCopy models.EmailOutbox
	require.NoError(t, db.First(&jobCopy).Error)

	require.True(t, claimEmail(db, jobCopy))
	sendQueuedEmail(db, emailCfg, jobCopy) // fails, released with attempts=1 and a backoff

	assert.False(t, claimEmail(db, jobCopy), "retry time is in the future")
	require.NoError(t, db.Model(&models.EmailOutbox{}).Where("id = ?", jobCopy.ID).Update("next_attempt_at", time.Now().Add(-time.Second)).Error)
	assert.False(t, claimEmail(db, jobCopy), "attempts changed since the copy was loaded")
	var fresh models.EmailOutbox
	require.NoError(t, db.First(&fresh).Error)
	assert.True(t, claimEmail(db, fresh))
}

func TestClaimEmailWaitsForTheRetryTime(t *testing.T) {
	db := emailDB(t)
	e := models.EmailOutbox{ToAddr: "a", Subject: "s", HTML: "h", Status: models.OutboxPending, NextAttemptAt: time.Now().Add(time.Hour)}
	require.NoError(t, db.Create(&e).Error)
	assert.False(t, claimEmail(db, e))
}

func TestProcessEmailOutboxClaimsInsideTheConcurrencyLimit(t *testing.T) {
	db := emailDB(t)
	noKick(t)
	sent := fakeSender(t)
	require.NoError(t, QueueEmail(db, emailCfg, nil, EmailMessage{To: "a@example.com", Subject: "s", HTML: "h"}))
	for i := 0; i < cap(emailSem); i++ {
		emailSem <- struct{}{}
	}
	done := make(chan struct{})
	go func() { ProcessEmailOutbox(db, emailCfg); close(done) }()
	time.Sleep(200 * time.Millisecond)
	var row models.EmailOutbox
	require.NoError(t, db.First(&row).Error)
	assert.Nil(t, row.LockedAt, "no claim while waiting for a slot")
	for i := 0; i < cap(emailSem); i++ {
		<-emailSem
	}
	<-done
	assert.Len(t, *sent, 1)
}

func TestEmailBodyIsClearedOnceDeliveredOrDead(t *testing.T) {
	db := emailDB(t)
	noKick(t)
	fakeSender(t)
	require.NoError(t, QueueEmail(db, emailCfg, nil, EmailMessage{To: "a@example.com", Subject: "s", HTML: "token"}))
	ProcessEmailOutbox(db, emailCfg)
	var row models.EmailOutbox
	require.NoError(t, db.First(&row).Error)
	assert.Equal(t, models.OutboxDelivered, row.Status)
	assert.Empty(t, row.HTML)

	boom := errors.New("down")
	errs := make([]error, maxOutboxAttempts)
	for i := range errs {
		errs[i] = boom
	}
	fakeSender(t, errs...)
	require.NoError(t, db.Delete(&models.EmailOutbox{}, row.ID).Error)
	row = models.EmailOutbox{}
	require.NoError(t, QueueEmail(db, emailCfg, nil, EmailMessage{To: "a@example.com", Subject: "s", HTML: "token"}))
	for n := 1; n <= maxOutboxAttempts; n++ {
		ProcessEmailOutbox(db, emailCfg)
		require.NoError(t, db.Model(&models.EmailOutbox{}).Where("1 = 1").Update("next_attempt_at", time.Now().Add(-time.Second)).Error)
	}
	require.NoError(t, db.First(&row).Error)
	assert.Equal(t, models.OutboxDead, row.Status)
	assert.Empty(t, row.HTML)
	assert.Equal(t, "a@example.com", row.ToAddr)
}

func TestExpiredEmailIsNeverSentAndLosesItsBody(t *testing.T) {
	db := emailDB(t)
	noKick(t)
	sent := fakeSender(t)
	require.NoError(t, QueueEmailUntil(db, emailCfg, nil, EmailMessage{To: "a@example.com", Subject: "s", HTML: "token"}, time.Now().Add(-time.Minute)))

	ProcessEmailOutbox(db, emailCfg)

	assert.Empty(t, *sent)
	var row models.EmailOutbox
	require.NoError(t, db.First(&row).Error)
	assert.Equal(t, models.OutboxDead, row.Status)
	assert.Empty(t, row.HTML)
	assert.Contains(t, row.LastError, "expired")
	assert.Zero(t, row.Attempts)
}

func TestEmailStopsRetryingWhenTheNextTryWouldBeAfterExpiry(t *testing.T) {
	db := emailDB(t)
	noKick(t)
	sent := fakeSender(t, errors.New("down"), errors.New("down"))
	// first retry (1 min) fits, the second (5 min) does not
	require.NoError(t, QueueEmailUntil(db, emailCfg, nil, EmailMessage{To: "a@example.com", Subject: "s", HTML: "token"}, time.Now().Add(2*time.Minute)))

	ProcessEmailOutbox(db, emailCfg)
	var row models.EmailOutbox
	require.NoError(t, db.First(&row).Error)
	assert.Equal(t, models.OutboxPending, row.Status)
	assert.Equal(t, "token", row.HTML)

	require.NoError(t, db.Model(&row).Update("next_attempt_at", time.Now().Add(-time.Second)).Error)
	ProcessEmailOutbox(db, emailCfg)
	require.NoError(t, db.First(&row).Error)
	assert.Len(t, *sent, 2)
	assert.Equal(t, models.OutboxDead, row.Status)
	assert.Empty(t, row.HTML)
}

func TestPurgeEmailOutbox(t *testing.T) {
	db := emailDB(t)
	now := time.Now()
	mk := func(status string, age time.Duration) uint {
		e := models.EmailOutbox{ToAddr: "a", Subject: "s", HTML: "h", Status: status, NextAttemptAt: now, CreatedAt: now.Add(-age), UpdatedAt: now.Add(-age)}
		require.NoError(t, db.Create(&e).Error)
		require.NoError(t, db.Model(&e).UpdateColumn("updated_at", now.Add(-age)).Error)
		return e.ID
	}
	oldSent := mk(models.OutboxDelivered, 8*24*time.Hour)
	newSent := mk(models.OutboxDelivered, time.Hour)
	oldDead := mk(models.OutboxDead, 31*24*time.Hour)
	newDead := mk(models.OutboxDead, 10*24*time.Hour)
	oldPending := mk(models.OutboxPending, 60*24*time.Hour)

	n, err := PurgeEmailOutbox(db, now)
	require.NoError(t, err)
	assert.Equal(t, int64(2), n)
	var ids []uint
	db.Model(&models.EmailOutbox{}).Pluck("id", &ids)
	assert.ElementsMatch(t, []uint{newSent, newDead, oldPending}, ids)
	assert.NotContains(t, ids, oldSent)
	assert.NotContains(t, ids, oldDead)
}
