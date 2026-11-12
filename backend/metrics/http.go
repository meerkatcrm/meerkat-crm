package metrics

import (
	"net/http"
	"strconv"
	"time"

	"github.com/gin-gonic/gin"
)

// HTTPMetrics records request counts and latency per route.
type HTTPMetrics struct {
	requests *CounterVec
	duration *HistogramVec
}

// NewHTTPMetrics registers the HTTP series in r.
func NewHTTPMetrics(r *Registry) *HTTPMetrics {
	return &HTTPMetrics{
		requests: r.Counter("meerkat_http_requests_total",
			"HTTP requests by method, route pattern and status.", "method", "route", "status"),
		duration: r.Histogram("meerkat_http_request_duration_seconds",
			"HTTP request latency by method and route pattern.",
			[]float64{0.005, 0.01, 0.025, 0.05, 0.1, 0.25, 0.5, 1, 2.5, 5, 10, 30}, "method", "route"),
	}
}

var knownMethods = map[string]bool{
	http.MethodGet: true, http.MethodHead: true, http.MethodPost: true, http.MethodPut: true,
	http.MethodPatch: true, http.MethodDelete: true, http.MethodOptions: true,
	"PROPFIND": true, "REPORT": true, "MKCOL": true, "COPY": true, "MOVE": true,
}

// Middleware records every request. The label is the route pattern (never the URL, which
// holds ids and the ICS token); requests without a route are "unmatched", and unknown
// methods "OTHER", so a client cannot create series at will.
func (m *HTTPMetrics) Middleware() gin.HandlerFunc {
	return func(c *gin.Context) {
		start := time.Now()
		c.Next()
		route := c.FullPath()
		if route == "" {
			route = "unmatched"
		}
		method := c.Request.Method
		if !knownMethods[method] {
			method = "OTHER"
		}
		m.requests.Inc(method, route, strconv.Itoa(c.Writer.Status()))
		m.duration.Observe(time.Since(start).Seconds(), method, route)
	}
}
