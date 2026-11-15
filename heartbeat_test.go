package main

import (
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestHeartbeatExcludesIgnoredCloudflareChecksFromUptime(t *testing.T) {
	logger, err := newLogger(filepath.Join(t.TempDir(), "monitor.log"))
	if err != nil {
		t.Fatal(err)
	}
	defer logger.close()

	url := "https://example.com"
	checkedAt := time.Now().UTC().AddDate(0, 0, -1)
	logger.write(CheckResult{URL: url, Up: true, SSLDays: -1, CheckedAt: checkedAt})
	logger.write(CheckResult{URL: url, Suppressed: true, Kind: "timeout", SSLDays: -1, CheckedAt: checkedAt})

	cfg := &Config{Websites: []Website{{URL: url}}}
	report, err := buildReport(cfg, logger)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(report, "Uptime: 100.0% (1/1 checks)") {
		t.Fatalf("ignored check affected uptime: %s", report)
	}
	if !strings.Contains(report, "Ignored network checks: 1") {
		t.Fatalf("ignored check was not reported: %s", report)
	}
}
