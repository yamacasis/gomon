package main

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestLoadConfigDefaultsPrometheusTarget(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.yaml")
	data := []byte(`
metrics:
  port: 9091
prometheus_targets:
  - name: node-a
    url: http://127.0.0.1:9100
    type: node_exporter
`)
	if err := os.WriteFile(path, data, 0600); err != nil {
		t.Fatal(err)
	}
	cfg, err := loadConfig(path)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Metrics.ListenAddress != "0.0.0.0" {
		t.Fatalf("unexpected listen address %q", cfg.Metrics.ListenAddress)
	}
	if len(cfg.PrometheusTargets) != 1 {
		t.Fatalf("expected one target, got %d", len(cfg.PrometheusTargets))
	}
	target := cfg.PrometheusTargets[0]
	if target.URL != "http://127.0.0.1:9100/metrics" {
		t.Fatalf("expected normalized metrics URL, got %q", target.URL)
	}
	if target.Interval != time.Minute || target.Timeout != 10*time.Second {
		t.Fatalf("unexpected timing defaults: interval=%s timeout=%s", target.Interval, target.Timeout)
	}
	if target.Thresholds.CPUPercent != 90 || target.Thresholds.DiskIOPercent != 95 {
		t.Fatalf("unexpected node exporter thresholds: %+v", target.Thresholds)
	}
}
