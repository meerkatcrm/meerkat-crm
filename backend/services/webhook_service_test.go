package services

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"meerkat/clock"
	"meerkat/config"
	"meerkat/models"

	"github.com/glebarez/sqlite"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

func setupWebhookTestDB(t *testing.T) *gorm.DB {
	t.Helper()
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	if err != nil {
		t.Fatalf("failed to open test db: %v", err)
	}
	if err := db.AutoMigrate(&models.Webhook{}, &models.WebhookDelivery{}); err != nil {
		t.Fatalf("failed to migrate: %v", err)
	}
	return db
}

func TestDeliverWebhook_GuardBlocksLoopbackAtDialTime(t *testing.T) {
	hit := false
	target := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { hit = true }))
	defer target.Close()

	d := attempt(t, webhookDB(t), config.Config{WebhookBlockPrivateURLs: true},
		models.Webhook{URL: target.URL, Secret: "s"}, []byte("{}"))

	assert.False(t, hit)
	if assert.NotNil(t, d.Error) {
		assert.Equal(t, webhookErrPrivateBlocked, *d.Error)
	}
	assert.Nil(t, d.NextRetryAt)
}

func TestGuardedDeliveryClient_RefusesLoopback(t *testing.T) {
	resp, err := guardedDeliveryClient.Get("http://127.0.0.1:1/")
	if resp != nil {
		_ = resp.Body.Close()
	}
	assert.ErrorIs(t, err, errWebhookPrivateAddress)
}

func TestDeliverWebhook_HidesTransportErrorDetail(t *testing.T) {
	target := httptest.NewServer(http.NotFoundHandler())
	url := target.URL
	target.Close() // nothing listens on this port any more

	d := attempt(t, webhookDB(t), config.Config{},
		models.Webhook{URL: url, Secret: "s"}, []byte("{}"))

	if assert.NotNil(t, d.Error) {
		assert.Equal(t, webhookErrDeliveryFailed, *d.Error)
		assert.False(t, strings.Contains(*d.Error, "refused"))
	}
	assert.NotNil(t, d.NextRetryAt)
}

func TestGuardedDeliveryClient_RefusesCarrierGradeNAT(t *testing.T) {
	resp, err := guardedDeliveryClient.Get("http://100.100.100.200/")
	if resp != nil {
		_ = resp.Body.Close()
	}
	assert.ErrorIs(t, err, errWebhookPrivateAddress)
}

func webhookDB(t *testing.T) *gorm.DB {
	t.Helper()
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	require.NoError(t, err)
	sqlDB, err := db.DB()
	require.NoError(t, err)
	sqlDB.SetMaxOpenConns(1)
	require.NoError(t, db.AutoMigrate(&models.User{}, &models.Webhook{}, &models.WebhookDelivery{}))
	// The outbox only delivers for webhooks whose owner still exists.
	require.NoError(t, db.Create(&models.User{Model: gorm.Model{ID: 1}, Username: "owner", Email: "owner@example.com", Password: "x"}).Error)
	return db
}

func TestComputeSignatureIsHMACSHA256Hex(t *testing.T) {
	body := []byte(`{"a":1}`)
	mac := hmac.New(sha256.New, []byte("s3cret"))
	mac.Write(body)
	assert.Equal(t, hex.EncodeToString(mac.Sum(nil)), computeSignature("s3cret", body))
	assert.NotEqual(t, computeSignature("s3cret", body), computeSignature("other", body))
}

// attempt enqueues a delivery and runs one try on it, like the outbox worker does.
func attempt(t *testing.T, db *gorm.DB, cfg config.Config, wh models.Webhook, body []byte) models.WebhookDelivery {
	t.Helper()
	d, err := enqueueWebhookDelivery(db, wh.ID, "e", body, time.Now())
	require.NoError(t, err)
	require.True(t, claimWebhookDelivery(db, d, time.Now()))
	return deliverWebhook(db, cfg, wh, d)
}

func TestDeliverWebhookSignsAndRecordsSuccess(t *testing.T) {
	db := webhookDB(t)
	var gotSig, gotEvent, gotType string
	var gotBody []byte
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotSig, gotEvent, gotType = r.Header.Get("X-Webhook-Signature"), r.Header.Get("X-Meerkat-Event"), r.Header.Get("Content-Type")
		gotBody, _ = io.ReadAll(r.Body)
		w.WriteHeader(http.StatusNoContent)
	}))
	defer srv.Close()

	wh := models.Webhook{UserID: 1, URL: srv.URL, Secret: "s3cret"}
	require.NoError(t, db.Create(&wh).Error)
	body := []byte(`{"event":"contact.created"}`)
	d, err := enqueueWebhookDelivery(db, wh.ID, "contact.created", body, time.Now())
	require.NoError(t, err)
	assert.Equal(t, models.OutboxPending, d.Status)
	assert.Zero(t, d.Attempts)
	require.True(t, claimWebhookDelivery(db, d, time.Now()))
	d = deliverWebhook(db, config.Config{}, wh, d)

	assert.Equal(t, "sha256="+computeSignature("s3cret", body), gotSig)
	assert.Equal(t, "contact.created", gotEvent)
	assert.Equal(t, "application/json", gotType)
	assert.Equal(t, body, gotBody)
	require.NotNil(t, d.StatusCode)
	assert.Equal(t, 204, *d.StatusCode)
	assert.Nil(t, d.Error)
	assert.Nil(t, d.NextRetryAt)
	assert.Equal(t, models.OutboxDelivered, d.Status)
	assert.Equal(t, 1, d.Attempts)

	var stored models.WebhookDelivery
	require.NoError(t, db.First(&stored, d.ID).Error)
	assert.Equal(t, models.OutboxDelivered, stored.Status, "the row itself is updated")
	assert.Nil(t, stored.LockedAt)
}

func TestDeliverWebhookBackoffThenDead(t *testing.T) {
	db := webhookDB(t)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(500) }))
	defer srv.Close()
	wh := models.Webhook{UserID: 1, URL: srv.URL, Secret: "s"}
	require.NoError(t, db.Create(&wh).Error)

	d, err := enqueueWebhookDelivery(db, wh.ID, "e", []byte("{}"), time.Now())
	require.NoError(t, err)
	for n := 1; n < maxOutboxAttempts; n++ {
		require.True(t, claimWebhookDelivery(db, d, time.Now()))
		d = deliverWebhook(db, config.Config{}, wh, d)
		assert.Equal(t, n, d.Attempts)
		assert.Equal(t, models.OutboxPending, d.Status)
		require.NotNil(t, d.NextRetryAt)
		assert.WithinDuration(t, time.Now().Add(outboxBackoff[n-1]), *d.NextRetryAt, time.Minute)
		assert.NotNil(t, d.Error)
		d = makeDue(t, db, d)
	}
	require.True(t, claimWebhookDelivery(db, d, time.Now()))
	d = deliverWebhook(db, config.Config{}, wh, d)
	assert.Equal(t, maxOutboxAttempts, d.Attempts)
	assert.Equal(t, models.OutboxDead, d.Status, "dead letter after the last try")
	assert.Nil(t, d.NextRetryAt)

	var n int64
	db.Model(&models.WebhookDelivery{}).Count(&n)
	assert.Equal(t, int64(1), n, "one row for the whole life of the delivery")
}

func TestDeliverWebhookConnectionErrorStaysPending(t *testing.T) {
	db := webhookDB(t)
	l, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err)
	addr := l.Addr().String()
	l.Close()
	wh := models.Webhook{UserID: 1, URL: "http://" + addr, Secret: "s"}
	require.NoError(t, db.Create(&wh).Error)
	d := attempt(t, db, config.Config{}, wh, []byte("{}"))
	assert.NotNil(t, d.Error)
	assert.Nil(t, d.StatusCode)
	assert.NotNil(t, d.NextRetryAt)
	assert.Equal(t, models.OutboxPending, d.Status)
}

func TestClaimWebhookDeliveryIsExclusiveUntilTheLockIsStale(t *testing.T) {
	db := webhookDB(t)
	wh := models.Webhook{UserID: 1, URL: "http://x", Secret: "s"}
	require.NoError(t, db.Create(&wh).Error)
	d, err := enqueueWebhookDelivery(db, wh.ID, "e", []byte("{}"), time.Now())
	require.NoError(t, err)

	assert.True(t, claimWebhookDelivery(db, d, time.Now()))
	assert.False(t, claimWebhookDelivery(db, d, time.Now()), "a second worker cannot take it")

	stale := time.Now().Add(-2 * outboxLockTimeout)
	require.NoError(t, db.Model(&models.WebhookDelivery{}).Where("id = ?", d.ID).Update("locked_at", stale).Error)
	assert.True(t, claimWebhookDelivery(db, d, time.Now()), "a crashed worker's lock expires")
}

func TestDeliverWebhookBlocksPrivateTargets(t *testing.T) {
	db := webhookDB(t)
	var hits int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { atomic.AddInt32(&hits, 1) }))
	defer srv.Close()
	wh := models.Webhook{UserID: 1, URL: srv.URL, Secret: "s"}

	require.NoError(t, db.Create(&wh).Error)
	d := attempt(t, db, config.Config{WebhookBlockPrivateURLs: true}, wh, []byte("{}"))
	require.NotNil(t, d.Error)
	assert.Contains(t, *d.Error, "private")
	assert.Nil(t, d.NextRetryAt, "a blocked target is not retried")
	assert.Equal(t, models.OutboxDead, d.Status)
	assert.Zero(t, atomic.LoadInt32(&hits))
}

// The delivery client refuses at connect time, which also covers redirects to internal hosts
// and DNS rebinding.
func TestBlockingClientRefusesLoopbackAtDialTime(t *testing.T) {
	var hits int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { atomic.AddInt32(&hits, 1) }))
	defer srv.Close()
	resp, err := guardedDeliveryClient.Get(srv.URL) //nolint:bodyclose // no response on error
	if resp != nil {
		_ = resp.Body.Close()
	}
	assert.Error(t, err)
	assert.Zero(t, atomic.LoadInt32(&hits))

	// The unspecified address reaches the local host on Linux and is refused too.
	resp, err = guardedDeliveryClient.Get("http://0.0.0.0:1/") //nolint:bodyclose // no response on error
	if resp != nil {
		_ = resp.Body.Close()
	}
	assert.Error(t, err)
}

func TestTriggerWebhooksFiltersByUserEventAndActive(t *testing.T) {
	db := webhookDB(t)
	var mu sync.Mutex
	hits := map[string]int{}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		hits[r.URL.Path]++
		mu.Unlock()
	}))
	defer srv.Close()
	mk := func(path string, user uint, events []string, active bool) {
		wh := models.Webhook{UserID: user, Name: path, URL: srv.URL + path, Secret: "s", Events: events, IsActive: true}
		require.NoError(t, db.Create(&wh).Error)
		if !active {
			require.NoError(t, db.Model(&wh).Update("is_active", false).Error)
		}
	}
	mk("/match", 1, []string{"contact.created"}, true)
	mk("/other-event", 1, []string{"contact.deleted"}, true)
	mk("/inactive", 1, []string{"contact.created"}, false)
	mk("/other-user", 2, []string{"contact.created"}, true)

	TriggerWebhooks(db, config.Config{}, 1, "contact.created", map[string]string{"k": "v"}, clock.Real())
	require.Eventually(t, func() bool { mu.Lock(); defer mu.Unlock(); return hits["/match"] == 1 }, 5*time.Second, 20*time.Millisecond)
	time.Sleep(100 * time.Millisecond)
	mu.Lock()
	defer mu.Unlock()
	assert.Equal(t, map[string]int{"/match": 1}, hits)
}

func TestProcessWebhookOutbox(t *testing.T) {
	db := webhookDB(t)
	var mu sync.Mutex
	var got []string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		b, _ := io.ReadAll(r.Body)
		mu.Lock()
		got = append(got, string(b))
		mu.Unlock()
	}))
	defer srv.Close()
	wh := models.Webhook{UserID: 1, URL: srv.URL, Secret: "s", IsActive: true}
	require.NoError(t, db.Create(&wh).Error)

	past, future := time.Now().Add(-time.Minute), time.Now().Add(time.Hour)
	mk := func(payload, status string, next *time.Time, attempts int, locked *time.Time) models.WebhookDelivery {
		d := models.WebhookDelivery{WebhookID: wh.ID, EventType: "e", Payload: payload, Status: status, NextRetryAt: next, Attempts: attempts, LockedAt: locked}
		require.NoError(t, db.Create(&d).Error)
		return d
	}
	due := mk(`{"id":"due"}`, models.OutboxPending, &past, 1, nil)
	mk(`{"id":"later"}`, models.OutboxPending, &future, 1, nil)
	mk(`{"id":"dead"}`, models.OutboxDead, nil, 5, nil)
	mk(`{"id":"done"}`, models.OutboxDelivered, nil, 1, nil)
	fresh := time.Now()
	mk(`{"id":"busy"}`, models.OutboxPending, &past, 1, &fresh)
	stale := time.Now().Add(-time.Hour)
	crashed := mk(`{"id":"crashed"}`, models.OutboxPending, &past, 0, &stale)

	ProcessWebhookOutbox(db, config.Config{})

	mu.Lock()
	assert.ElementsMatch(t, []string{`{"id":"due"}`, `{"id":"crashed"}`}, got, "only due, unlocked (or stale-locked) rows are sent, with the original payload")
	mu.Unlock()
	var reloaded models.WebhookDelivery
	require.NoError(t, db.First(&reloaded, due.ID).Error)
	assert.Equal(t, models.OutboxDelivered, reloaded.Status)
	assert.Equal(t, 2, reloaded.Attempts)
	var reloaded2 models.WebhookDelivery
	require.NoError(t, db.First(&reloaded2, crashed.ID).Error)
	assert.Equal(t, models.OutboxDelivered, reloaded2.Status, "a delivery interrupted by a restart is resumed")
	var n int64
	db.Model(&models.WebhookDelivery{}).Count(&n)
	assert.Equal(t, int64(6), n, "no new rows")
}

func TestProcessWebhookOutboxKillsDeliveriesOfInactiveWebhook(t *testing.T) {
	db := webhookDB(t)
	wh := models.Webhook{UserID: 1, URL: "http://127.0.0.1:1", Secret: "s", IsActive: true}
	require.NoError(t, db.Create(&wh).Error)
	require.NoError(t, db.Model(&wh).Update("is_active", false).Error)
	past := time.Now().Add(-time.Minute)
	d := models.WebhookDelivery{WebhookID: wh.ID, EventType: "e", Payload: "{}", Attempts: 1, NextRetryAt: &past, Status: models.OutboxPending}
	require.NoError(t, db.Create(&d).Error)

	ProcessWebhookOutbox(db, config.Config{})

	var reloaded models.WebhookDelivery
	require.NoError(t, db.First(&reloaded, d.ID).Error)
	assert.Equal(t, models.OutboxDead, reloaded.Status)
	assert.Nil(t, reloaded.NextRetryAt)
	require.NotNil(t, reloaded.Error)
	assert.Equal(t, 1, reloaded.Attempts, "nothing was sent")
}

func TestTriggerWebhooksQueuesBeforeSending(t *testing.T) {
	db := webhookDB(t)
	wh := models.Webhook{UserID: 1, Name: "w", URL: "http://127.0.0.1:1", Secret: "s", Events: []string{"contact.created"}, IsActive: true}
	require.NoError(t, db.Create(&wh).Error)
	stop := make(chan struct{})
	old := startDelivery
	startDelivery = func(f func()) { go func() { <-stop; f() }() } // hold the send back
	defer func() { startDelivery = old; close(stop) }()

	TriggerWebhooks(db, config.Config{}, 1, "contact.created", map[string]string{"k": "v"}, clock.Real())

	var rows []models.WebhookDelivery
	require.NoError(t, db.Find(&rows).Error)
	require.Len(t, rows, 1, "the row exists before any HTTP call")
	assert.Equal(t, models.OutboxPending, rows[0].Status)
	assert.Nil(t, rows[0].LockedAt, "claimed only when the send starts")
}

// makeDue moves a pending delivery's next try into the past and returns the stored row.
func makeDue(t *testing.T, db *gorm.DB, d models.WebhookDelivery) models.WebhookDelivery {
	t.Helper()
	require.NoError(t, db.Model(&models.WebhookDelivery{}).Where("id = ?", d.ID).Update("next_retry_at", time.Now().Add(-time.Second)).Error)
	var out models.WebhookDelivery
	require.NoError(t, db.First(&out, d.ID).Error)
	return out
}

// The outbox job loaded the row (attempts 0) before the immediate send failed and released
// it with a retry in the future; the job's claim on its old copy must not succeed.
func TestClaimWebhookDeliveryRejectsAStaleCopyAfterARelease(t *testing.T) {
	db := webhookDB(t)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(500) }))
	defer srv.Close()
	wh := models.Webhook{UserID: 1, URL: srv.URL, Secret: "s"}
	require.NoError(t, db.Create(&wh).Error)
	d, err := enqueueWebhookDelivery(db, wh.ID, "e", []byte("{}"), time.Now())
	require.NoError(t, err)

	jobCopy := d // loaded by the job before the immediate send started
	require.True(t, claimWebhookDelivery(db, d, time.Now()))
	deliverWebhook(db, config.Config{}, wh, d) // fails, released with attempts=1 and a backoff

	assert.False(t, claimWebhookDelivery(db, jobCopy, time.Now()), "stale copy must not claim a released row")

	// even once the backoff has passed, the old copy (attempts 0) is stale
	fresh := makeDue(t, db, d)
	assert.False(t, claimWebhookDelivery(db, jobCopy, time.Now()))
	assert.True(t, claimWebhookDelivery(db, fresh, time.Now()))
}

func TestClaimWebhookDeliveryWaitsForTheRetryTime(t *testing.T) {
	db := webhookDB(t)
	future := time.Now().Add(time.Hour)
	d := models.WebhookDelivery{WebhookID: 1, Payload: "{}", EventType: "e", Status: models.OutboxPending, NextRetryAt: &future}
	require.NoError(t, db.Create(&d).Error)
	assert.False(t, claimWebhookDelivery(db, d, time.Now()))
}

func TestPurgeWebhookDeliveries(t *testing.T) {
	db := webhookDB(t)
	now := time.Now()
	live := models.Webhook{UserID: 1, URL: "http://x", Secret: "s", IsActive: true}
	require.NoError(t, db.Create(&live).Error)
	mk := func(status string, age time.Duration) uint {
		d := models.WebhookDelivery{WebhookID: live.ID, EventType: "e", Payload: "{}", Status: status}
		require.NoError(t, db.Create(&d).Error)
		require.NoError(t, db.Model(&d).UpdateColumn("updated_at", now.Add(-age)).Error)
		return d.ID
	}
	oldOK := mk(models.OutboxDelivered, 8*24*time.Hour)
	newOK := mk(models.OutboxDelivered, time.Hour)
	oldDead := mk(models.OutboxDead, 31*24*time.Hour)
	newDead := mk(models.OutboxDead, 10*24*time.Hour)
	oldPending := mk(models.OutboxPending, 60*24*time.Hour)

	n, err := PurgeWebhookDeliveries(db, now)
	require.NoError(t, err)
	assert.Equal(t, int64(2), n)
	var ids []uint
	db.Unscoped().Model(&models.WebhookDelivery{}).Pluck("id", &ids)
	assert.ElementsMatch(t, []uint{newOK, newDead, oldPending}, ids)
	assert.NotContains(t, ids, oldOK)
	assert.NotContains(t, ids, oldDead)
}

// Deliveries of deleted webhooks are of no use to anybody, whatever their state.
func TestPurgeWebhookDeliveriesDropsThoseOfDeletedWebhooks(t *testing.T) {
	db := webhookDB(t)
	gone := models.Webhook{UserID: 1, URL: "http://x", Secret: "s", IsActive: true}
	require.NoError(t, db.Create(&gone).Error)
	for _, st := range []string{models.OutboxPending, models.OutboxDead, models.OutboxDelivered} {
		require.NoError(t, db.Create(&models.WebhookDelivery{WebhookID: gone.ID, Status: st, Payload: "{}", EventType: "e"}).Error)
	}
	require.NoError(t, db.Delete(&gone).Error)

	n, err := PurgeWebhookDeliveries(db, time.Now())
	require.NoError(t, err)
	assert.Equal(t, int64(3), n)
}

// A due row must stay unclaimed while it waits for a concurrency slot: a claim held in the
// queue would expire before the send starts.
func TestProcessWebhookOutboxClaimsInsideTheConcurrencyLimit(t *testing.T) {
	db := webhookDB(t)
	var hits atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { hits.Add(1) }))
	defer srv.Close()
	wh := models.Webhook{UserID: 1, URL: srv.URL, Secret: "s", IsActive: true}
	require.NoError(t, db.Create(&wh).Error)
	d, err := enqueueWebhookDelivery(db, wh.ID, "e", []byte("{}"), time.Now())
	require.NoError(t, err)
	for i := 0; i < cap(deliverySem); i++ {
		deliverySem <- struct{}{}
	}
	done := make(chan struct{})
	go func() { ProcessWebhookOutbox(db, config.Config{}); close(done) }()
	time.Sleep(200 * time.Millisecond)
	var stored models.WebhookDelivery
	require.NoError(t, db.First(&stored, d.ID).Error)
	assert.Nil(t, stored.LockedAt, "no claim while waiting for a slot")
	for i := 0; i < cap(deliverySem); i++ {
		<-deliverySem
	}
	<-done
	assert.EqualValues(t, 1, hits.Load())
}

func TestLockTimeoutOutlastsTheSlowestSend(t *testing.T) {
	assert.Greater(t, outboxLockTimeout, 2*(deliveryClient.Timeout))
	assert.Greater(t, outboxLockTimeout, 2*(smtpTimeout+resendTimeout))
}
