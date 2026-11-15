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
}
