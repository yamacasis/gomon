package main

import (
	"fmt"
	"log"
	"strings"
	"time"
)

type monitorState struct {
	wasDown    bool
	sslAlerted bool
}

func runMonitor(site Website, notifiers []Notifier, logger *Logger) {
	state := &monitorState{}

	doCheck(site, state, notifiers, logger)

	ticker := time.NewTicker(site.Interval)
	defer ticker.Stop()
	for range ticker.C {
		doCheck(site, state, notifiers, logger)
	}
}

func doCheck(site Website, state *monitorState, notifiers []Notifier, logger *Logger) {
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
		log.Printf("[CHECK] DOWN %s  %s", site.URL, result.StatusMsg)
	}
	if sslExpiring {
		log.Printf("[CHECK] SSL  %s  expires in %d days (min %d)", site.URL, result.SSLDays, site.SSLMinDays)
	}

	newDown := !result.Up && !state.wasDown
	newSSL := sslExpiring && !state.sslAlerted

	if newDown || newSSL {
		notify(notifiers, buildAlertMsg(site, result, !result.Up, sslExpiring))
	}

	if result.Up && state.wasDown {
		log.Printf("[CHECK] RECOVERED %s", site.URL)
		notify(notifiers, fmt.Sprintf("✅ *Site Recovered*\n`%s` is back online", site.URL))
	}

	state.wasDown = !result.Up
	state.sslAlerted = sslExpiring
}

func buildAlertMsg(site Website, result CheckResult, isDown, sslExpiring bool) string {
	var sb strings.Builder
	sb.WriteString(fmt.Sprintf("🚨 *Alert: %s*\n", site.URL))

	if isDown {
		sb.WriteString(fmt.Sprintf("\n🔴 *Site is DOWN*\nError: %s", result.StatusMsg))
	}

	if sslExpiring {
		if isDown {
			sb.WriteString("\n")
		}
		sb.WriteString(fmt.Sprintf("\n⚠️ *SSL Certificate Expiring*\nDays remaining: %d (min allowed: %d)",
			result.SSLDays, site.SSLMinDays))
	}

	return sb.String()
}
