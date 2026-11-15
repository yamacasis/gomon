package main

import (
	"strings"
	"testing"
	"time"
)

func TestMonitorDebouncesDownAndRecovery(t *testing.T) {
	site := Website{
		URL:               "https://example.com",
		FailureThreshold:  2,
		RecoveryThreshold: 2,
	}
	state := &monitorState{}
	now := time.Date(2026, 11, 15, 7, 9, 20, 0, time.UTC)
	down := func(at time.Time) CheckResult {
		return CheckResult{
			URL:        site.URL,
			CheckedAt:  at,
			Kind:       "timeout",
			StatusMsg:  "connection timed out",
			Duration:   15 * time.Second,
			Attempts:   3,
			RemoteAddr: "203.0.113.10:443",
		}
	}
	up := func(at time.Time) CheckResult {
		return CheckResult{URL: site.URL, Up: true, CheckedAt: at, StatusMsg: "HTTP 200", Attempts: 1}
	}

	if messages, _ := updateMonitorState(site, state, down(now), false); len(messages) != 0 {
		t.Fatalf("first failure must not alert, got %v", messages)
	}
	if messages, _ := updateMonitorState(site, state, up(now.Add(time.Second)), false); len(messages) != 0 {
		t.Fatalf("transient recovery must not alert, got %v", messages)
	}
	if messages, _ := updateMonitorState(site, state, down(now.Add(2*time.Second)), false); len(messages) != 0 {
		t.Fatalf("first consecutive failure must not alert, got %v", messages)
	}
	messages, recovered := updateMonitorState(site, state, down(now.Add(3*time.Second)), false)
	if recovered || len(messages) != 1 {
		t.Fatalf("second consecutive failure must send one down alert, recovered=%v messages=%v", recovered, messages)
	}
	if !strings.Contains(messages[0], "Cause: TIMEOUT") || !strings.Contains(messages[0], now.Add(3*time.Second).Format(time.RFC3339)) {
		t.Fatalf("down alert lacks current diagnostics: %s", messages[0])
	}
	for _, tag := range []string{"#gomon", "#webcheck", "#down", "#timeout", "#examplecom"} {
		if !strings.Contains(messages[0], tag) {
			t.Fatalf("down alert lacks tag %s: %s", tag, messages[0])
		}
	}

	if messages, _ := updateMonitorState(site, state, up(now.Add(4*time.Second)), false); len(messages) != 0 {
		t.Fatalf("first success must not recover, got %v", messages)
	}
	messages, recovered = updateMonitorState(site, state, up(now.Add(5*time.Second)), false)
	if !recovered || len(messages) != 1 {
		t.Fatalf("second success must recover once, recovered=%v messages=%v", recovered, messages)
	}
	if !strings.Contains(messages[0], now.Add(5*time.Second).Format(time.RFC3339)) {
		t.Fatalf("recovery alert must use recovery check time: %s", messages[0])
	}
	for _, tag := range []string{"#gomon", "#webcheck", "#recovery", "#examplecom"} {
		if !strings.Contains(messages[0], tag) {
			t.Fatalf("recovery alert lacks tag %s: %s", tag, messages[0])
		}
	}
}

func TestSSLAlertTags(t *testing.T) {
	message := buildAlertMsg(
		Website{URL: "https://example.com", SSLMinDays: 30},
		CheckResult{Up: true, SSLDays: 5, CheckedAt: time.Now()},
		false,
		true,
		2,
	)
	for _, tag := range []string{"#gomon", "#webcheck", "#ssl", "#examplecom"} {
		if !strings.Contains(message, tag) {
			t.Fatalf("SSL alert lacks tag %s: %s", tag, message)
		}
	}
}

func TestCloudflareNetworkFailuresAreNeutralButHTTPErrorsAlert(t *testing.T) {
	site := Website{
		URL:               "https://example.com",
		Cloudflare:        true,
		FailureThreshold:  2,
		RecoveryThreshold: 2,
	}
	state := &monitorState{status: "up"}
	now := time.Now().UTC()
	timeout := CheckResult{URL: site.URL, Kind: "timeout", StatusMsg: "awaiting headers", CheckedAt: now}

	if !shouldSuppressCloudflareNetworkFailure(site, timeout) {
		t.Fatal("expected a no-response Cloudflare timeout to be suppressed")
	}
	if messages, recovered := updateMonitorState(site, state, timeout, false); len(messages) != 0 || recovered {
		t.Fatalf("suppressed timeout changed alert state: messages=%v recovered=%v", messages, recovered)
	}
	if state.status != "up" || state.consecutiveFailures != 0 {
		t.Fatalf("suppressed timeout changed monitor state: %+v", state)
	}

	httpFailure := CheckResult{URL: site.URL, Kind: "http", StatusCode: 503, StatusMsg: "HTTP 503", CheckedAt: now.Add(time.Minute)}
	if shouldSuppressCloudflareNetworkFailure(site, httpFailure) {
		t.Fatal("HTTP errors must not be suppressed")
	}
	if messages, _ := updateMonitorState(site, state, httpFailure, false); len(messages) != 0 {
		t.Fatalf("first HTTP failure must debounce: %v", messages)
	}
	httpFailure.CheckedAt = now.Add(2 * time.Minute)
	if messages, _ := updateMonitorState(site, state, httpFailure, false); len(messages) != 1 || !strings.Contains(messages[0], "HTTP 503") {
		t.Fatalf("second HTTP failure must alert: %v", messages)
	} else if !strings.Contains(messages[0], "#cloudflare") {
		t.Fatalf("Cloudflare HTTP alert lacks tag: %s", messages[0])
	}

	timeout.Suppressed = true
	timeout.CheckedAt = now.Add(3 * time.Minute)
	if messages, recovered := updateMonitorState(site, state, timeout, false); len(messages) != 0 || recovered || state.status != "down" {
		t.Fatalf("a suppressed timeout must not recover an HTTP outage: messages=%v recovered=%v state=%+v", messages, recovered, state)
	}
}
