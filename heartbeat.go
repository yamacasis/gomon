package main

import (
	"fmt"
	"log"
	"strings"
	"time"
)


func runHeartbeat(cfg *Config, notifiers []Notifier, logger *Logger) {
	for {
		next := nextFireTime(cfg.Heartbeat.Time)
		time.Sleep(time.Until(next))

		report, err := buildReport(cfg, logger)
		if err != nil {
			log.Printf("heartbeat report error: %v", err)
		} else {
			notify(notifiers, report)
		}
	}
}

func nextFireTime(hhmm string) time.Time {
	now := time.Now()
	var hour, min int
	fmt.Sscanf(hhmm, "%d:%d", &hour, &min)
	next := time.Date(now.Year(), now.Month(), now.Day(), hour, min, 0, 0, now.Location())
	if !next.After(now) {
		next = next.Add(24 * time.Hour)
	}
	return next
}

func buildReport(cfg *Config, logger *Logger) (string, error) {
	yesterday := time.Now().UTC().AddDate(0, 0, -1)
	dateStr := yesterday.Format("2006-01-02")

	entries, err := logger.readDate(dateStr)
	if err != nil {
		return "", err
	}

	type stat struct {
		up      int
		down    int
		lastSSL int
	}

	stats := make(map[string]*stat, len(cfg.Websites))
	for _, s := range cfg.Websites {
		stats[s.URL] = &stat{lastSSL: -1}
	}

	for _, e := range entries {
		st, ok := stats[e.URL]
		if !ok {
			st = &stat{lastSSL: -1}
			stats[e.URL] = st
		}
		if e.Status == "UP" {
			st.up++
		} else {
			st.down++
		}
		if e.SSLDays >= 0 {
			st.lastSSL = e.SSLDays
		}
	}

	var sb strings.Builder
	sb.WriteString(fmt.Sprintf("📊 *Daily Report — %s*\n", dateStr))

	if len(entries) == 0 {
		sb.WriteString("\nNo monitoring data recorded for this date.")
		return sb.String(), nil
	}

	sb.WriteString("\n")
	for _, site := range cfg.Websites {
		st := stats[site.URL]
		total := st.up + st.down

		uptime := 0.0
		if total > 0 {
			uptime = float64(st.up) / float64(total) * 100
		}

		icon := "✅"
		if st.down > 0 {
			icon = "⚠️"
		}
		if st.up == 0 && total > 0 {
			icon = "🔴"
		}

		sb.WriteString(fmt.Sprintf("%s `%s`\n", icon, site.URL))
		sb.WriteString(fmt.Sprintf("   Uptime: %.1f%% (%d/%d checks)\n", uptime, st.up, total))
		if st.lastSSL >= 0 {
			sb.WriteString(fmt.Sprintf("   SSL: %d days remaining\n", st.lastSSL))
		}
		sb.WriteString("\n")
	}

	return strings.TrimRight(sb.String(), "\n"), nil
}
