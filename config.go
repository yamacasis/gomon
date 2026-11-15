package main

import (
	"fmt"
	"net/url"
	"os"
	"strings"
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
	Metrics   MetricsConfig  `yaml:"metrics"`
	Heartbeat struct {
		Time string `yaml:"time"` // "HH:MM" 24-hour format
	} `yaml:"heartbeat"`
	LogFile           string                   `yaml:"log_file"`
	Websites          []Website                `yaml:"websites"`
	PrometheusTargets []PrometheusTargetConfig `yaml:"prometheus_targets"`
}

type MetricsConfig struct {
	Port          int    `yaml:"port"`
	ListenAddress string `yaml:"listen_address"`
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

type PrometheusTargetConfig struct {
	Name              string                 `yaml:"name"`
	URL               string                 `yaml:"url"`
	Type              string                 `yaml:"type"`
	BasicAuth         *BasicAuthConfig       `yaml:"basic_auth"`
	Headers           map[string]string      `yaml:"headers"`
	Interval          time.Duration          `yaml:"interval"`
	Timeout           time.Duration          `yaml:"timeout"`
	FailureThreshold  int                    `yaml:"failure_threshold"`
	RecoveryThreshold int                    `yaml:"recovery_threshold"`
	Thresholds        NodeExporterThresholds `yaml:"thresholds"`
}

type BasicAuthConfig struct {
	Username string `yaml:"username"`
	Password string `yaml:"password"`
}

type NodeExporterThresholds struct {
	CPUPercent         float64 `yaml:"cpu_percent"`
	RAMPercent         float64 `yaml:"ram_percent"`
	DiskPercent        float64 `yaml:"disk_percent"`
	DiskIOPercent      float64 `yaml:"disk_io_percent"`
	LoadPerCPU         float64 `yaml:"load_per_cpu"`
	TemperatureCelsius float64 `yaml:"temperature_celsius"`
}

const (
	defaultCheckInterval     = 60 * time.Second
	defaultCheckTimeout      = 15 * time.Second
	defaultCheckAttempts     = 3
	defaultRetryDelay        = time.Second
	defaultFailureThreshold  = 2
	defaultRecoveryThreshold = 2
	defaultExporterInterval  = 60 * time.Second
	defaultExporterTimeout   = 10 * time.Second
	defaultCPUPercent        = 90.0
	defaultRAMPercent        = 90.0
	defaultDiskPercent       = 90.0
	defaultDiskIOPercent     = 95.0
	defaultLoadPerCPU        = 2.0
	defaultTemperature       = 85.0
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
	if cfg.Metrics.Port < 0 || cfg.Metrics.Port > 65535 {
		return nil, fmt.Errorf("metrics.port must be between 0 and 65535")
	}
	if cfg.Metrics.Port > 0 && cfg.Metrics.ListenAddress == "" {
		cfg.Metrics.ListenAddress = "0.0.0.0"
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
	seenTargetNames := make(map[string]struct{}, len(cfg.PrometheusTargets))
	for i := range cfg.PrometheusTargets {
		target := &cfg.PrometheusTargets[i]
		target.Name = strings.TrimSpace(target.Name)
		if target.Name == "" {
			return nil, fmt.Errorf("prometheus_targets[%d].name is required", i)
		}
		if _, exists := seenTargetNames[target.Name]; exists {
			return nil, fmt.Errorf("prometheus target name %q is duplicated", target.Name)
		}
		seenTargetNames[target.Name] = struct{}{}

		target.Type = strings.ToLower(strings.TrimSpace(target.Type))
		if target.Type == "" {
			target.Type = "node_exporter"
		}
		if target.Type != "node_exporter" {
			return nil, fmt.Errorf("prometheus target %q has unsupported type %q", target.Name, target.Type)
		}
		normalizedURL, err := normalizePrometheusURL(target.URL)
		if err != nil {
			return nil, fmt.Errorf("prometheus target %q: %w", target.Name, err)
		}
		target.URL = normalizedURL
		if target.BasicAuth != nil {
			target.BasicAuth.Username = strings.TrimSpace(target.BasicAuth.Username)
			if target.BasicAuth.Username == "" {
				return nil, fmt.Errorf("prometheus target %q basic_auth.username is required", target.Name)
			}
			for header := range target.Headers {
				if strings.EqualFold(strings.TrimSpace(header), "Authorization") {
					return nil, fmt.Errorf("prometheus target %q cannot use basic_auth and an Authorization header together", target.Name)
				}
			}
		}
		if target.Interval <= 0 {
			target.Interval = defaultExporterInterval
		}
		if target.Timeout <= 0 {
			target.Timeout = defaultExporterTimeout
		}
		if target.FailureThreshold <= 0 {
			target.FailureThreshold = defaultFailureThreshold
		}
		if target.RecoveryThreshold <= 0 {
			target.RecoveryThreshold = defaultRecoveryThreshold
		}
		applyNodeExporterThresholdDefaults(&target.Thresholds)
	}
	return &cfg, nil
}

func normalizePrometheusURL(raw string) (string, error) {
	parsed, err := url.Parse(strings.TrimSpace(raw))
	if err != nil || parsed.Scheme == "" || parsed.Host == "" {
		return "", fmt.Errorf("url must be an absolute HTTP(S) URL")
	}
	if parsed.Scheme != "http" && parsed.Scheme != "https" {
		return "", fmt.Errorf("url scheme must be http or https")
	}
	if parsed.User != nil {
		return "", fmt.Errorf("credentials in url are not supported; use basic_auth instead")
	}
	if parsed.Path == "" || parsed.Path == "/" {
		parsed.Path = "/metrics"
	}
	return parsed.String(), nil
}

func applyNodeExporterThresholdDefaults(thresholds *NodeExporterThresholds) {
	if thresholds.CPUPercent == 0 {
		thresholds.CPUPercent = defaultCPUPercent
	}
	if thresholds.RAMPercent == 0 {
		thresholds.RAMPercent = defaultRAMPercent
	}
	if thresholds.DiskPercent == 0 {
		thresholds.DiskPercent = defaultDiskPercent
	}
	if thresholds.DiskIOPercent == 0 {
		thresholds.DiskIOPercent = defaultDiskIOPercent
	}
	if thresholds.LoadPerCPU == 0 {
		thresholds.LoadPerCPU = defaultLoadPerCPU
	}
	if thresholds.TemperatureCelsius == 0 {
		thresholds.TemperatureCelsius = defaultTemperature
	}
}
