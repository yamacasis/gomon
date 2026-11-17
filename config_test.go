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
	if len(target.Tags) != 1 || target.Tags[0] != "general" {
		t.Fatalf("expected default general routing tag, got %v", target.Tags)
	}
}

func TestLoadConfigMultipleTaggedChannels(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.yaml")
	data := []byte(`
telegram_channels:
  - name: infrastructure
    tags: [infra, Critical]
    bot_token: telegram-token
    chat_id: telegram-chat
bale_channels:
  - name: websites
    tags: [web]
    bot_token: bale-token
    chat_id: bale-chat
websites:
  - url: https://example.com
    tags: [web, General]
`)
	if err := os.WriteFile(path, data, 0600); err != nil {
		t.Fatal(err)
	}
	cfg, err := loadConfig(path)
	if err != nil {
		t.Fatal(err)
	}
	if len(cfg.TelegramChannels) != 1 || len(cfg.BaleChannels) != 1 {
		t.Fatalf("unexpected channel counts: telegram=%d bale=%d", len(cfg.TelegramChannels), len(cfg.BaleChannels))
	}
	if got := cfg.TelegramChannels[0].Tags; len(got) != 2 || got[0] != "infra" || got[1] != "critical" {
		t.Fatalf("unexpected normalized Telegram tags: %v", got)
	}
	if got := cfg.Websites[0].Tags; len(got) != 2 || got[0] != "web" || got[1] != "general" {
		t.Fatalf("unexpected website tags: %v", got)
	}
	notifiers, err := buildNotifiers(cfg)
	if err != nil {
		t.Fatal(err)
	}
	if len(notifiers) != 2 {
		t.Fatalf("expected two tagged notifiers, got %d", len(notifiers))
	}
}

func TestLoadConfigLegacyOutputsDefaultToGeneral(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.yaml")
	data := []byte(`
telegram:
  bot_token: telegram-token
  chat_id: telegram-chat
bale:
  bot_token: bale-token
  chat_id: bale-chat
websites:
  - url: https://example.com
`)
	if err := os.WriteFile(path, data, 0600); err != nil {
		t.Fatal(err)
	}
	cfg, err := loadConfig(path)
	if err != nil {
		t.Fatal(err)
	}
	for name, tags := range map[string][]string{
		"telegram": cfg.Telegram.Tags,
		"bale":     cfg.Bale.Tags,
		"website":  cfg.Websites[0].Tags,
	} {
		if len(tags) != 1 || tags[0] != "general" {
			t.Fatalf("expected %s to default to general, got %v", name, tags)
		}
	}
	notifiers, err := buildNotifiers(cfg)
	if err != nil {
		t.Fatal(err)
	}
	if len(notifiers) != 2 {
		t.Fatalf("expected two legacy notifiers, got %d", len(notifiers))
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

func TestLoadConfigCloudflareWebsitePolicy(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.yaml")
	data := []byte(`
websites:
  - url: https://example.com
    cloudflare: true
`)
	if err := os.WriteFile(path, data, 0600); err != nil {
		t.Fatal(err)
	}
	cfg, err := loadConfig(path)
	if err != nil {
		t.Fatal(err)
	}
	if len(cfg.Websites) != 1 || !cfg.Websites[0].Cloudflare {
		t.Fatalf("expected Cloudflare policy to be enabled: %+v", cfg.Websites)
	}
}

func TestLoadConfigAPIAndNotificationSettings(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.yaml")
	data := []byte(`
notifications:
  queue_file: state/queue.json
  delivery_log: state/deliveries.log
  timeout: 3s
  initial_retry: 2s
  max_retry: 1m
  max_attempts: 7
websites:
  - url: https://api.example.com/health
    method: post
    bearer_token: secret
    body: '{"probe":true}'
    expected_statuses: [200, 201]
    response_type: json
    response_contains: [healthy]
    json_assertions:
      status: ok
      data.ready: true
`)
	if err := os.WriteFile(path, data, 0600); err != nil {
		t.Fatal(err)
	}
	cfg, err := loadConfig(path)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Notifications.Timeout != 3*time.Second || cfg.Notifications.MaxAttempts != 7 {
		t.Fatalf("unexpected notification settings: %+v", cfg.Notifications)
	}
	site := cfg.Websites[0]
	if site.Method != "POST" || site.ResponseType != "json" || len(site.ExpectedStatuses) != 2 {
		t.Fatalf("unexpected API settings: %+v", site)
	}
	if site.JSONAssertions["status"] != "ok" || site.JSONAssertions["data.ready"] != true {
		t.Fatalf("unexpected JSON assertions: %#v", site.JSONAssertions)
	}
}

func TestExampleConfigLoads(t *testing.T) {
	if _, err := loadConfig("config.example.yaml"); err != nil {
		t.Fatalf("config.example.yaml must remain valid: %v", err)
	}
}

func TestNotificationFilesDefaultBesideMonitorLog(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.yaml")
	data := []byte("log_file: state/monitor.log\n")
	if err := os.WriteFile(path, data, 0600); err != nil {
		t.Fatal(err)
	}
	cfg, err := loadConfig(path)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Notifications.QueueFile != filepath.Join("state", "notification-queue.json") ||
		cfg.Notifications.DeliveryLog != filepath.Join("state", "notification-deliveries.log") {
		t.Fatalf("notification files should default beside log_file: %+v", cfg.Notifications)
	}
}
