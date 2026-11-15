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
	routingTags() []string
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
	for _, channel := range cfg.TelegramChannels {
		tg, err := newTelegram(channel)
		if err != nil {
			return nil, fmt.Errorf("telegram channel %q: %w", channel.Name, err)
		}
		nn = append(nn, tg)
	}
	if cfg.Bale.BotToken != "" && cfg.Bale.ChatID != "" {
		nn = append(nn, newBale(cfg.Bale))
	}
	for _, channel := range cfg.BaleChannels {
		nn = append(nn, newBale(channel))
	}
	if cfg.Webhook.URL != "" {
		nn = append(nn, newWebhook(cfg.Webhook))
	}
	return nn, nil
}

func normalizeRoutingTags(tags []string) []string {
	normalized := make([]string, 0, len(tags))
	seen := make(map[string]struct{}, len(tags))
	for _, raw := range tags {
		tag := normalizeAlertTag(raw)
		if tag == "" {
			continue
		}
		if _, duplicate := seen[tag]; duplicate {
			continue
		}
		seen[tag] = struct{}{}
		normalized = append(normalized, tag)
	}
	if len(normalized) == 0 {
		return []string{"general"}
	}
	return normalized
}

func routingTagsMatch(channelTags, eventTags []string) bool {
	channels := normalizeRoutingTags(channelTags)
	events := normalizeRoutingTags(eventTags)
	eventSet := make(map[string]struct{}, len(events))
	for _, tag := range events {
		eventSet[tag] = struct{}{}
	}
	for _, tag := range channels {
		if _, match := eventSet[tag]; match {
			return true
		}
	}
	return false
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

func notify(notifiers []Notifier, text string, eventTags ...string) {
	for _, n := range notifiers {
		if !routingTagsMatch(n.routingTags(), eventTags) {
			continue
		}
		if err := n.send(text); err != nil {
			log.Printf("notifier error (%T): %v", n, err)
		}
	}
}
