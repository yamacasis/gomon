package main

import (
	"fmt"
	"log"
	"strings"
	"time"
)

type monitorState struct {
	status               string
	consecutiveFailures  int
	consecutiveSuccesses int
	firstFailureAt       time.Time
	downSince            time.Time
	lastFailure          CheckResult
	sslAlerted           bool
}

func runMonitor(site Website, notifiers []Notifier, logger *Logger, metrics *monitorMetrics) {
	state := &monitorState{}

	doCheck(site, state, notifiers, logger, metrics)

	ticker := time.NewTicker(site.Interval)
	defer ticker.Stop()
	for range ticker.C {
		doCheck(site, state, notifiers, logger, metrics)
	}
}

func doCheck(site Website, state *monitorState, notifiers []Notifier, logger *Logger, metrics *monitorMetrics) {
	result := checkSite(site)
	logger.write(result)

	sslExpiring := site.SSL && result.SSLDays >= 0 && result.SSLDays < site.SSLMinDays

	// Console log every check
	if result.Up {
		if result.SSLDays >= 0 {
			log.Printf("[CHECK] UP   %s  %s  ssl=%dd", site.URL, result.StatusMsg, result.SSLDays)
		} else {
			log.Printf("[CHECK] UP   %s  %s", site.URL, result.StatusMsg)
		}
	} else {
		log.Printf("[CHECK] DOWN %s  kind=%s attempts=%d duration=%s remote=%s error=%s",
			site.URL, result.Kind, result.Attempts, result.Duration.Round(time.Millisecond), result.RemoteAddr, result.StatusMsg)
	}
	if sslExpiring {
		log.Printf("[CHECK] SSL  %s  expires in %d days (min %d)", site.URL, result.SSLDays, site.SSLMinDays)
	}

	messages, recovered := updateMonitorState(site, state, result, sslExpiring)
	metrics.recordSite(site, state, result)
	if recovered {
		log.Printf("[CHECK] RECOVERED %s after %d consecutive successful checks", site.URL, site.RecoveryThreshold)
	}
	for _, message := range messages {
		notify(notifiers, message)
	}
}

func updateMonitorState(site Website, state *monitorState, result CheckResult, sslExpiring bool) ([]string, bool) {
	failureThreshold := site.FailureThreshold
	if failureThreshold <= 0 {
		failureThreshold = defaultFailureThreshold
	}
	recoveryThreshold := site.RecoveryThreshold
	if recoveryThreshold <= 0 {
		recoveryThreshold = defaultRecoveryThreshold
	}

	var messages []string
	recovered := false
	newSSL := sslExpiring && !state.sslAlerted

	if result.Up {
		state.consecutiveFailures = 0
		state.firstFailureAt = time.Time{}
		if state.status == "down" {
			state.consecutiveSuccesses++
			if state.consecutiveSuccesses >= recoveryThreshold {
				messages = append(messages, buildRecoveryMsg(site, state, result, recoveryThreshold))
				state.status = "up"
				state.consecutiveSuccesses = 0
				recovered = true
			}
		} else {
			state.status = "up"
			state.consecutiveSuccesses = 0
		}
	} else {
		state.consecutiveSuccesses = 0
		state.consecutiveFailures++
		state.lastFailure = result
		if state.firstFailureAt.IsZero() {
			state.firstFailureAt = result.CheckedAt
		}
		if state.status != "down" && state.consecutiveFailures >= failureThreshold {
			state.status = "down"
			state.downSince = state.firstFailureAt
			messages = append(messages, buildAlertMsg(site, result, true, newSSL, failureThreshold))
			newSSL = false
		}
	}

	if newSSL {
		messages = append(messages, buildAlertMsg(site, result, false, true, failureThreshold))
	}
	state.sslAlerted = sslExpiring
	return messages, recovered
}

func buildAlertMsg(site Website, result CheckResult, isDown, sslExpiring bool, failureThreshold int) string {
	var sb strings.Builder
	tags := []string{"webcheck", siteAlertTag(site.URL)}
	sb.WriteString(fmt.Sprintf("🚨 *Alert: %s*\n", site.URL))

	if isDown {
		tags = append(tags, "down", result.Kind)
		sb.WriteString(fmt.Sprintf("\n🔴 *Site is DOWN*\nCause: %s\nError: %s", displayKind(result.Kind), result.StatusMsg))
		sb.WriteString(fmt.Sprintf("\nConfirmed: %d consecutive failed checks; %d request attempt(s) in the last check",
			failureThreshold, result.Attempts))
		sb.WriteString(fmt.Sprintf("\nDuration: %s", result.Duration.Round(time.Millisecond)))
		if result.RemoteAddr != "" {
			sb.WriteString(fmt.Sprintf("\nRemote: %s", result.RemoteAddr))
		}
		sb.WriteString(fmt.Sprintf("\nChecked: %s", result.CheckedAt.Format(time.RFC3339)))
	}

	if sslExpiring {
		tags = append(tags, "ssl")
		if isDown {
			sb.WriteString("\n")
		}
		sb.WriteString(fmt.Sprintf("\n⚠️ *SSL Certificate Expiring*\nDays remaining: %d (min allowed: %d)",
			result.SSLDays, site.SSLMinDays))
	}

	return withAlertTags(sb.String(), tags...)
}

func buildRecoveryMsg(site Website, state *monitorState, result CheckResult, recoveryThreshold int) string {
	var sb strings.Builder
	sb.WriteString("✅ *Site Recovered*\n")
	sb.WriteString(fmt.Sprintf("`%s` is stable after %d consecutive successful checks", site.URL, recoveryThreshold))
	if !state.downSince.IsZero() {
		sb.WriteString(fmt.Sprintf("\nOutage duration: %s", result.CheckedAt.Sub(state.downSince).Round(time.Second)))
	}
	sb.WriteString(fmt.Sprintf("\nCurrent status: %s", result.StatusMsg))
	if state.lastFailure.StatusMsg != "" {
		sb.WriteString(fmt.Sprintf("\nPrevious cause: %s — %s", displayKind(state.lastFailure.Kind), state.lastFailure.StatusMsg))
	}
	sb.WriteString(fmt.Sprintf("\nChecked: %s", result.CheckedAt.Format(time.RFC3339)))
	return withAlertTags(sb.String(), "webcheck", "recovery", siteAlertTag(site.URL))
}

func displayKind(kind string) string {
	if kind == "" {
		return "unknown"
	}
	return strings.ToUpper(kind)
}
