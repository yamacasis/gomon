package main

import "log"

type Notifier interface {
	send(text string) error
}

func buildNotifiers(cfg *Config) []Notifier {
	var nn []Notifier
	if cfg.Telegram.BotToken != "" && cfg.Telegram.ChatID != "" {
		nn = append(nn, newTelegram(cfg.Telegram))
	}
	if cfg.Webhook.URL != "" {
		nn = append(nn, newWebhook(cfg.Webhook))
	}
	return nn
}

func notify(notifiers []Notifier, text string) {
	for _, n := range notifiers {
		if err := n.send(text); err != nil {
			log.Printf("notifier error (%T): %v", n, err)
		}
	}
}
