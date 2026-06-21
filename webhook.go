package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"time"
)

type Webhook struct {
	cfg WebhookConfig
}

func newWebhook(cfg WebhookConfig) *Webhook {
	return &Webhook{cfg: cfg}
}

func (w *Webhook) send(text string) error {
	if w.cfg.URL == "" {
		return nil
	}

	payload, _ := json.Marshal(map[string]string{
		"text":      text,
		"timestamp": time.Now().UTC().Format(time.RFC3339),
	})

	req, err := http.NewRequest(http.MethodPost, w.cfg.URL, bytes.NewReader(payload))
	if err != nil {
		return fmt.Errorf("webhook build request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	for k, v := range w.cfg.Headers {
		req.Header.Set(k, v)
	}

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return fmt.Errorf("webhook request: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return fmt.Errorf("webhook returned HTTP %d", resp.StatusCode)
	}
	return nil
}
