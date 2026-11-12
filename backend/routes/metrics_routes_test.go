package routes

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"meerkat/config"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
)

func metricsStatus(t *testing.T, cfg *config.Config, auth string) int {
	t.Helper()
	gin.SetMode(gin.ReleaseMode)
	cfg.JWTSecretKey = "test-secret-key-32-chars-minimum!"
	r := gin.New()
	RegisterRoutes(r, cfg, nil, nil)
	req := httptest.NewRequest(http.MethodGet, "/metrics", nil)
	if auth != "" {
		req.Header.Set("Authorization", auth)
	}
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)
	return rec.Code
}

func TestMetricsRouteIsOffByDefault(t *testing.T) {
	assert.Equal(t, http.StatusNotFound, metricsStatus(t, &config.Config{}, ""))
}

func TestMetricsRouteOnMainListener(t *testing.T) {
	assert.Equal(t, http.StatusOK, metricsStatus(t, &config.Config{MetricsEnabled: true}, ""))
}

func TestMetricsRouteTokenOnMainListener(t *testing.T) {
	cfg := &config.Config{MetricsEnabled: true, MetricsToken: "tok"}
	assert.Equal(t, http.StatusUnauthorized, metricsStatus(t, cfg, ""))
	assert.Equal(t, http.StatusOK, metricsStatus(t, cfg, "Bearer tok"))
}

func TestMetricsRouteAbsentWithSeparateListener(t *testing.T) {
	cfg := &config.Config{MetricsEnabled: true, MetricsAddr: "127.0.0.1:9090"}
	assert.Equal(t, http.StatusNotFound, metricsStatus(t, cfg, ""))
}
