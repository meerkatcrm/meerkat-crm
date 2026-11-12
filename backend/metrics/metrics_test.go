package metrics

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func scrape(t *testing.T, h http.Handler, header string) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(http.MethodGet, "/metrics", nil)
	if header != "" {
		req.Header.Set("Authorization", header)
	}
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	return rec
}

func TestCounterExposition(t *testing.T) {
	r := NewRegistry()
	c := r.Counter("things_total", "Things done.", "kind")
	c.Inc("a")
	c.Inc("a")
	c.Inc(`we"ird\` + "\n")

	body := scrape(t, Handler(r, ""), "").Body.String()
	assert.Contains(t, body, "# HELP things_total Things done.\n# TYPE things_total counter\n")
	assert.Contains(t, body, "things_total{kind=\"a\"} 2\n")
	assert.Contains(t, body, `things_total{kind="we\"ird\\\n"} 1`+"\n")
}

func TestHistogramExposition(t *testing.T) {
	r := NewRegistry()
	h := r.Histogram("lat_seconds", "Latency.", []float64{0.1, 1}, "route")
	h.Observe(0.05, "/x")
	h.Observe(0.5, "/x")
	h.Observe(5, "/x")

	body := scrape(t, Handler(r, ""), "").Body.String()
	assert.Contains(t, body, "# TYPE lat_seconds histogram\n")
	assert.Contains(t, body, "lat_seconds_bucket{route=\"/x\",le=\"0.1\"} 1\n")
	assert.Contains(t, body, "lat_seconds_bucket{route=\"/x\",le=\"1\"} 2\n")
	assert.Contains(t, body, "lat_seconds_bucket{route=\"/x\",le=\"+Inf\"} 3\n")
	assert.Contains(t, body, "lat_seconds_count{route=\"/x\"} 3\n")
	assert.Contains(t, body, "lat_seconds_sum{route=\"/x\"} 5.55\n")
}

func TestGaugeFuncIsReadAtScrapeTime(t *testing.T) {
	r := NewRegistry()
	v := 1.0
	r.GaugeFunc("level", "Level.", []string{"job"}, func() []Sample {
		return []Sample{{Labels: []string{"b"}, Value: v}}
	})
	assert.Contains(t, scrape(t, Handler(r, ""), "").Body.String(), "level{job=\"b\"} 1\n")
	v = 7
	assert.Contains(t, scrape(t, Handler(r, ""), "").Body.String(), "level{job=\"b\"} 7\n")
}

func TestWrongLabelCountIsIgnored(t *testing.T) {
	r := NewRegistry()
	c := r.Counter("x_total", "x", "a", "b")
	c.Inc("only-one") // must not panic
	assert.NotContains(t, scrape(t, Handler(r, ""), "").Body.String(), "x_total{")
}

func TestHandlerToken(t *testing.T) {
	h := Handler(NewRegistry(), "s3cret")

	rec := scrape(t, h, "")
	assert.Equal(t, http.StatusUnauthorized, rec.Code)
	assert.Contains(t, rec.Header().Get("WWW-Authenticate"), "Bearer")
	assert.NotContains(t, rec.Body.String(), "# TYPE")

	assert.Equal(t, http.StatusUnauthorized, scrape(t, h, "Bearer wrong").Code)
	assert.Equal(t, http.StatusUnauthorized, scrape(t, h, "s3cret").Code)
	assert.Equal(t, http.StatusUnauthorized, scrape(t, h, "Bearer s3cret2").Code)

	rec = scrape(t, h, "Bearer s3cret")
	assert.Equal(t, http.StatusOK, rec.Code)
	assert.Contains(t, rec.Header().Get("Content-Type"), "text/plain")
	assert.Contains(t, rec.Header().Get("Cache-Control"), "no-store")
}

func TestHandlerWithoutTokenIsOpen(t *testing.T) {
	assert.Equal(t, http.StatusOK, scrape(t, Handler(NewRegistry(), ""), "").Code)
}

func TestHandlerOnlyGet(t *testing.T) {
	rec := httptest.NewRecorder()
	Handler(NewRegistry(), "").ServeHTTP(rec, httptest.NewRequest(http.MethodPost, "/metrics", nil))
	assert.Equal(t, http.StatusMethodNotAllowed, rec.Code)
}

func TestMiddlewareRecordsRouteNotPath(t *testing.T) {
	gin.SetMode(gin.TestMode)
	r := NewRegistry()
	m := NewHTTPMetrics(r)
	e := gin.New()
	e.Use(m.Middleware())
	e.GET("/contacts/:id", func(c *gin.Context) { c.String(200, "ok") })
	e.GET("/boom", func(c *gin.Context) { c.Status(500) })

	for _, p := range []string{"/contacts/1", "/contacts/2", "/boom", "/nope/123", "/nope/456"} {
		e.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest(http.MethodGet, p, nil))
	}
	body := scrape(t, Handler(r, ""), "").Body.String()
	assert.Contains(t, body, `meerkat_http_requests_total{method="GET",route="/contacts/:id",status="200"} 2`)
	assert.Contains(t, body, `meerkat_http_requests_total{method="GET",route="/boom",status="500"} 1`)
	assert.Contains(t, body, `meerkat_http_requests_total{method="GET",route="unmatched",status="404"} 2`)
	assert.Contains(t, body, `meerkat_http_request_duration_seconds_count{method="GET",route="/contacts/:id"} 2`)
	assert.NotContains(t, body, "/contacts/1")
	assert.NotContains(t, body, "/nope/123")
}

func TestMiddlewareBoundsMethodCardinality(t *testing.T) {
	gin.SetMode(gin.TestMode)
	r := NewRegistry()
	e := gin.New()
	e.Use(NewHTTPMetrics(r).Middleware())
	e.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest("WEIRD", "/x", nil))
	assert.Contains(t, scrape(t, Handler(r, ""), "").Body.String(), `method="OTHER"`)
}

func TestDefaultRegistryHasAppMetrics(t *testing.T) {
	WebhookDelivery(true)
	WebhookDelivery(false)
	EmailSent(true)
	EmailSent(false)
	JobResult("backup", ResultFailure)
	JobDone("backup", nil)
	JobResult("reminders", ResultSkipped)

	body := scrape(t, Handler(Default, ""), "").Body.String()
	for _, want := range []string{
		`meerkat_webhook_deliveries_total{result="success"}`,
		`meerkat_webhook_deliveries_total{result="failure"}`,
		`meerkat_emails_total{result="success"}`,
		`meerkat_emails_total{result="failure"}`,
		`meerkat_job_runs_total{job="backup",result="failure"}`,
		`meerkat_job_runs_total{job="backup",result="success"}`,
		`meerkat_job_runs_total{job="reminders",result="skipped"}`,
		"go_goroutines ",
		"process_start_time_seconds ",
	} {
		assert.Contains(t, body, want)
	}
	require.True(t, strings.HasSuffix(body, "\n"))
}

func TestServerServesOnlyMetrics(t *testing.T) {
	srv := NewServer("127.0.0.1:0", NewRegistry(), "")
	assert.NotZero(t, srv.ReadHeaderTimeout)
	rec := httptest.NewRecorder()
	srv.Handler.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/metrics", nil))
	assert.Equal(t, http.StatusOK, rec.Code)
	rec = httptest.NewRecorder()
	srv.Handler.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/v1/users/me", nil))
	assert.Equal(t, http.StatusNotFound, rec.Code)
}

func TestDuplicateRegistrationPanics(t *testing.T) {
	r := NewRegistry()
	r.Counter("dup_total", "x")
	assert.Panics(t, func() { r.Counter("dup_total", "y") })
	assert.Panics(t, func() { r.GaugeFunc("dup_total", "y", nil, func() []Sample { return nil }) })
	assert.Panics(t, func() {
		NewHTTPMetrics(r)
		NewHTTPMetrics(r)
	})
}

func TestOnScrapeRunsOncePerWrite(t *testing.T) {
	r := NewRegistry()
	n := 0
	r.OnScrape(func() { n++ })
	r.GaugeFunc("a", "a", nil, func() []Sample { return nil })
	r.GaugeFunc("b", "b", nil, func() []Sample { return nil })
	scrape(t, Handler(r, ""), "")
	assert.Equal(t, 1, n)
}
