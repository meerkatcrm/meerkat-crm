package middleware

import (
	"time"

	"meerkat/clock"

	"github.com/gin-gonic/gin"
)

// ClockMiddleware reads the clock once per request, so that everything a request does
// agrees on the time. Handlers read it with RequestNow. The clock itself is kept for work
// that outlives the request or does slow I/O (RequestClock): a time taken before the I/O
// is stale after it.
func ClockMiddleware(clk clock.Clock) gin.HandlerFunc {
	return func(c *gin.Context) {
		c.Set(clock.ContextKey, clk.Now())
		c.Set(clock.ClockKey, clk)
		c.Next()
	}
}

// RequestNow returns the time ClockMiddleware stored for this request. Without the
// middleware (a bare test context) it reads the wall clock.
func RequestNow(c *gin.Context) time.Time { return clock.RequestTime(c) }

// RequestClock returns the clock ClockMiddleware stored, for work that must read the time
// again later (background deliveries, long batches). Without the middleware it is the
// wall clock.
func RequestClock(c *gin.Context) clock.Clock {
	if v, ok := c.Get(clock.ClockKey); ok {
		if clk, ok := v.(clock.Clock); ok {
			return clk
		}
	}
	return clock.Real()
}
