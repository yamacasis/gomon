package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
)

const defaultTelegramAPI = "https://api.telegram.org"

type Telegram struct {
	cfg TelegramConfig
}

func newTelegram(cfg TelegramConfig) *Telegram {
	if cfg.APIURL == "" {
		cfg.APIURL = defaultTelegramAPI
	}
	return &Telegram{cfg: cfg}
}

func (t *Telegram) send(text string) error {
	if t.cfg.BotToken == "" || t.cfg.ChatID == "" {
		return nil
	}

	url := fmt.Sprintf("%s/bot%s/sendMessage", t.cfg.APIURL, t.cfg.BotToken)

	payload, _ := json.Marshal(map[string]string{
		"chat_id":    t.cfg.ChatID,
		"text":       text,
		"parse_mode": "Markdown",
	})

	resp, err := http.Post(url, "application/json", bytes.NewReader(payload))
	if err != nil {
		return fmt.Errorf("telegram request: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != 200 {
		return fmt.Errorf("telegram returned HTTP %d", resp.StatusCode)
	}
	return nil
}
