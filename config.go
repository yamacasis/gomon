package main

import (
	"fmt"
	"os"
	"time"

	"gopkg.in/yaml.v3"
)

type TelegramConfig struct {
	BotToken string `yaml:"bot_token"`
	ChatID   string `yaml:"chat_id"`
	APIURL   string `yaml:"api_url"` // default: https://api.telegram.org
}

type WebhookConfig struct {
	URL     string            `yaml:"url"`
	Headers map[string]string `yaml:"headers"` // optional auth headers
}

type Config struct {
	Telegram  TelegramConfig `yaml:"telegram"`
	Webhook   WebhookConfig  `yaml:"webhook"`
	Heartbeat struct {
		Time string `yaml:"time"` // "HH:MM" 24-hour format
	} `yaml:"heartbeat"`
	LogFile  string    `yaml:"log_file"`
	Websites []Website `yaml:"websites"`
}

type Website struct {
	URL        string        `yaml:"url"`
	Interval   time.Duration `yaml:"interval"`
	SSL        bool          `yaml:"ssl"`
	SSLMinDays int           `yaml:"ssl_min_days"`
}

func loadConfig(path string) (*Config, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("reading config: %w", err)
	}
	var cfg Config
	if err := yaml.Unmarshal(data, &cfg); err != nil {
		return nil, fmt.Errorf("parsing config: %w", err)
	}
	if cfg.LogFile == "" {
		cfg.LogFile = "monitor.log"
	}
	if cfg.Heartbeat.Time == "" {
		cfg.Heartbeat.Time = "08:00"
	}
	for i := range cfg.Websites {
		if cfg.Websites[i].Interval == 0 {
			cfg.Websites[i].Interval = 60 * time.Second
		}
		if cfg.Websites[i].SSLMinDays == 0 {
			cfg.Websites[i].SSLMinDays = 30
		}
	}
	return &cfg, nil
}
