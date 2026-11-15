package main

import (
	"context"
	"fmt"
	"log"
	"net"
	"net/http"
	"runtime"
	"strconv"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promhttp"
)

const metricsPath = "/metrics/me"

var (
	version = "dev"
	commit  = "unknown"
)

type monitorMetrics struct {
	registry *prometheus.Registry

	selfUp                   prometheus.Gauge
	buildInfo                *prometheus.GaugeVec
	siteUp                   *prometheus.GaugeVec
	siteAlerting             *prometheus.GaugeVec
	siteCheckDuration        *prometheus.GaugeVec
	siteLastCheck            *prometheus.GaugeVec
	siteHTTPStatus           *prometheus.GaugeVec
	siteAttempts             *prometheus.GaugeVec
	siteSSLDays              *prometheus.GaugeVec
	siteConsecutiveFailures  *prometheus.GaugeVec
	siteCheckSuppressed      *prometheus.GaugeVec
	exporterUp               *prometheus.GaugeVec
	exporterAlerting         *prometheus.GaugeVec
	exporterScrapeDuration   *prometheus.GaugeVec
	exporterLastScrape       *prometheus.GaugeVec
	exporterCPU              *prometheus.GaugeVec
	exporterRAM              *prometheus.GaugeVec
	exporterLoadPerCPU       *prometheus.GaugeVec
	exporterFilesystemUsage  *prometheus.GaugeVec
	exporterFilesystemRO     *prometheus.GaugeVec
	exporterFilesystemError  *prometheus.GaugeVec
	exporterDiskIO           *prometheus.GaugeVec
	exporterTemperature      *prometheus.GaugeVec
	exporterCollectorSuccess *prometheus.GaugeVec
}

func newMonitorMetrics() *monitorMetrics {
	m := &monitorMetrics{
		registry: prometheus.NewRegistry(),
		selfUp: prometheus.NewGauge(prometheus.GaugeOpts{
			Name: "gomon_up",
			Help: "Whether the Gomon process initialized successfully.",
		}),
		buildInfo: prometheus.NewGaugeVec(prometheus.GaugeOpts{
			Name: "gomon_build_info",
			Help: "Build information for Gomon.",
		}, []string{"version", "commit", "go_version"}),
		siteUp: prometheus.NewGaugeVec(prometheus.GaugeOpts{
			Name: "gomon_site_up",
			Help: "Effective website availability after applying configured suppression policies.",
		}, []string{"url"}),
		siteAlerting: prometheus.NewGaugeVec(prometheus.GaugeOpts{
			Name: "gomon_site_alerting",
			Help: "Whether the website is in a confirmed down alert state.",
		}, []string{"url"}),
		siteCheckDuration: prometheus.NewGaugeVec(prometheus.GaugeOpts{
			Name: "gomon_site_check_duration_seconds",
			Help: "Duration of the most recent website check including retries.",
		}, []string{"url"}),
		siteLastCheck: prometheus.NewGaugeVec(prometheus.GaugeOpts{
			Name: "gomon_site_last_check_timestamp_seconds",
			Help: "Unix timestamp of the most recent website check.",
		}, []string{"url"}),
		siteHTTPStatus: prometheus.NewGaugeVec(prometheus.GaugeOpts{
			Name: "gomon_site_http_status_code",
			Help: "HTTP status code from the most recent website check, or zero when unavailable.",
		}, []string{"url"}),
		siteAttempts: prometheus.NewGaugeVec(prometheus.GaugeOpts{
			Name: "gomon_site_check_attempts",
			Help: "Number of HTTP attempts used by the most recent website check.",
		}, []string{"url"}),
		siteSSLDays: prometheus.NewGaugeVec(prometheus.GaugeOpts{
			Name: "gomon_site_ssl_days_remaining",
			Help: "Whole days remaining on the website TLS certificate, or -1 when unavailable.",
		}, []string{"url"}),
		siteConsecutiveFailures: prometheus.NewGaugeVec(prometheus.GaugeOpts{
			Name: "gomon_site_consecutive_failures",
			Help: "Current number of consecutive failed website checks.",
		}, []string{"url"}),
		siteCheckSuppressed: prometheus.NewGaugeVec(prometheus.GaugeOpts{
			Name: "gomon_site_check_suppressed",
			Help: "Whether the most recent website failure was excluded from availability state by policy.",
		}, []string{"url"}),
		exporterUp: prometheus.NewGaugeVec(prometheus.GaugeOpts{
			Name: "gomon_prometheus_target_up",
			Help: "Whether Gomon successfully scraped and parsed the Prometheus target.",
		}, []string{"target", "type"}),
		exporterAlerting: prometheus.NewGaugeVec(prometheus.GaugeOpts{
			Name: "gomon_prometheus_target_active_alerts",
			Help: "Number of active confirmed endpoint or resource alerts for the Prometheus target.",
		}, []string{"target", "type"}),
		exporterScrapeDuration: prometheus.NewGaugeVec(prometheus.GaugeOpts{
			Name: "gomon_prometheus_target_scrape_duration_seconds",
			Help: "Duration of Gomon's most recent scrape of the Prometheus target.",
		}, []string{"target", "type"}),
		exporterLastScrape: prometheus.NewGaugeVec(prometheus.GaugeOpts{
			Name: "gomon_prometheus_target_last_scrape_timestamp_seconds",
			Help: "Unix timestamp of Gomon's most recent scrape attempt.",
		}, []string{"target", "type"}),
		exporterCPU: prometheus.NewGaugeVec(prometheus.GaugeOpts{
			Name: "gomon_node_cpu_usage_percent",
			Help: "CPU usage calculated from consecutive node_exporter samples.",
		}, []string{"target"}),
		exporterRAM: prometheus.NewGaugeVec(prometheus.GaugeOpts{
			Name: "gomon_node_memory_usage_percent",
			Help: "RAM usage calculated from node_memory_MemAvailable_bytes.",
		}, []string{"target"}),
		exporterLoadPerCPU: prometheus.NewGaugeVec(prometheus.GaugeOpts{
			Name: "gomon_node_load1_per_cpu",
			Help: "One-minute load average divided by logical CPU count.",
		}, []string{"target"}),
		exporterFilesystemUsage: prometheus.NewGaugeVec(prometheus.GaugeOpts{
			Name: "gomon_node_filesystem_usage_percent",
			Help: "Filesystem usage percentage derived from size and available bytes.",
		}, []string{"target", "mountpoint", "device", "fstype"}),
		exporterFilesystemRO: prometheus.NewGaugeVec(prometheus.GaugeOpts{
			Name: "gomon_node_filesystem_readonly",
			Help: "Whether node_exporter reports the filesystem as read-only.",
		}, []string{"target", "mountpoint", "device"}),
		exporterFilesystemError: prometheus.NewGaugeVec(prometheus.GaugeOpts{
			Name: "gomon_node_filesystem_device_error",
			Help: "Whether node_exporter reports a filesystem device error.",
		}, []string{"target", "mountpoint", "device"}),
		exporterDiskIO: prometheus.NewGaugeVec(prometheus.GaugeOpts{
			Name: "gomon_node_disk_io_busy_percent",
			Help: "Disk I/O busy percentage calculated from consecutive node_exporter samples.",
		}, []string{"target", "device"}),
		exporterTemperature: prometheus.NewGaugeVec(prometheus.GaugeOpts{
			Name: "gomon_node_temperature_celsius",
			Help: "Hardware temperature reported by node_exporter.",
		}, []string{"target", "sensor"}),
		exporterCollectorSuccess: prometheus.NewGaugeVec(prometheus.GaugeOpts{
			Name: "gomon_node_collector_success",
			Help: "Success value reported by each node_exporter collector.",
		}, []string{"target", "collector"}),
	}

	m.registry.MustRegister(
		m.selfUp,
		m.buildInfo,
		m.siteUp,
		m.siteAlerting,
		m.siteCheckDuration,
		m.siteLastCheck,
		m.siteHTTPStatus,
		m.siteAttempts,
		m.siteSSLDays,
		m.siteConsecutiveFailures,
		m.siteCheckSuppressed,
		m.exporterUp,
		m.exporterAlerting,
		m.exporterScrapeDuration,
		m.exporterLastScrape,
		m.exporterCPU,
		m.exporterRAM,
		m.exporterLoadPerCPU,
		m.exporterFilesystemUsage,
		m.exporterFilesystemRO,
		m.exporterFilesystemError,
		m.exporterDiskIO,
		m.exporterTemperature,
		m.exporterCollectorSuccess,
		prometheus.NewGoCollector(),
		prometheus.NewProcessCollector(prometheus.ProcessCollectorOpts{}),
	)
	m.selfUp.Set(1)
	m.buildInfo.WithLabelValues(version, commit, runtime.Version()).Set(1)
	return m
}

func (m *monitorMetrics) recordSite(site Website, state *monitorState, result CheckResult) {
	up := 0.0
	if result.Up || (result.Suppressed && state.status != "down") {
		up = 1
	}
	alerting := 0.0
	if state.status == "down" {
		alerting = 1
	}
	m.siteUp.WithLabelValues(site.URL).Set(up)
	m.siteAlerting.WithLabelValues(site.URL).Set(alerting)
	m.siteCheckDuration.WithLabelValues(site.URL).Set(result.Duration.Seconds())
	m.siteLastCheck.WithLabelValues(site.URL).Set(float64(result.CheckedAt.Unix()))
	m.siteHTTPStatus.WithLabelValues(site.URL).Set(float64(result.StatusCode))
	m.siteAttempts.WithLabelValues(site.URL).Set(float64(result.Attempts))
	m.siteSSLDays.WithLabelValues(site.URL).Set(float64(result.SSLDays))
	m.siteConsecutiveFailures.WithLabelValues(site.URL).Set(float64(state.consecutiveFailures))
	m.siteCheckSuppressed.WithLabelValues(site.URL).Set(boolFloat(result.Suppressed))
}

func (m *monitorMetrics) recordExporter(target PrometheusTargetConfig, state *exporterState, result ExporterResult) {
	labels := []string{target.Name, target.Type}
	up := 0.0
	if result.Up {
		up = 1
	}
	m.exporterUp.WithLabelValues(labels...).Set(up)
	m.exporterScrapeDuration.WithLabelValues(labels...).Set(result.Duration.Seconds())
	m.exporterLastScrape.WithLabelValues(labels...).Set(float64(result.CheckedAt.Unix()))
	m.exporterAlerting.WithLabelValues(labels...).Set(float64(state.activeIssueCount()))
	if !result.Up {
		return
	}

	if result.CPUValid {
		m.exporterCPU.WithLabelValues(target.Name).Set(result.CPUPercent)
	}
	if result.RAMValid {
		m.exporterRAM.WithLabelValues(target.Name).Set(result.RAMPercent)
	}
	if result.LoadValid {
		m.exporterLoadPerCPU.WithLabelValues(target.Name).Set(result.LoadPerCPU)
	}

	m.exporterFilesystemUsage.DeletePartialMatch(prometheus.Labels{"target": target.Name})
	m.exporterFilesystemRO.DeletePartialMatch(prometheus.Labels{"target": target.Name})
	m.exporterFilesystemError.DeletePartialMatch(prometheus.Labels{"target": target.Name})
	for _, filesystem := range result.Filesystems {
		m.exporterFilesystemUsage.WithLabelValues(target.Name, filesystem.Mountpoint, filesystem.Device, filesystem.FSType).Set(filesystem.UsagePercent)
		m.exporterFilesystemRO.WithLabelValues(target.Name, filesystem.Mountpoint, filesystem.Device).Set(boolFloat(filesystem.ReadOnly))
		m.exporterFilesystemError.WithLabelValues(target.Name, filesystem.Mountpoint, filesystem.Device).Set(boolFloat(filesystem.DeviceError))
	}

	m.exporterDiskIO.DeletePartialMatch(prometheus.Labels{"target": target.Name})
	for _, disk := range result.Disks {
		if disk.Valid {
			m.exporterDiskIO.WithLabelValues(target.Name, disk.Device).Set(disk.BusyPercent)
		}
	}

	m.exporterTemperature.DeletePartialMatch(prometheus.Labels{"target": target.Name})
	for _, temperature := range result.Temperatures {
		m.exporterTemperature.WithLabelValues(target.Name, temperature.Sensor).Set(temperature.Celsius)
	}

	m.exporterCollectorSuccess.DeletePartialMatch(prometheus.Labels{"target": target.Name})
	for collector, success := range result.Collectors {
		m.exporterCollectorSuccess.WithLabelValues(target.Name, collector).Set(success)
	}
}

func startMetricsServer(cfg MetricsConfig, metrics *monitorMetrics) (*http.Server, error) {
	if cfg.Port == 0 {
		return nil, nil
	}
	address := net.JoinHostPort(cfg.ListenAddress, strconv.Itoa(cfg.Port))
	listener, err := net.Listen("tcp", address)
	if err != nil {
		return nil, fmt.Errorf("listen on %s: %w", address, err)
	}

	mux := http.NewServeMux()
	mux.Handle(metricsPath, promhttp.HandlerFor(metrics.registry, promhttp.HandlerOpts{}))
	mux.HandleFunc("/healthz", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/plain; charset=utf-8")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("ok\n"))
	})
	server := &http.Server{
		Addr:              address,
		Handler:           mux,
		ReadHeaderTimeout: 5 * time.Second,
		IdleTimeout:       30 * time.Second,
	}
	go func() {
		if err := server.Serve(listener); err != nil && err != http.ErrServerClosed {
			log.Printf("metrics server error: %v", err)
		}
	}()
	log.Printf("[METRICS] listening on %s%s", address, metricsPath)
	return server, nil
}

func stopMetricsServer(server *http.Server) {
	if server == nil {
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := server.Shutdown(ctx); err != nil {
		log.Printf("metrics server shutdown error: %v", err)
	}
}

func boolFloat(value bool) float64 {
	if value {
		return 1
	}
	return 0
}
