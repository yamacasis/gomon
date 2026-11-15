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
    basic_auth:
      username: admin
      password: secret
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
	if target.BasicAuth == nil || target.BasicAuth.Username != "admin" || target.BasicAuth.Password != "secret" {
		t.Fatalf("unexpected basic auth config: %+v", target.BasicAuth)
	}
	if target.Thresholds.CPUPercent != 90 || target.Thresholds.DiskIOPercent != 95 {
		t.Fatalf("unexpected node exporter thresholds: %+v", target.Thresholds)
	}
}

func TestLoadConfigRejectsBasicAuthWithoutUsername(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.yaml")
	data := []byte(`
prometheus_targets:
  - name: node-a
    url: http://127.0.0.1:9100
    basic_auth:
      password: secret
`)
	if err := os.WriteFile(path, data, 0600); err != nil {
		t.Fatal(err)
	}
	if _, err := loadConfig(path); err == nil {
		t.Fatal("expected missing basic auth username to be rejected")
	}
}

func TestLoadConfigRejectsConflictingAuthorization(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.yaml")
	data := []byte(`
prometheus_targets:
  - name: node-a
    url: http://127.0.0.1:9100
    basic_auth:
      username: admin
      password: secret
    headers:
      authorization: Bearer token
`)
	if err := os.WriteFile(path, data, 0600); err != nil {
		t.Fatal(err)
	}
	if _, err := loadConfig(path); err == nil {
		t.Fatal("expected conflicting Authorization settings to be rejected")
	}
}
