package main

import (
	"fmt"
	"io"
	"log"
	"math"
	"net/http"
	"sort"
	"strings"
	"time"

	dto "github.com/prometheus/client_model/go"
	"github.com/prometheus/common/expfmt"
	"github.com/prometheus/common/model"
)

const maxPrometheusResponseBytes = 64 << 20

type ExporterResult struct {
	Up           bool
	CheckedAt    time.Time
	Duration     time.Duration
	ErrorKind    string
	ErrorMessage string
	CPUPercent   float64
	CPUValid     bool
	RAMPercent   float64
	RAMValid     bool
	LoadPerCPU   float64
	LoadValid    bool
	Filesystems  []FilesystemStat
	Disks        []DiskStat
	Temperatures []TemperatureStat
	Collectors   map[string]float64
	Issues       []ExporterIssue
}

type FilesystemStat struct {
	Mountpoint   string
	Device       string
	FSType       string
	UsagePercent float64
	ReadOnly     bool
	DeviceError  bool
}

type DiskStat struct {
	Device      string
	BusyPercent float64
	Valid       bool
}

type TemperatureStat struct {
	Sensor  string
	Celsius float64
}

type ExporterIssue struct {
	Key     string
	Kind    string
	Summary string
}

type cpuCounter struct {
	Total float64
	Idle  float64
}

type exporterIssueState struct {
	BadCount  int
	GoodCount int
	Active    bool
	Last      ExporterIssue
}

type exporterState struct {
	status               string
	consecutiveFailures  int
	consecutiveSuccesses int
	firstFailureAt       time.Time
	downSince            time.Time
	lastError            string
	issues               map[string]*exporterIssueState
	previousCPU          map[string]cpuCounter
	previousDiskBusy     map[string]float64
	previousSampleAt     time.Time
}

func (s *exporterState) activeIssueCount() int {
	count := 0
	if s.status == "down" {
		count++
	}
	for _, issue := range s.issues {
		if issue.Active {
			count++
		}
	}
	return count
}

func runPrometheusTarget(target PrometheusTargetConfig, notifiers []Notifier, metrics *monitorMetrics) {
	state := &exporterState{issues: make(map[string]*exporterIssueState)}
	doPrometheusCheck(target, state, notifiers, metrics)

	ticker := time.NewTicker(target.Interval)
	defer ticker.Stop()
	for range ticker.C {
		doPrometheusCheck(target, state, notifiers, metrics)
	}
}

func doPrometheusCheck(target PrometheusTargetConfig, state *exporterState, notifiers []Notifier, metrics *monitorMetrics) {
	result := checkPrometheusTarget(target, state)
	messages := updateExporterState(target, state, result)
	metrics.recordExporter(target, state, result)

	if result.Up {
		log.Printf("[PROMETHEUS] UP target=%s type=%s duration=%s cpu=%s ram=%s issues=%d",
			target.Name, target.Type, result.Duration.Round(time.Millisecond), optionalPercent(result.CPUPercent, result.CPUValid),
			optionalPercent(result.RAMPercent, result.RAMValid), len(result.Issues))
	} else {
		log.Printf("[PROMETHEUS] DOWN target=%s type=%s kind=%s duration=%s error=%s",
			target.Name, target.Type, result.ErrorKind, result.Duration.Round(time.Millisecond), result.ErrorMessage)
	}
	for _, message := range messages {
		notify(notifiers, message)
	}
}

func checkPrometheusTarget(target PrometheusTargetConfig, state *exporterState) (result ExporterResult) {
	result = ExporterResult{
		CheckedAt:  time.Now().UTC(),
		Collectors: make(map[string]float64),
	}
	started := time.Now()
	defer func() {
		result.Duration = time.Since(started)
	}()

	client := &http.Client{Timeout: target.Timeout}
	req, err := http.NewRequest(http.MethodGet, target.URL, nil)
	if err != nil {
		result.ErrorKind = "configuration"
		result.ErrorMessage = shortError(err)
		return result
	}
	req.Header.Set("User-Agent", "gomon")
	req.Header.Set("Accept", "text/plain; version=0.0.4, application/openmetrics-text")
	for key, value := range target.Headers {
		req.Header.Set(key, value)
	}
	if target.BasicAuth != nil {
		req.SetBasicAuth(target.BasicAuth.Username, target.BasicAuth.Password)
	}

	response, err := client.Do(req)
	if err != nil {
		result.ErrorKind = classifyRequestError(err)
		result.ErrorMessage = shortError(err)
		return result
	}
	defer response.Body.Close()
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		result.ErrorKind = "http"
		result.ErrorMessage = fmt.Sprintf("HTTP %d", response.StatusCode)
		return result
	}

	parser := expfmt.NewTextParser(model.LegacyValidation)
	families, err := parser.TextToMetricFamilies(io.LimitReader(response.Body, maxPrometheusResponseBytes))
	if err != nil {
		result.ErrorKind = "parse"
		result.ErrorMessage = shortError(err)
		return result
	}
	if target.Type != "node_exporter" {
		result.ErrorKind = "configuration"
		result.ErrorMessage = "unsupported prometheus target type: " + target.Type
		return result
	}

	evaluateNodeExporter(target, families, state, &result)
	result.Up = true
	return result
}

func evaluateNodeExporter(target PrometheusTargetConfig, families map[string]*dto.MetricFamily, state *exporterState, result *ExporterResult) {
	now := result.CheckedAt
	cpuCurrent := collectCPUCounters(families["node_cpu_seconds_total"])
	if len(cpuCurrent) > 0 {
		result.CPUPercent, result.CPUValid = calculateCPUPercent(state.previousCPU, cpuCurrent)
		state.previousCPU = cpuCurrent
	}

	if total, ok := firstMetricValue(families["node_memory_MemTotal_bytes"]); ok && total > 0 {
		if available, ok := firstMetricValue(families["node_memory_MemAvailable_bytes"]); ok {
			result.RAMPercent = clampPercent((1 - available/total) * 100)
			result.RAMValid = true
		}
	}

	coreCount := len(cpuCurrent)
	if load, ok := firstMetricValue(families["node_load1"]); ok && coreCount > 0 {
		result.LoadPerCPU = load / float64(coreCount)
		result.LoadValid = true
	}

	result.Filesystems = collectFilesystems(families)
	diskCurrent := collectDiskBusyCounters(families["node_disk_io_time_seconds_total"])
	result.Disks = calculateDiskBusy(state.previousDiskBusy, diskCurrent, now.Sub(state.previousSampleAt))
	if len(diskCurrent) > 0 {
		state.previousDiskBusy = diskCurrent
	}
	state.previousSampleAt = now

	result.Temperatures = collectTemperatures(families)
	result.Collectors = collectCollectors(families["node_scrape_collector_success"])
	result.Issues = buildNodeExporterIssues(target, families, *result)
}

func collectCPUCounters(family *dto.MetricFamily) map[string]cpuCounter {
	counters := make(map[string]cpuCounter)
	if family == nil {
		return counters
	}
	for _, metric := range family.Metric {
		cpu := metricLabel(metric, "cpu")
		mode := metricLabel(metric, "mode")
		value, ok := metricValue(metric)
		if cpu == "" || !ok {
			continue
		}
		counter := counters[cpu]
		counter.Total += value
		if mode == "idle" {
			counter.Idle += value
		}
		counters[cpu] = counter
	}
	return counters
}

func calculateCPUPercent(previous, current map[string]cpuCounter) (float64, bool) {
	if len(previous) == 0 || len(current) == 0 {
		return 0, false
	}
	var totalDelta, idleDelta float64
	for cpu, currentCounter := range current {
		previousCounter, ok := previous[cpu]
		if !ok {
			continue
		}
		total := currentCounter.Total - previousCounter.Total
		idle := currentCounter.Idle - previousCounter.Idle
		if total <= 0 || idle < 0 {
			continue
		}
		totalDelta += total
		idleDelta += idle
	}
	if totalDelta <= 0 {
		return 0, false
	}
	return clampPercent((1 - idleDelta/totalDelta) * 100), true
}

func collectFilesystems(families map[string]*dto.MetricFamily) []FilesystemStat {
	available := metricValuesByFilesystem(families["node_filesystem_avail_bytes"])
	readOnly := metricValuesByFilesystem(families["node_filesystem_readonly"])
	deviceErrors := metricValuesByFilesystem(families["node_filesystem_device_error"])
	var filesystems []FilesystemStat
	sizes := families["node_filesystem_size_bytes"]
	if sizes == nil {
		return filesystems
	}
	for _, metric := range sizes.Metric {
		device := metricLabel(metric, "device")
		mountpoint := metricLabel(metric, "mountpoint")
		fsType := metricLabel(metric, "fstype")
		if ignoreFilesystem(mountpoint, fsType) {
			continue
		}
		size, ok := metricValue(metric)
		if !ok || size <= 0 {
			continue
		}
		key := filesystemKey(device, mountpoint)
		avail, ok := available[key]
		if !ok {
			continue
		}
		filesystems = append(filesystems, FilesystemStat{
			Mountpoint:   mountpoint,
			Device:       device,
			FSType:       fsType,
			UsagePercent: clampPercent((1 - avail/size) * 100),
			ReadOnly:     readOnly[key] > 0,
			DeviceError:  deviceErrors[key] > 0,
		})
	}
	sort.Slice(filesystems, func(i, j int) bool { return filesystems[i].Mountpoint < filesystems[j].Mountpoint })
	return filesystems
}

func metricValuesByFilesystem(family *dto.MetricFamily) map[string]float64 {
	values := make(map[string]float64)
	if family == nil {
		return values
	}
	for _, metric := range family.Metric {
		value, ok := metricValue(metric)
		if !ok {
			continue
		}
		values[filesystemKey(metricLabel(metric, "device"), metricLabel(metric, "mountpoint"))] = value
	}
	return values
}

func collectDiskBusyCounters(family *dto.MetricFamily) map[string]float64 {
	values := make(map[string]float64)
	if family == nil {
		return values
	}
	for _, metric := range family.Metric {
		device := metricLabel(metric, "device")
		value, ok := metricValue(metric)
		if device == "" || ignoreDiskDevice(device) || !ok {
			continue
		}
		values[device] = value
	}
	return values
}

func calculateDiskBusy(previous, current map[string]float64, elapsed time.Duration) []DiskStat {
	disks := make([]DiskStat, 0, len(current))
	for device, currentValue := range current {
		disk := DiskStat{Device: device}
		if previousValue, ok := previous[device]; ok && elapsed > 0 {
			delta := currentValue - previousValue
			if delta >= 0 {
				disk.BusyPercent = clampPercent(delta / elapsed.Seconds() * 100)
				disk.Valid = true
			}
		}
		disks = append(disks, disk)
	}
	sort.Slice(disks, func(i, j int) bool { return disks[i].Device < disks[j].Device })
	return disks
}

func collectTemperatures(families map[string]*dto.MetricFamily) []TemperatureStat {
	var temperatures []TemperatureStat
	for _, familyName := range []string{"node_hwmon_temp_celsius", "node_thermal_zone_temp"} {
		family := families[familyName]
		if family == nil {
			continue
		}
		for _, metric := range family.Metric {
			value, ok := metricValue(metric)
			if !ok || math.IsNaN(value) || math.IsInf(value, 0) {
				continue
			}
			sensor := joinNonEmpty(metricLabel(metric, "chip"), metricLabel(metric, "sensor"), metricLabel(metric, "zone"), metricLabel(metric, "type"))
			if sensor == "" {
				sensor = familyName
			}
			temperatures = append(temperatures, TemperatureStat{Sensor: sensor, Celsius: value})
		}
	}
	return temperatures
}

func collectCollectors(family *dto.MetricFamily) map[string]float64 {
	collectors := make(map[string]float64)
	if family == nil {
		return collectors
	}
	for _, metric := range family.Metric {
		collector := metricLabel(metric, "collector")
		value, ok := metricValue(metric)
		if collector != "" && ok {
			collectors[collector] = value
		}
	}
	return collectors
}

func buildNodeExporterIssues(target PrometheusTargetConfig, families map[string]*dto.MetricFamily, result ExporterResult) []ExporterIssue {
	thresholds := target.Thresholds
	var issues []ExporterIssue
	if result.CPUValid && thresholdEnabled(thresholds.CPUPercent) && result.CPUPercent >= thresholds.CPUPercent {
		issues = append(issues, ExporterIssue{Key: "cpu", Kind: "CPU", Summary: fmt.Sprintf("CPU usage %.1f%% >= %.1f%%", result.CPUPercent, thresholds.CPUPercent)})
	}
	if result.RAMValid && thresholdEnabled(thresholds.RAMPercent) && result.RAMPercent >= thresholds.RAMPercent {
		issues = append(issues, ExporterIssue{Key: "ram", Kind: "RAM", Summary: fmt.Sprintf("RAM usage %.1f%% >= %.1f%%", result.RAMPercent, thresholds.RAMPercent)})
	}
	if result.LoadValid && thresholdEnabled(thresholds.LoadPerCPU) && result.LoadPerCPU >= thresholds.LoadPerCPU {
		issues = append(issues, ExporterIssue{Key: "load", Kind: "LOAD", Summary: fmt.Sprintf("load1 per CPU %.2f >= %.2f", result.LoadPerCPU, thresholds.LoadPerCPU)})
	}
	for _, filesystem := range result.Filesystems {
		key := sanitizeIssueKey(filesystem.Mountpoint)
		if thresholdEnabled(thresholds.DiskPercent) && filesystem.UsagePercent >= thresholds.DiskPercent {
			issues = append(issues, ExporterIssue{Key: "filesystem_usage:" + key, Kind: "DISK", Summary: fmt.Sprintf("filesystem %s usage %.1f%% >= %.1f%%", filesystem.Mountpoint, filesystem.UsagePercent, thresholds.DiskPercent)})
		}
		if filesystem.ReadOnly {
			issues = append(issues, ExporterIssue{Key: "filesystem_readonly:" + key, Kind: "DISK", Summary: fmt.Sprintf("filesystem %s is read-only", filesystem.Mountpoint)})
		}
		if filesystem.DeviceError {
			issues = append(issues, ExporterIssue{Key: "filesystem_error:" + key, Kind: "HARDWARE", Summary: fmt.Sprintf("filesystem %s reports a device error", filesystem.Mountpoint)})
		}
	}
	for _, disk := range result.Disks {
		if disk.Valid && thresholdEnabled(thresholds.DiskIOPercent) && disk.BusyPercent >= thresholds.DiskIOPercent {
			issues = append(issues, ExporterIssue{Key: "disk_io:" + sanitizeIssueKey(disk.Device), Kind: "DISK IO", Summary: fmt.Sprintf("disk %s busy %.1f%% >= %.1f%%", disk.Device, disk.BusyPercent, thresholds.DiskIOPercent)})
		}
	}
	for _, temperature := range result.Temperatures {
		if thresholdEnabled(thresholds.TemperatureCelsius) && temperature.Celsius >= thresholds.TemperatureCelsius {
			issues = append(issues, ExporterIssue{Key: "temperature:" + sanitizeIssueKey(temperature.Sensor), Kind: "HARDWARE", Summary: fmt.Sprintf("sensor %s temperature %.1f°C >= %.1f°C", temperature.Sensor, temperature.Celsius, thresholds.TemperatureCelsius)})
		}
	}
	for collector, success := range result.Collectors {
		if success == 0 {
			issues = append(issues, ExporterIssue{Key: "collector:" + sanitizeIssueKey(collector), Kind: "COLLECTOR", Summary: fmt.Sprintf("node_exporter collector %s failed", collector)})
		}
	}
	issues = append(issues, collectRAIDIssues(families)...)
	issues = append(issues, collectSMARTIssues(families)...)
	sort.Slice(issues, func(i, j int) bool { return issues[i].Key < issues[j].Key })
	return issues
}

func collectRAIDIssues(families map[string]*dto.MetricFamily) []ExporterIssue {
	var issues []ExporterIssue
	degraded := families["node_md_degraded"]
	if degraded != nil {
		for _, metric := range degraded.Metric {
			value, ok := metricValue(metric)
			if !ok || value <= 0 {
				continue
			}
			device := metricLabel(metric, "device")
			issues = append(issues, ExporterIssue{Key: "raid:" + sanitizeIssueKey(device), Kind: "HARDWARE", Summary: fmt.Sprintf("RAID %s is degraded by %.0f disk(s)", device, value)})
		}
		if len(issues) > 0 {
			return issues
		}
	}

	family := families["node_md_disks"]
	if family == nil {
		return issues
	}
	for _, metric := range family.Metric {
		if metricLabel(metric, "state") != "failed" {
			continue
		}
		value, ok := metricValue(metric)
		if !ok || value <= 0 {
			continue
		}
		device := metricLabel(metric, "device")
		issues = append(issues, ExporterIssue{Key: "raid:" + sanitizeIssueKey(device), Kind: "HARDWARE", Summary: fmt.Sprintf("RAID %s has %.0f failed disk(s)", device, value)})
	}
	return issues
}

func collectSMARTIssues(families map[string]*dto.MetricFamily) []ExporterIssue {
	var issues []ExporterIssue
	for _, name := range []string{"smartmon_device_smart_healthy", "smartctl_device_smart_status"} {
		family := families[name]
		if family == nil {
			continue
		}
		for _, metric := range family.Metric {
			value, ok := metricValue(metric)
			if !ok || value > 0 {
				continue
			}
			device := metricLabel(metric, "device")
			if device == "" {
				device = metricLabel(metric, "disk")
			}
			issues = append(issues, ExporterIssue{Key: "smart:" + sanitizeIssueKey(device), Kind: "HARDWARE", Summary: fmt.Sprintf("SMART health check failed for %s", device)})
		}
	}
	return issues
}

func updateExporterState(target PrometheusTargetConfig, state *exporterState, result ExporterResult) []string {
	if state.issues == nil {
		state.issues = make(map[string]*exporterIssueState)
	}
	var messages []string
	if !result.Up {
		state.consecutiveSuccesses = 0
		state.consecutiveFailures++
		if state.firstFailureAt.IsZero() {
			state.firstFailureAt = result.CheckedAt
		}
		state.lastError = result.ErrorMessage
		if state.status != "down" && state.consecutiveFailures >= target.FailureThreshold {
			state.status = "down"
			state.downSince = state.firstFailureAt
			messages = append(messages, buildExporterDownMessage(target, result))
		}
		return messages
	}

	state.consecutiveFailures = 0
	state.firstFailureAt = time.Time{}
	if state.status == "down" {
		state.consecutiveSuccesses++
		if state.consecutiveSuccesses >= target.RecoveryThreshold {
			messages = append(messages, buildExporterRecoveryMessage(target, state, result))
			state.status = "up"
			state.consecutiveSuccesses = 0
		}
	} else {
		state.status = "up"
		state.consecutiveSuccesses = 0
	}

	current := make(map[string]ExporterIssue, len(result.Issues))
	var newlyActive []ExporterIssue
	for _, issue := range result.Issues {
		if _, duplicate := current[issue.Key]; duplicate {
			continue
		}
		current[issue.Key] = issue
		tracked := state.issues[issue.Key]
		if tracked == nil {
			tracked = &exporterIssueState{}
			state.issues[issue.Key] = tracked
		}
		tracked.BadCount++
		tracked.GoodCount = 0
		tracked.Last = issue
		if !tracked.Active && tracked.BadCount >= target.FailureThreshold {
			tracked.Active = true
			newlyActive = append(newlyActive, issue)
		}
	}

	var recovered []ExporterIssue
	for key, tracked := range state.issues {
		if _, exists := current[key]; exists {
			continue
		}
		tracked.BadCount = 0
		if !tracked.Active {
			delete(state.issues, key)
			continue
		}
		tracked.GoodCount++
		if tracked.GoodCount >= target.RecoveryThreshold {
			recovered = append(recovered, tracked.Last)
			delete(state.issues, key)
		}
	}
	if len(newlyActive) > 0 {
		messages = append(messages, buildExporterIssueMessage(target, result.CheckedAt, newlyActive))
	}
	if len(recovered) > 0 {
		messages = append(messages, buildExporterIssueRecoveryMessage(target, result.CheckedAt, recovered))
	}
	return messages
}

func buildExporterDownMessage(target PrometheusTargetConfig, result ExporterResult) string {
	return fmt.Sprintf("🚨 *Prometheus Target Down*\n`%s` (%s)\nCause: %s\nError: %s\nConfirmed: %d consecutive failed scrapes\nChecked: %s",
		target.Name, target.Type, displayKind(result.ErrorKind), result.ErrorMessage, target.FailureThreshold, result.CheckedAt.Format(time.RFC3339))
}

func buildExporterRecoveryMessage(target PrometheusTargetConfig, state *exporterState, result ExporterResult) string {
	duration := "unknown"
	if !state.downSince.IsZero() {
		duration = result.CheckedAt.Sub(state.downSince).Round(time.Second).String()
	}
	return fmt.Sprintf("✅ *Prometheus Target Recovered*\n`%s` (%s)\nStable after %d successful scrapes\nOutage duration: %s\nPrevious error: %s\nChecked: %s",
		target.Name, target.Type, target.RecoveryThreshold, duration, state.lastError, result.CheckedAt.Format(time.RFC3339))
}

func buildExporterIssueMessage(target PrometheusTargetConfig, checkedAt time.Time, issues []ExporterIssue) string {
	var builder strings.Builder
	builder.WriteString(fmt.Sprintf("🔥 *Server Resource Alert: %s*\n", target.Name))
	for _, issue := range issues {
		builder.WriteString(fmt.Sprintf("\n- *%s*: %s", issue.Kind, issue.Summary))
	}
	builder.WriteString(fmt.Sprintf("\n\nConfirmed: %d consecutive scrapes\nChecked: %s", target.FailureThreshold, checkedAt.Format(time.RFC3339)))
	return builder.String()
}

func buildExporterIssueRecoveryMessage(target PrometheusTargetConfig, checkedAt time.Time, issues []ExporterIssue) string {
	var builder strings.Builder
	builder.WriteString(fmt.Sprintf("✅ *Server Resource Recovered: %s*\n", target.Name))
	for _, issue := range issues {
		builder.WriteString(fmt.Sprintf("\n- *%s*: %s", issue.Kind, issue.Summary))
	}
	builder.WriteString(fmt.Sprintf("\n\nStable after %d successful scrapes\nChecked: %s", target.RecoveryThreshold, checkedAt.Format(time.RFC3339)))
	return builder.String()
}

func firstMetricValue(family *dto.MetricFamily) (float64, bool) {
	if family == nil || len(family.Metric) == 0 {
		return 0, false
	}
	return metricValue(family.Metric[0])
}

func metricValue(metric *dto.Metric) (float64, bool) {
	switch {
	case metric.Gauge != nil:
		return metric.Gauge.GetValue(), true
	case metric.Counter != nil:
		return metric.Counter.GetValue(), true
	case metric.Untyped != nil:
		return metric.Untyped.GetValue(), true
	default:
		return 0, false
	}
}

func metricLabel(metric *dto.Metric, name string) string {
	for _, pair := range metric.Label {
		if pair.GetName() == name {
			return pair.GetValue()
		}
	}
	return ""
}

func filesystemKey(device, mountpoint string) string {
	return device + "\x00" + mountpoint
}

func ignoreFilesystem(mountpoint, fsType string) bool {
	ignoredTypes := map[string]struct{}{
		"autofs": {}, "binfmt_misc": {}, "bpf": {}, "cgroup": {}, "cgroup2": {}, "configfs": {},
		"debugfs": {}, "devpts": {}, "devtmpfs": {}, "fusectl": {}, "hugetlbfs": {}, "mqueue": {},
		"nsfs": {}, "overlay": {}, "proc": {}, "pstore": {}, "rpc_pipefs": {}, "securityfs": {},
		"selinuxfs": {}, "squashfs": {}, "sysfs": {}, "tracefs": {}, "tmpfs": {},
	}
	if _, ignored := ignoredTypes[fsType]; ignored {
		return true
	}
	for _, prefix := range []string{"/dev", "/proc", "/sys", "/run"} {
		if mountpoint == prefix || strings.HasPrefix(mountpoint, prefix+"/") {
			return true
		}
	}
	return false
}

func ignoreDiskDevice(device string) bool {
	for _, prefix := range []string{"loop", "ram", "fd", "sr"} {
		if strings.HasPrefix(device, prefix) {
			return true
		}
	}
	return false
}

func clampPercent(value float64) float64 {
	return math.Max(0, math.Min(100, value))
}

func thresholdEnabled(value float64) bool {
	return value >= 0
}

func sanitizeIssueKey(value string) string {
	return strings.NewReplacer(" ", "_", ":", "_", "/", "_").Replace(value)
}

func joinNonEmpty(values ...string) string {
	var nonEmpty []string
	for _, value := range values {
		if value != "" {
			nonEmpty = append(nonEmpty, value)
		}
	}
	return strings.Join(nonEmpty, "/")
}

func optionalPercent(value float64, valid bool) string {
	if !valid {
		return "n/a"
	}
	return fmt.Sprintf("%.1f%%", value)
}
