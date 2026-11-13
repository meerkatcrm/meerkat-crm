package middleware

import (
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"meerkat/clock"

	"github.com/gin-gonic/gin"
)

func TestClockMiddlewareFixesNowForTheRequest(t *testing.T) {
	gin.SetMode(gin.TestMode)
	fake := clock.NewFake(time.Date(2026, 12, 24, 12, 0, 0, 0, time.UTC))
	var first, second time.Time
	r := gin.New()
	r.Use(ClockMiddleware(fake))
	r.GET("/", func(c *gin.Context) {
		first = RequestNow(c)
		fake.Advance(time.Hour) // later reads of the clock must not change this request's now
		second = RequestNow(c)
		c.Status(http.StatusNoContent)
	})
	r.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest(http.MethodGet, "/", nil))

	want := time.Date(2026, 12, 24, 12, 0, 0, 0, time.UTC)
	if !first.Equal(want) || !second.Equal(want) {
		t.Fatalf("now = %v then %v, want %v both times", first, second, want)
	}
}

func TestRequestNowFallsBackToTheWallClock(t *testing.T) {
	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	if d := time.Since(RequestNow(c)); d < 0 || d > time.Minute {
		t.Fatalf("fallback now is %v away from the wall clock", d)
	}
}
