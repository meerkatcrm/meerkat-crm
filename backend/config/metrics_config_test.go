package config

import "testing"

func TestMetricsConfig(t *testing.T) {
	unsetEnv(t, "METRICS_ENABLED")
	unsetEnv(t, "METRICS_TOKEN")
	unsetEnv(t, "METRICS_ADDR")
	cfg := LoadConfig()
	if cfg.MetricsEnabled || cfg.MetricsToken != "" || cfg.MetricsAddr != "" {
		t.Errorf("metrics must be off by default, got %+v %q %q", cfg.MetricsEnabled, cfg.MetricsToken, cfg.MetricsAddr)
	}

	t.Setenv("METRICS_ENABLED", "true")
	t.Setenv("METRICS_TOKEN", "tok")
	t.Setenv("METRICS_ADDR", "127.0.0.1:9090")
	cfg = LoadConfig()
	if !cfg.MetricsEnabled || cfg.MetricsToken != "tok" || cfg.MetricsAddr != "127.0.0.1:9090" {
		t.Errorf("unexpected metrics config: %+v %q %q", cfg.MetricsEnabled, cfg.MetricsToken, cfg.MetricsAddr)
	}
}
