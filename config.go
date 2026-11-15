package main

import (
	"fmt"
	"os"
	"time"

	"gopkg.in/yaml.v3"
)

type TelegramConfig struct {
	BotToken   string `yaml:"bot_token"`
	ChatID     string `yaml:"chat_id"`
	APIURL     string `yaml:"api_url"`     // default: https://api.telegram.org
	SocksProxy string `yaml:"socks_proxy"` // e.g. socks5://user:pass@host:1080
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
	URL               string        `yaml:"url"`
	Interval          time.Duration `yaml:"interval"`
	Timeout           time.Duration `yaml:"timeout"`
	Attempts          int           `yaml:"attempts"`
	RetryDelay        time.Duration `yaml:"retry_delay"`
	FailureThreshold  int           `yaml:"failure_threshold"`
	RecoveryThreshold int           `yaml:"recovery_threshold"`
	SSL               bool          `yaml:"ssl"`
	SSLMinDays        int           `yaml:"ssl_min_days"`
}

const (
	defaultCheckInterval     = 60 * time.Second
	defaultCheckTimeout      = 15 * time.Second
	defaultCheckAttempts     = 3
	defaultRetryDelay        = time.Second
	defaultFailureThreshold  = 2
	defaultRecoveryThreshold = 2
)

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
		if cfg.Websites[i].Interval <= 0 {
			cfg.Websites[i].Interval = defaultCheckInterval
		}
		if cfg.Websites[i].Timeout <= 0 {
			cfg.Websites[i].Timeout = defaultCheckTimeout
		}
		if cfg.Websites[i].Attempts <= 0 {
			cfg.Websites[i].Attempts = defaultCheckAttempts
		}
		if cfg.Websites[i].RetryDelay < 0 {
			return nil, fmt.Errorf("websites[%d].retry_delay must not be negative", i)
		}
		if cfg.Websites[i].RetryDelay == 0 {
			cfg.Websites[i].RetryDelay = defaultRetryDelay
		}
		if cfg.Websites[i].FailureThreshold <= 0 {
			cfg.Websites[i].FailureThreshold = defaultFailureThreshold
		}
		if cfg.Websites[i].RecoveryThreshold <= 0 {
			cfg.Websites[i].RecoveryThreshold = defaultRecoveryThreshold
		}
		if cfg.Websites[i].SSLMinDays == 0 {
			cfg.Websites[i].SSLMinDays = 30
		}
	}
	return &cfg, nil
}
