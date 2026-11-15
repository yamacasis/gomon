package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"strings"
	"time"
)

const defaultBaleAPI = "https://tapi.bale.ai"

type Bale struct {
	cfg    BaleConfig
	client *http.Client
}

func newBale(cfg BaleConfig) *Bale {
	if cfg.APIURL == "" {
		cfg.APIURL = defaultBaleAPI
	}
	cfg.Tags = normalizeRoutingTags(cfg.Tags)
	return &Bale{
		cfg:    cfg,
		client: &http.Client{Timeout: 15 * time.Second},
	}
}

func (b *Bale) routingTags() []string {
	return b.cfg.Tags
}

func (b *Bale) send(text string) error {
	if b.cfg.BotToken == "" || b.cfg.ChatID == "" {
		return nil
	}

	log.Printf("[BALE] sending to chat %s via %s", b.cfg.ChatID, b.cfg.APIURL)
	endpoint := fmt.Sprintf("%s/bot%s/sendMessage", strings.TrimRight(b.cfg.APIURL, "/"), b.cfg.BotToken)
	payload, err := json.Marshal(map[string]string{
		"chat_id":    b.cfg.ChatID,
		"text":       text,
		"parse_mode": "Markdown",
	})
	if err != nil {
		return fmt.Errorf("bale encode request: %w", err)
	}

	response, err := b.client.Post(endpoint, "application/json", bytes.NewReader(payload))
	if err != nil {
		return fmt.Errorf("bale request: %w", err)
	}
	defer response.Body.Close()
	body, _ := io.ReadAll(io.LimitReader(response.Body, 64<<10))

	if response.StatusCode < 200 || response.StatusCode >= 300 {
		return fmt.Errorf("bale returned HTTP %d", response.StatusCode)
	}
	var apiResponse struct {
		OK          bool   `json:"ok"`
		Description string `json:"description"`
	}
	if len(body) > 0 && json.Unmarshal(body, &apiResponse) == nil && !apiResponse.OK {
		if apiResponse.Description == "" {
			apiResponse.Description = "unknown API error"
		}
		return fmt.Errorf("bale API error: %s", apiResponse.Description)
	}
	log.Printf("[BALE] sent ok")
	return nil
}
