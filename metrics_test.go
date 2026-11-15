package main

import (
	"net"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/prometheus/client_golang/prometheus/promhttp"
)

func TestMetricsEndpointIncludesSiteAndExporterState(t *testing.T) {
	metrics := newMonitorMetrics()
	site := Website{URL: "https://example.com"}
	siteState := &monitorState{status: "up"}
	metrics.recordSite(site, siteState, CheckResult{
		URL: site.URL, Up: true, StatusCode: 200, SSLDays: 42, Attempts: 2,
		Duration: 250 * time.Millisecond, CheckedAt: time.Unix(100, 0),
	})
	cloudflareSite := Website{URL: "https://cloudflare.example", Cloudflare: true}
	metrics.recordSite(cloudflareSite, &monitorState{status: "up"}, CheckResult{
		URL: cloudflareSite.URL, Suppressed: true, Kind: "timeout", SSLDays: -1,
		Attempts: 3, Duration: time.Second, CheckedAt: time.Unix(100, 0),
	})
	target := PrometheusTargetConfig{Name: "node-a", Type: "node_exporter"}
	exporterState := &exporterState{status: "up", issues: make(map[string]*exporterIssueState)}
	metrics.recordExporter(target, exporterState, ExporterResult{
		Up: true, CheckedAt: time.Unix(100, 0), Duration: time.Second,
		CPUValid: true, CPUPercent: 25, RAMValid: true, RAMPercent: 50,
	})

	recorder := httptest.NewRecorder()
	request := httptest.NewRequest("GET", metricsPath, nil)
	promhttp.HandlerFor(metrics.registry, promhttp.HandlerOpts{}).ServeHTTP(recorder, request)
	if recorder.Code != 200 {
		t.Fatalf("expected HTTP 200, got %d", recorder.Code)
	}
	body := recorder.Body.String()
	for _, expected := range []string{
		`gomon_up 1`,
		`gomon_site_up{url="https://example.com"} 1`,
		`gomon_site_http_status_code{url="https://example.com"} 200`,
		`gomon_site_up{url="https://cloudflare.example"} 1`,
		`gomon_site_check_suppressed{url="https://cloudflare.example"} 1`,
		`gomon_prometheus_target_up{target="node-a",type="node_exporter"} 1`,
		`gomon_node_cpu_usage_percent{target="node-a"} 25`,
	} {
		if !strings.Contains(body, expected) {
			t.Errorf("metrics output missing %q", expected)
		}
	}
}

func TestStartMetricsServerServesConfiguredPath(t *testing.T) {
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	port := listener.Addr().(*net.TCPAddr).Port
	_ = listener.Close()

	server, err := startMetricsServer(MetricsConfig{ListenAddress: "127.0.0.1", Port: port}, newMonitorMetrics())
	if err != nil {
		t.Fatal(err)
	}
	defer stopMetricsServer(server)
	response, err := http.Get("http://127.0.0.1:" + strconv.Itoa(port) + metricsPath)
	if err != nil {
		t.Fatal(err)
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		t.Fatalf("expected HTTP 200, got %d", response.StatusCode)
	}
}
