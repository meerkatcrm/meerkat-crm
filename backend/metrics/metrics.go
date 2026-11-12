// Package metrics is a small Prometheus-compatible registry: counters, histograms and
// gauges read at scrape time, written in the text exposition format (version 0.0.4).
// It has no dependencies; the app needs a handful of series, not a client library.
package metrics

import (
	"crypto/subtle"
	"fmt"
	"io"
	"math"
	"net/http"
	"runtime"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"
)

// Sample is one gauge value with its label values (in the order of the label names).
type Sample struct {
	Labels []string
	Value  float64
}

type collector interface {
	name() string
	write(w io.Writer)
}

// Registry holds metrics and writes them out. It is safe for concurrent use.
type Registry struct {
	mu         sync.Mutex
	collectors []collector
	onScrape   []func()
}

// OnScrape registers fn to run once at the start of every Write, before any gauge function
// is read. Gauges that share a data source load it here, so a scrape queries it once.
func (r *Registry) OnScrape(fn func()) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.onScrape = append(r.onScrape, fn)
}

// NewRegistry returns an empty registry.
func NewRegistry() *Registry { return &Registry{} }

// add registers c. A name registered twice is a programming error (the second metric would
// write a second, conflicting TYPE line), so it panics at start-up.
func (r *Registry) add(c collector) {
	r.mu.Lock()
	defer r.mu.Unlock()
	for _, o := range r.collectors {
		if o.name() == c.name() {
			panic("metrics: duplicate registration of " + c.name())
		}
	}
	r.collectors = append(r.collectors, c)
}

// Write writes all metrics, sorted by name, in the Prometheus text format.
func (r *Registry) Write(w io.Writer) {
	r.mu.Lock()
	cs := append([]collector(nil), r.collectors...)
	hooks := append([]func(){}, r.onScrape...)
	r.mu.Unlock()
	for _, fn := range hooks {
		fn()
	}
	sort.Slice(cs, func(i, j int) bool { return cs[i].name() < cs[j].name() })
	for _, c := range cs {
		c.write(w)
	}
}

var labelEscaper = strings.NewReplacer(`\`, `\\`, `"`, `\"`, "\n", `\n`)

func helpEscape(s string) string {
	return strings.NewReplacer(`\`, `\\`, "\n", `\n`).Replace(s)
}

// labelString renders {a="x",b="y"} plus optional extra pairs (used for le).
func labelString(names, values []string, extra ...string) string {
	parts := make([]string, 0, len(names)+len(extra)/2)
	for i, n := range names {
		parts = append(parts, n+`="`+labelEscaper.Replace(values[i])+`"`)
	}
	for i := 0; i+1 < len(extra); i += 2 {
		parts = append(parts, extra[i]+`="`+extra[i+1]+`"`)
	}
	if len(parts) == 0 {
		return ""
	}
	return "{" + strings.Join(parts, ",") + "}"
}

func formatFloat(f float64) string {
	if math.IsInf(f, 1) {
		return "+Inf"
	}
	return strconv.FormatFloat(f, 'g', -1, 64)
}

const labelSep = "\xff"

func header(w io.Writer, name, help, typ string) {
	_, _ = fmt.Fprintf(w, "# HELP %s %s\n# TYPE %s %s\n", name, helpEscape(help), name, typ)
}

// CounterVec is a counter with labels.
type CounterVec struct {
	n      string
	help   string
	labels []string
	mu     sync.Mutex
	vals   map[string]*counterSeries
}

type counterSeries struct {
	labels []string
	value  float64
}

// Counter registers a counter.
func (r *Registry) Counter(name, help string, labelNames ...string) *CounterVec {
	c := &CounterVec{n: name, help: help, labels: labelNames, vals: map[string]*counterSeries{}}
	r.add(c)
	return c
}

// Inc adds one to the series for the label values. A wrong number of values is ignored.
func (c *CounterVec) Inc(labelValues ...string) {
	if len(labelValues) != len(c.labels) {
		return
	}
	key := strings.Join(labelValues, labelSep)
	c.mu.Lock()
	defer c.mu.Unlock()
	s := c.vals[key]
	if s == nil {
		s = &counterSeries{labels: append([]string(nil), labelValues...)}
		c.vals[key] = s
	}
	s.value++
}

func (c *CounterVec) name() string { return c.n }

func (c *CounterVec) write(w io.Writer) {
	c.mu.Lock()
	keys := make([]string, 0, len(c.vals))
	for k := range c.vals {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	lines := make([]string, 0, len(keys))
	for _, k := range keys {
		s := c.vals[k]
		lines = append(lines, fmt.Sprintf("%s%s %s\n", c.n, labelString(c.labels, s.labels), formatFloat(s.value)))
	}
	c.mu.Unlock()
	header(w, c.n, c.help, "counter")
	for _, l := range lines {
		_, _ = io.WriteString(w, l)
	}
}

// HistogramVec is a histogram with labels.
type HistogramVec struct {
	n       string
	help    string
	labels  []string
	buckets []float64
	mu      sync.Mutex
	vals    map[string]*histSeries
}

type histSeries struct {
	labels []string
	counts []uint64 // per bucket, not cumulative
	count  uint64
	sum    float64
}

// Histogram registers a histogram with the given upper bounds (ascending).
func (r *Registry) Histogram(name, help string, buckets []float64, labelNames ...string) *HistogramVec {
	h := &HistogramVec{n: name, help: help, labels: labelNames, buckets: buckets, vals: map[string]*histSeries{}}
	r.add(h)
	return h
}

// Observe records a value. A wrong number of label values is ignored.
func (h *HistogramVec) Observe(v float64, labelValues ...string) {
	if len(labelValues) != len(h.labels) {
		return
	}
	key := strings.Join(labelValues, labelSep)
	h.mu.Lock()
	defer h.mu.Unlock()
	s := h.vals[key]
	if s == nil {
		s = &histSeries{labels: append([]string(nil), labelValues...), counts: make([]uint64, len(h.buckets))}
		h.vals[key] = s
	}
	for i, ub := range h.buckets {
		if v <= ub {
			s.counts[i]++
			break
		}
	}
	s.count++
	s.sum += v
}

func (h *HistogramVec) name() string { return h.n }

func (h *HistogramVec) write(w io.Writer) {
	h.mu.Lock()
	keys := make([]string, 0, len(h.vals))
	for k := range h.vals {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	var sb strings.Builder
	for _, k := range keys {
		s := h.vals[k]
		var cum uint64
		for i, ub := range h.buckets {
			cum += s.counts[i]
			fmt.Fprintf(&sb, "%s_bucket%s %d\n", h.n, labelString(h.labels, s.labels, "le", formatFloat(ub)), cum)
		}
		fmt.Fprintf(&sb, "%s_bucket%s %d\n", h.n, labelString(h.labels, s.labels, "le", "+Inf"), s.count)
		fmt.Fprintf(&sb, "%s_sum%s %s\n", h.n, labelString(h.labels, s.labels), formatFloat(s.sum))
		fmt.Fprintf(&sb, "%s_count%s %d\n", h.n, labelString(h.labels, s.labels), s.count)
	}
	h.mu.Unlock()
	header(w, h.n, h.help, "histogram")
	_, _ = io.WriteString(w, sb.String())
}

type gaugeFunc struct {
	n      string
	help   string
	labels []string
	fn     func() []Sample
}

// GaugeFunc registers a gauge whose samples are computed on every scrape.
func (r *Registry) GaugeFunc(name, help string, labelNames []string, fn func() []Sample) {
	r.add(&gaugeFunc{n: name, help: help, labels: labelNames, fn: fn})
}

func (g *gaugeFunc) name() string { return g.n }

func (g *gaugeFunc) write(w io.Writer) {
	samples := g.fn()
	header(w, g.n, g.help, "gauge")
	for _, s := range samples {
		if len(s.Labels) != len(g.labels) {
			continue
		}
		_, _ = fmt.Fprintf(w, "%s%s %s\n", g.n, labelString(g.labels, s.Labels), formatFloat(s.Value))
	}
}

// Handler serves the registry. With a non-empty token the request needs
// "Authorization: Bearer <token>" (compared in constant time).
func Handler(r *Registry, token string) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		if req.Method != http.MethodGet && req.Method != http.MethodHead {
			w.Header().Set("Allow", "GET, HEAD")
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		if token != "" {
			got, ok := strings.CutPrefix(req.Header.Get("Authorization"), "Bearer ")
			if !ok || subtle.ConstantTimeCompare([]byte(got), []byte(token)) != 1 {
				w.Header().Set("WWW-Authenticate", `Bearer realm="metrics"`)
				http.Error(w, "unauthorized", http.StatusUnauthorized)
				return
			}
		}
		w.Header().Set("Content-Type", "text/plain; version=0.0.4; charset=utf-8")
		w.Header().Set("Cache-Control", "no-store")
		if req.Method == http.MethodHead {
			return
		}
		r.Write(w)
	})
}

// NewServer returns an HTTP server that serves only /metrics (for METRICS_ADDR).
func NewServer(addr string, r *Registry, token string) *http.Server {
	mux := http.NewServeMux()
	mux.Handle("/metrics", Handler(r, token))
	return &http.Server{
		Addr:              addr,
		Handler:           mux,
		ReadHeaderTimeout: 10 * time.Second,
		WriteTimeout:      30 * time.Second,
	}
}

// Default is the registry the application records into.
var Default = NewRegistry()

var (
	webhookDeliveries = Default.Counter("meerkat_webhook_deliveries_total",
		"Webhook delivery attempts by result (failures include retries).", "result")
	emailsSent = Default.Counter("meerkat_emails_total",
		"Email sends by result (failure: every configured channel failed).", "result")
	jobRuns = Default.Counter("meerkat_job_runs_total",
		"Scheduled job runs by result (success, failure, or skipped by the rate limit or another instance's lock).", "job", "result")
	defaultHTTP = NewHTTPMetrics(Default)
)

func init() {
	start := float64(time.Now().UnixNano()) / 1e9
	Default.GaugeFunc("process_start_time_seconds", "Start time of the process, Unix seconds.", nil,
		func() []Sample { return []Sample{{Value: start}} })
	Default.GaugeFunc("go_goroutines", "Number of goroutines.", nil,
		func() []Sample { return []Sample{{Value: float64(runtime.NumGoroutine())}} })
	Default.GaugeFunc("go_memstats_alloc_bytes", "Bytes of allocated heap objects.", nil,
		func() []Sample {
			var m runtime.MemStats
			runtime.ReadMemStats(&m)
			return []Sample{{Value: float64(m.Alloc)}}
		})
}

func result(ok bool) string {
	if ok {
		return "success"
	}
	return "failure"
}

// WebhookDelivery counts one delivery attempt.
func WebhookDelivery(ok bool) { webhookDeliveries.Inc(result(ok)) }

// EmailSent counts one email send (after all channels were tried).
func EmailSent(ok bool) { emailsSent.Inc(result(ok)) }

// Results of a scheduled job run. A run that the rate limit or another instance's lock
// turned away is "skipped", not a success.
const (
	ResultSuccess = "success"
	ResultFailure = "failure"
	ResultSkipped = "skipped"
)

// JobResult counts one run of a scheduled job. job is the scheduler's job name
// (reminders, calendar-sync, contact-sync, trash-purge, backup).
func JobResult(job, res string) { jobRuns.Inc(job, res) }

// JobDone counts a finished run: success when err is nil, otherwise failure.
func JobDone(job string, err error) {
	if err != nil {
		JobResult(job, ResultFailure)
		return
	}
	JobResult(job, ResultSuccess)
}

// DefaultHTTP returns the HTTP metrics of the default registry.
func DefaultHTTP() *HTTPMetrics { return defaultHTTP }
