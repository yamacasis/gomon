package main

import (
	"fmt"
	"log"
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
	if cfg.Webhook.URL != "" {
		nn = append(nn, newWebhook(cfg.Webhook))
	}
	return nn, nil
}

func notify(notifiers []Notifier, text string) {
	for _, n := range notifiers {
		if err := n.send(text); err != nil {
			log.Printf("notifier error (%T): %v", n, err)
		}
	}
}
