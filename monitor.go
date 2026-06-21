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

func runMonitor(site Website, tg *Telegram, logger *Logger) {
	state := &monitorState{}

	// Check immediately on startup, then tick
	doCheck(site, state, tg, logger)

	ticker := time.NewTicker(site.Interval)
	defer ticker.Stop()
	for range ticker.C {
		doCheck(site, state, tg, logger)
	}
}

func doCheck(site Website, state *monitorState, tg *Telegram, logger *Logger) {
	result := checkSite(site)
	logger.write(result)

	sslExpiring := site.SSL && result.SSLDays >= 0 && result.SSLDays < site.SSLMinDays

	newDown := !result.Up && !state.wasDown
	newSSL := sslExpiring && !state.sslAlerted

	if newDown || newSSL {
		msg := buildAlertMsg(site, result, !result.Up, sslExpiring)
		if err := tg.send(msg); err != nil {
			log.Printf("telegram alert error: %v", err)
		}
	}

	// Recovery notice
	if result.Up && state.wasDown {
		msg := fmt.Sprintf("✅ *Site Recovered*\n`%s` is back online", site.URL)
		if err := tg.send(msg); err != nil {
			log.Printf("telegram recovery error: %v", err)
		}
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
