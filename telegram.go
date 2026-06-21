package main

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"log"
	"net"
	"net/http"
	"net/url"
	"time"

	"golang.org/x/net/proxy"
)

const defaultTelegramAPI = "https://api.telegram.org"

type Telegram struct {
	cfg    TelegramConfig
	client *http.Client
}

func newTelegram(cfg TelegramConfig) (*Telegram, error) {
	if cfg.APIURL == "" {
		cfg.APIURL = defaultTelegramAPI
	}

	client, err := buildClient(cfg.SocksProxy)
	if err != nil {
		return nil, fmt.Errorf("telegram http client: %w", err)
	}

	if cfg.SocksProxy != "" {
		log.Printf("[TELEGRAM] using SOCKS5 proxy: %s", cfg.SocksProxy)
	}

	return &Telegram{cfg: cfg, client: client}, nil
}

func (t *Telegram) send(text string) error {
	if t.cfg.BotToken == "" || t.cfg.ChatID == "" {
		return nil
	}

	log.Printf("[TELEGRAM] sending to chat %s via %s", t.cfg.ChatID, t.cfg.APIURL)

	endpoint := fmt.Sprintf("%s/bot%s/sendMessage", t.cfg.APIURL, t.cfg.BotToken)

	payload, _ := json.Marshal(map[string]string{
		"chat_id":    t.cfg.ChatID,
		"text":       text,
		"parse_mode": "Markdown",
	})

	resp, err := t.client.Post(endpoint, "application/json", bytes.NewReader(payload))
	if err != nil {
		return fmt.Errorf("telegram request: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != 200 {
		err := fmt.Errorf("telegram returned HTTP %d", resp.StatusCode)
		log.Printf("[TELEGRAM] error: %v", err)
		return err
	}
	log.Printf("[TELEGRAM] sent ok")
	return nil
}

func buildClient(socksProxy string) (*http.Client, error) {
	transport := &http.Transport{}

	if socksProxy != "" {
		u, err := url.Parse(socksProxy)
		if err != nil {
			return nil, fmt.Errorf("invalid socks proxy URL %q: %w", socksProxy, err)
		}

		dialer, err := proxy.FromURL(u, proxy.Direct)
		if err != nil {
			return nil, fmt.Errorf("socks5 dialer: %w", err)
		}

		transport.DialContext = func(ctx context.Context, network, addr string) (net.Conn, error) {
			return dialer.Dial(network, addr)
		}
	}

	return &http.Client{
		Timeout:   15 * time.Second,
		Transport: transport,
	}, nil
}
