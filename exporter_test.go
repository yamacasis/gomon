package main

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	dto "github.com/prometheus/client_model/go"
	"github.com/prometheus/common/expfmt"
	"github.com/prometheus/common/model"
)

func TestEvaluateNodeExporterCalculatesRatesAndIssues(t *testing.T) {
	target := PrometheusTargetConfig{
		Name: "node-a",
		Type: "node_exporter",
		Thresholds: NodeExporterThresholds{
			CPUPercent:         70,
			RAMPercent:         80,
			DiskPercent:        90,
			DiskIOPercent:      70,
			LoadPerCPU:         2,
			TemperatureCelsius: 85,
		},
	}
	state := &exporterState{issues: make(map[string]*exporterIssueState)}
	baseline := parseMetrics(t, `
node_cpu_seconds_total{cpu="0",mode="idle"} 80
node_cpu_seconds_total{cpu="0",mode="user"} 20
node_cpu_seconds_total{cpu="1",mode="idle"} 80
node_cpu_seconds_total{cpu="1",mode="user"} 20
node_disk_io_time_seconds_total{device="sda"} 10
`)
	first := ExporterResult{CheckedAt: time.Unix(100, 0), Collectors: make(map[string]float64)}
	evaluateNodeExporter(target, baseline, state, &first)
	if first.CPUValid || len(first.Disks) != 1 || first.Disks[0].Valid {
		t.Fatal("first cumulative sample must not produce CPU or disk rates")
	}

	current := parseMetrics(t, `
node_cpu_seconds_total{cpu="0",mode="idle"} 85
node_cpu_seconds_total{cpu="0",mode="user"} 35
node_cpu_seconds_total{cpu="1",mode="idle"} 85
node_cpu_seconds_total{cpu="1",mode="user"} 35
node_memory_MemTotal_bytes 100
node_memory_MemAvailable_bytes 10
node_load1 6
node_filesystem_size_bytes{device="/dev/sda1",mountpoint="/",fstype="ext4"} 100
node_filesystem_avail_bytes{device="/dev/sda1",mountpoint="/",fstype="ext4"} 5
node_filesystem_readonly{device="/dev/sda1",mountpoint="/",fstype="ext4"} 0
node_filesystem_device_error{device="/dev/sda1",mountpoint="/",fstype="ext4"} 0
node_disk_io_time_seconds_total{device="sda"} 10.8
node_hwmon_temp_celsius{chip="coretemp",sensor="temp1"} 90
node_scrape_collector_success{collector="filesystem"} 0
node_scrape_collector_success{collector="zfs"} 0
node_md_disks{device="md0",state="failed"} 1
smartmon_device_smart_healthy{device="sdb"} 0
`)
	second := ExporterResult{CheckedAt: time.Unix(101, 0), Collectors: make(map[string]float64)}
	evaluateNodeExporter(target, current, state, &second)

	if !second.CPUValid || second.CPUPercent < 74.9 || second.CPUPercent > 75.1 {
		t.Fatalf("expected 75%% CPU, got valid=%v value=%.2f", second.CPUValid, second.CPUPercent)
	}
	if !second.RAMValid || second.RAMPercent != 90 {
		t.Fatalf("expected 90%% RAM, got valid=%v value=%.2f", second.RAMValid, second.RAMPercent)
	}
	if len(second.Filesystems) != 1 || second.Filesystems[0].UsagePercent != 95 {
		t.Fatalf("expected 95%% root filesystem usage, got %+v", second.Filesystems)
	}
	if len(second.Disks) != 1 || !second.Disks[0].Valid || second.Disks[0].BusyPercent < 79.9 {
		t.Fatalf("expected about 80%% disk busy, got %+v", second.Disks)
	}

	keys := make(map[string]bool)
	for _, issue := range second.Issues {
		keys[issue.Key] = true
	}
	for _, key := range []string{"cpu", "ram", "load", "filesystem_usage:_", "disk_io:sda", "temperature:coretemp_temp1", "collector:filesystem", "raid:md0", "smart:sdb"} {
		if !keys[key] {
			t.Errorf("expected issue %q, got %+v", key, second.Issues)
		}
	}
	if keys["collector:zfs"] {
		t.Fatal("an unavailable optional collector must not trigger an alert")
	}
}

func TestExporterStateDebouncesResourceAndEndpointAlerts(t *testing.T) {
	target := PrometheusTargetConfig{Name: "node-a", Type: "node_exporter", FailureThreshold: 2, RecoveryThreshold: 2}
	state := &exporterState{issues: make(map[string]*exporterIssueState)}
	now := time.Unix(100, 0).UTC()
	bad := ExporterResult{Up: true, CheckedAt: now, Issues: []ExporterIssue{{Key: "cpu", Kind: "CPU", Summary: "CPU usage 95%"}}}

	if messages := updateExporterState(target, state, bad); len(messages) != 0 {
		t.Fatalf("first resource violation must not alert: %v", messages)
	}
	bad.CheckedAt = now.Add(time.Minute)
	if messages := updateExporterState(target, state, bad); len(messages) != 1 {
		t.Fatalf("second resource violation must alert once: %v", messages)
	} else {
		for _, tag := range []string{"#gomon", "#prometheus", "#nodeexporter", "#resource", "#cpu", "#nodea"} {
			if !strings.Contains(messages[0], tag) {
				t.Fatalf("resource alert lacks tag %s: %s", tag, messages[0])
			}
		}
	}
	if messages := updateExporterState(target, state, bad); len(messages) != 0 {
		t.Fatalf("active resource issue must not repeat: %v", messages)
	}

	healthy := ExporterResult{Up: true, CheckedAt: now.Add(2 * time.Minute)}
	if messages := updateExporterState(target, state, healthy); len(messages) != 0 {
		t.Fatalf("first healthy scrape must not recover: %v", messages)
	}
	healthy.CheckedAt = now.Add(3 * time.Minute)
	if messages := updateExporterState(target, state, healthy); len(messages) != 1 || !strings.Contains(messages[0], "Resource Recovered") {
		t.Fatalf("second healthy scrape must recover resource issue: %v", messages)
	}

	down := ExporterResult{CheckedAt: now.Add(4 * time.Minute), ErrorKind: "timeout", ErrorMessage: "timed out"}
	if messages := updateExporterState(target, state, down); len(messages) != 0 {
		t.Fatalf("first failed scrape must not alert: %v", messages)
	}
	down.CheckedAt = now.Add(5 * time.Minute)
	if messages := updateExporterState(target, state, down); len(messages) != 1 || !strings.Contains(messages[0], "Target Down") {
		t.Fatalf("second failed scrape must alert endpoint down: %v", messages)
	}
}

func TestCheckPrometheusTargetScrapesHTTPMetrics(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		username, password, ok := r.BasicAuth()
		if !ok || username != "admin" || password != "secret" {
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		_, _ = w.Write([]byte("node_memory_MemTotal_bytes 100\nnode_memory_MemAvailable_bytes 40\n"))
	}))
	defer server.Close()
	target := PrometheusTargetConfig{
		Name:       "node-a",
		URL:        server.URL,
		Type:       "node_exporter",
		BasicAuth:  &BasicAuthConfig{Username: "admin", Password: "secret"},
		Timeout:    time.Second,
		Thresholds: NodeExporterThresholds{CPUPercent: 101, RAMPercent: 101, DiskPercent: 101, DiskIOPercent: 101, LoadPerCPU: 101, TemperatureCelsius: 101},
	}
	state := &exporterState{issues: make(map[string]*exporterIssueState)}
	result := checkPrometheusTarget(target, state)
	if !result.Up || !result.RAMValid || result.RAMPercent != 60 {
		t.Fatalf("unexpected scrape result: %+v", result)
	}
	if result.Duration <= 0 {
		t.Fatal("expected scrape duration to be recorded")
	}
}

func parseMetrics(t *testing.T, input string) map[string]*dto.MetricFamily {
	t.Helper()
	parser := expfmt.NewTextParser(model.LegacyValidation)
	families, err := parser.TextToMetricFamilies(strings.NewReader(strings.TrimSpace(input) + "\n"))
	if err != nil {
		t.Fatalf("parse fixture: %v", err)
	}
	return families
}
