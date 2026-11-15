package main

import (
	"fmt"
	"log"
	"net/url"
	"strings"
	"unicode"
)

type Notifier interface {
	send(text string) error
}

func buildNotifiers(cfg *Config) ([]Notifier, error) {
	var nn []Notifier
	if cfg.Telegram.BotToken != "" && cfg.Telegram.ChatID != "" {
		tg, err := newTelegram(cfg.Telegram)
		if err != nil {
			return nil, fmt.Errorf("telegram notifier: %w", err)
		}
		nn = append(nn, tg)
	}
	if cfg.Bale.BotToken != "" && cfg.Bale.ChatID != "" {
		nn = append(nn, newBale(cfg.Bale))
	}
	if cfg.Webhook.URL != "" {
		nn = append(nn, newWebhook(cfg.Webhook))
	}
	return nn, nil
}

func withAlertTags(text string, tags ...string) string {
	allTags := append([]string{"gomon"}, tags...)
	seen := make(map[string]struct{}, len(allTags))
	formatted := make([]string, 0, len(allTags))
	for _, raw := range allTags {
		tag := normalizeAlertTag(raw)
		if tag == "" {
			continue
		}
		if _, duplicate := seen[tag]; duplicate {
			continue
		}
		seen[tag] = struct{}{}
		formatted = append(formatted, "#"+tag)
	}
	if len(formatted) == 0 {
		return text
	}
	return strings.TrimRight(text, "\n") + "\n\n" + strings.Join(formatted, " ")
}

func normalizeAlertTag(raw string) string {
	var builder strings.Builder
	for _, r := range strings.ToLower(strings.TrimSpace(strings.TrimPrefix(raw, "#"))) {
		if unicode.IsLetter(r) || unicode.IsDigit(r) {
			builder.WriteRune(r)
		}
	}
	return builder.String()
}

func siteAlertTag(raw string) string {
	candidate := strings.TrimSpace(raw)
	if !strings.Contains(candidate, "://") {
		candidate = "//" + candidate
	}
	parsed, err := url.Parse(candidate)
	if err == nil && parsed.Hostname() != "" {
		return parsed.Hostname()
	}
	return raw
}

func notify(notifiers []Notifier, text string) {
	for _, n := range notifiers {
		if err := n.send(text); err != nil {
			log.Printf("notifier error (%T): %v", n, err)
		}
	}
}
