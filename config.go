package main

import (
	"fmt"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"time"

	"gopkg.in/yaml.v3"
)

type TelegramConfig struct {
	Name       string   `yaml:"name"`
	Tags       []string `yaml:"tags"`
	BotToken   string   `yaml:"bot_token"`
	ChatID     string   `yaml:"chat_id"`
	APIURL     string   `yaml:"api_url"`     // default: https://api.telegram.org
	SocksProxy string   `yaml:"socks_proxy"` // e.g. socks5://user:pass@host:1080
}

type BaleConfig struct {
	Name     string   `yaml:"name"`
	Tags     []string `yaml:"tags"`
	BotToken string   `yaml:"bot_token"`
	ChatID   string   `yaml:"chat_id"`
	APIURL   string   `yaml:"api_url"` // default: https://tapi.bale.ai
}

type WebhookConfig struct {
	URL     string            `yaml:"url"`
	Headers map[string]string `yaml:"headers"` // optional auth headers
}

type Config struct {
	Telegram         TelegramConfig     `yaml:"telegram"`
	TelegramChannels []TelegramConfig   `yaml:"telegram_channels"`
	Bale             BaleConfig         `yaml:"bale"`
	BaleChannels     []BaleConfig       `yaml:"bale_channels"`
	Webhook          WebhookConfig      `yaml:"webhook"`
	Notifications    NotificationConfig `yaml:"notifications"`
	Metrics          MetricsConfig      `yaml:"metrics"`
	Heartbeat        struct {
		Time string `yaml:"time"` // "HH:MM" 24-hour format
	} `yaml:"heartbeat"`
	LogFile           string                   `yaml:"log_file"`
	Websites          []Website                `yaml:"websites"`
	PrometheusTargets []PrometheusTargetConfig `yaml:"prometheus_targets"`
}

type NotificationConfig struct {
	QueueFile    string        `yaml:"queue_file"`
	DeliveryLog  string        `yaml:"delivery_log"`
	Timeout      time.Duration `yaml:"timeout"`
	InitialRetry time.Duration `yaml:"initial_retry"`
	MaxRetry     time.Duration `yaml:"max_retry"`
	MaxAttempts  int           `yaml:"max_attempts"`
}

type MetricsConfig struct {
	Port          int    `yaml:"port"`
	ListenAddress string `yaml:"listen_address"`
}

type Website struct {
	URL                 string                 `yaml:"url"`
	Method              string                 `yaml:"method"`
	Headers             map[string]string      `yaml:"headers"`
	BearerToken         string                 `yaml:"bearer_token"`
	Body                string                 `yaml:"body"`
	ExpectedStatus      int                    `yaml:"expected_status"`
	ExpectedStatuses    []int                  `yaml:"expected_statuses"`
	ResponseType        string                 `yaml:"response_type"`
	ResponseContains    []string               `yaml:"response_contains"`
	ResponseNotContains []string               `yaml:"response_not_contains"`
	JSONAssertions      map[string]interface{} `yaml:"json_assertions"`
	Tags                []string               `yaml:"tags"`
	Cloudflare          bool                   `yaml:"cloudflare"`
	Interval            time.Duration          `yaml:"interval"`
	Timeout             time.Duration          `yaml:"timeout"`
	Attempts            int                    `yaml:"attempts"`
	RetryDelay          time.Duration          `yaml:"retry_delay"`
	FailureThreshold    int                    `yaml:"failure_threshold"`
	RecoveryThreshold   int                    `yaml:"recovery_threshold"`
	SSL                 bool                   `yaml:"ssl"`
	SSLMinDays          int                    `yaml:"ssl_min_days"`
}

type PrometheusTargetConfig struct {
	Name              string                 `yaml:"name"`
	Tags              []string               `yaml:"tags"`
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
	defaultCheckInterval            = 60 * time.Second
	defaultCheckTimeout             = 15 * time.Second
	defaultCheckAttempts            = 3
	defaultRetryDelay               = time.Second
	defaultFailureThreshold         = 2
	defaultRecoveryThreshold        = 2
	defaultExporterInterval         = 60 * time.Second
	defaultExporterTimeout          = 10 * time.Second
	defaultCPUPercent               = 90.0
	defaultRAMPercent               = 90.0
	defaultDiskPercent              = 90.0
	defaultDiskIOPercent            = 95.0
	defaultLoadPerCPU               = 2.0
	defaultTemperature              = 85.0
	defaultNotificationTimeout      = 15 * time.Second
	defaultNotificationInitialRetry = 5 * time.Second
	defaultNotificationMaxRetry     = 5 * time.Minute
	defaultNotificationMaxAttempts  = 10
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
	if cfg.Notifications.QueueFile == "" {
		cfg.Notifications.QueueFile = filepath.Join(filepath.Dir(cfg.LogFile), "notification-queue.json")
	}
	if cfg.Notifications.DeliveryLog == "" {
		cfg.Notifications.DeliveryLog = filepath.Join(filepath.Dir(cfg.LogFile), "notification-deliveries.log")
	}
	if cfg.Notifications.Timeout <= 0 {
		cfg.Notifications.Timeout = defaultNotificationTimeout
	}
	if cfg.Notifications.InitialRetry <= 0 {
		cfg.Notifications.InitialRetry = defaultNotificationInitialRetry
	}
	if cfg.Notifications.MaxRetry <= 0 {
		cfg.Notifications.MaxRetry = defaultNotificationMaxRetry
	}
	if cfg.Notifications.MaxRetry < cfg.Notifications.InitialRetry {
		return nil, fmt.Errorf("notifications.max_retry must be greater than or equal to initial_retry")
	}
	if cfg.Notifications.MaxAttempts < 0 {
		return nil, fmt.Errorf("notifications.max_attempts must not be negative")
	}
	if cfg.Notifications.MaxAttempts == 0 {
		cfg.Notifications.MaxAttempts = defaultNotificationMaxAttempts
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
	cfg.Telegram.Tags = normalizeRoutingTags(cfg.Telegram.Tags)
	cfg.Bale.Tags = normalizeRoutingTags(cfg.Bale.Tags)
	if err := normalizeTelegramChannels(cfg.TelegramChannels); err != nil {
		return nil, err
	}
	if err := normalizeBaleChannels(cfg.BaleChannels); err != nil {
		return nil, err
	}
	for i := range cfg.Websites {
		site := &cfg.Websites[i]
		site.Tags = normalizeRoutingTags(site.Tags)
		site.Method = strings.ToUpper(strings.TrimSpace(site.Method))
		if site.Method == "" {
			site.Method = http.MethodGet
		}
		if !validHTTPMethod(site.Method) {
			return nil, fmt.Errorf("websites[%d].method %q is invalid", i, site.Method)
		}
		for header := range site.Headers {
			if strings.EqualFold(strings.TrimSpace(header), "Authorization") && site.BearerToken != "" {
				return nil, fmt.Errorf("websites[%d] cannot use bearer_token and an Authorization header together", i)
			}
		}
		site.ResponseType = strings.ToLower(strings.TrimSpace(site.ResponseType))
		if site.ResponseType == "" {
			if len(site.JSONAssertions) > 0 {
				site.ResponseType = "json"
			} else {
				site.ResponseType = "html"
			}
		}
		switch site.ResponseType {
		case "html", "json", "text", "any":
		default:
			return nil, fmt.Errorf("websites[%d].response_type must be html, json, text, or any", i)
		}
		statuses := append([]int(nil), site.ExpectedStatuses...)
		if site.ExpectedStatus != 0 {
			statuses = append(statuses, site.ExpectedStatus)
		}
		for _, status := range statuses {
			if status < 100 || status > 599 {
				return nil, fmt.Errorf("websites[%d] expected HTTP status %d is invalid", i, status)
			}
		}
		if site.Interval <= 0 {
			site.Interval = defaultCheckInterval
		}
		if site.Timeout <= 0 {
			site.Timeout = defaultCheckTimeout
		}
		if site.Attempts <= 0 {
			site.Attempts = defaultCheckAttempts
		}
		if site.RetryDelay < 0 {
			return nil, fmt.Errorf("websites[%d].retry_delay must not be negative", i)
		}
		if site.RetryDelay == 0 {
			site.RetryDelay = defaultRetryDelay
		}
		if site.FailureThreshold <= 0 {
			site.FailureThreshold = defaultFailureThreshold
		}
		if site.RecoveryThreshold <= 0 {
			site.RecoveryThreshold = defaultRecoveryThreshold
		}
		if site.SSLMinDays == 0 {
			site.SSLMinDays = 30
		}
	}
	seenTargetNames := make(map[string]struct{}, len(cfg.PrometheusTargets))
	for i := range cfg.PrometheusTargets {
		target := &cfg.PrometheusTargets[i]
		target.Tags = normalizeRoutingTags(target.Tags)
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

func validHTTPMethod(method string) bool {
	if method == "" {
		return false
	}
	for _, r := range method {
		if r <= 32 || r >= 127 || strings.ContainsRune("()<>@,;:\\\"/[]?={}\t", r) {
			return false
		}
	}
	return true
}

func normalizeTelegramChannels(channels []TelegramConfig) error {
	seen := make(map[string]struct{}, len(channels))
	for i := range channels {
		channel := &channels[i]
		channel.Name = strings.TrimSpace(channel.Name)
		if channel.Name == "" {
			channel.Name = fmt.Sprintf("telegram-%d", i+1)
		}
		if _, duplicate := seen[channel.Name]; duplicate {
			return fmt.Errorf("telegram channel name %q is duplicated", channel.Name)
		}
		seen[channel.Name] = struct{}{}
		if strings.TrimSpace(channel.BotToken) == "" || strings.TrimSpace(channel.ChatID) == "" {
			return fmt.Errorf("telegram channel %q requires bot_token and chat_id", channel.Name)
		}
		channel.Tags = normalizeRoutingTags(channel.Tags)
	}
	return nil
}

func normalizeBaleChannels(channels []BaleConfig) error {
	seen := make(map[string]struct{}, len(channels))
	for i := range channels {
		channel := &channels[i]
		channel.Name = strings.TrimSpace(channel.Name)
		if channel.Name == "" {
			channel.Name = fmt.Sprintf("bale-%d", i+1)
		}
		if _, duplicate := seen[channel.Name]; duplicate {
			return fmt.Errorf("bale channel name %q is duplicated", channel.Name)
		}
		seen[channel.Name] = struct{}{}
		if strings.TrimSpace(channel.BotToken) == "" || strings.TrimSpace(channel.ChatID) == "" {
			return fmt.Errorf("bale channel %q requires bot_token and chat_id", channel.Name)
		}
		channel.Tags = normalizeRoutingTags(channel.Tags)
	}
	return nil
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
