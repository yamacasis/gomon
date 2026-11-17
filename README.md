# Gomon

A lightweight website and API monitoring tool written in Go. Monitors HTTP responses, HTML or JSON content, and SSL certificate expiry — sending reliable alerts via Telegram, Bale, and/or webhook with daily summary reports.

## Features

- Monitors multiple websites on independent intervals
- Checks APIs with custom HTTP methods, headers, bearer tokens, request bodies, expected status codes, and JSON assertions
- Detects site downtime (HTTP errors, no response, non-HTML content)
- Retries transient failures and confirms state changes across consecutive checks
- Classifies failures as DNS, timeout, TLS, connection, HTTP, or content errors
- Optional Cloudflare-aware policy ignores no-response network failures while still alerting on HTTP errors
- Checks SSL certificate expiry against a configurable minimum days threshold
- Sends a single combined alert when a site is down or SSL is expiring
- Sends a recovery notification when a site comes back online
- Daily heartbeat report with uptime % and SSL status per site
- Appends structured check results to a log file
- Exposes Gomon, website, and exporter health at `/metrics/me` for Prometheus
- Scrapes remote `node_exporter` endpoints and alerts on CPU, RAM, disk, I/O, load, temperature, collector, RAID, and SMART health
- Adds searchable `#gomon` and event-specific tags to every notification
- Routes tagged website/exporter alerts to any number of tagged Telegram and Bale channels
- Persists notifications before delivery and retries failed channels with exponential backoff
- Records the final delivery result for every target channel in a JSONL audit log
- **Telegram notifications** with optional custom/proxied API URL
- **Bale notifications** through its Telegram-compatible Bot API
- **Generic webhook notifications** with custom headers for any HTTP endpoint

## Requirements

- Go 1.25+ (build from source only)
- At least one notifier configured (Telegram, Bale, and/or webhook)

## Installation

### Linux — one-line install (recommended)

Downloads the latest release binary, creates a `gomon` system user, installs the systemd service, and drops an example config at `/etc/gomon/config.yaml`:

```bash
curl -fsSL https://raw.githubusercontent.com/yamacasis/gomon/main/install.sh | sudo bash
```

Or download and inspect first:

```bash
curl -fsSL https://raw.githubusercontent.com/yamacasis/gomon/main/install.sh -o install.sh
# review install.sh ...
sudo bash install.sh
```

**After install**, edit the config and start the service:

```bash
sudo nano /etc/gomon/config.yaml   # set bot_token, chat_id, websites
sudo systemctl start gomon
sudo journalctl -u gomon -f        # watch live logs
```

**Uninstall:**

```bash
sudo bash install.sh --uninstall
```

---

### Build from source

```bash
git clone https://github.com/yamacasis/gomon
cd gomon
go mod tidy
go build -o gomon .
```

## Configuration

Copy `config.example.yaml` to `config.yaml` and edit:

```yaml
telegram:
  name: "default-telegram"
  tags: ["general"]
  bot_token: "YOUR_BOT_TOKEN"
  chat_id: "YOUR_CHAT_ID"
  api_url: "https://api.telegram.org"  # change for proxy

bale:
  name: "default-bale"
  tags: ["general"]
  bot_token: "YOUR_BALE_BOT_TOKEN"
  chat_id: "YOUR_BALE_CHANNEL_OR_CHAT_ID"
  api_url: "https://tapi.bale.ai"

telegram_channels:
  - name: "infrastructure-team"
    tags: ["infra", "critical"]
    bot_token: "ANOTHER_TELEGRAM_BOT_TOKEN"
    chat_id: "ANOTHER_TELEGRAM_CHAT_ID"

bale_channels:
  - name: "website-team"
    tags: ["web"]
    bot_token: "ANOTHER_BALE_BOT_TOKEN"
    chat_id: "ANOTHER_BALE_CHAT_ID"

webhook:
  url: "https://your-server.com/hook"  # leave empty to disable
  headers:
    Authorization: "Bearer YOUR_TOKEN"

notifications:
  queue_file: "notification-queue.json"
  delivery_log: "notification-deliveries.log"
  timeout: 15s
  initial_retry: 5s
  max_retry: 5m
  max_attempts: 10

heartbeat:
  time: "08:00"       # 24-hour local time for daily report

metrics:
  port: 9091
  listen_address: "0.0.0.0"

log_file: "monitor.log"

websites:
  - url: "https://example.com"
    tags: ["general", "web"]
    cloudflare: true
    interval: 30s
    timeout: 15s
    attempts: 3
    retry_delay: 1s
    failure_threshold: 2
    recovery_threshold: 2
    ssl: true
    ssl_min_days: 30

  - url: "example.com:8443"   # scheme auto-added based on ssl flag
    interval: 1m
    ssl: true
    ssl_min_days: 14

  - url: "https://api.example.com/v1/health"
    method: POST
    headers:
      X-Tenant: "production"
    bearer_token: "YOUR_API_TOKEN"
    body: '{"probe":true}'
    expected_status: 200
    response_type: json
    response_contains: ["healthy"]
    response_not_contains: ["maintenance"]
    json_assertions:
      status: "ok"
      data.ready: true
      data.regions.0.online: true
    interval: 1m

  - url: "http://internal.example.com"
    interval: 5m
    ssl: false

prometheus_targets:
  - name: "server-01"
    tags: ["general", "infra"]
    url: "http://10.0.0.10:9100/metrics"
    type: "node_exporter"
    interval: 1m
    timeout: 10s
    failure_threshold: 2
    recovery_threshold: 2
    basic_auth:
      username: "admin"
      password: "CHANGE_ME"
    thresholds:
      cpu_percent: 90
      ram_percent: 90
      disk_percent: 90
      disk_io_percent: 95
      load_per_cpu: 2
      temperature_celsius: 85
```

### Config fields

| Field | Default | Description |
|---|---|---|
| `telegram.bot_token` | — | Telegram bot token from @BotFather |
| `telegram.chat_id` | — | Target chat ID (use @userinfobot to find yours) |
| `telegram.name` | — | Human-readable output name |
| `telegram.tags` | `[general]` | Routing tags accepted by the legacy/default Telegram output |
| `telegram.api_url` | `https://api.telegram.org` | Telegram API base URL — set to a proxy for restricted networks |
| `telegram.socks_proxy` | — | SOCKS5 proxy for Telegram requests, e.g. `socks5://user:pass@host:1080` |
| `telegram_channels[]` | — | Additional Telegram outputs, each with `name`, `tags`, `bot_token`, `chat_id`, and optional API/proxy settings |
| `bale.bot_token` | — | Bale bot token; leave empty to disable Bale notifications |
| `bale.chat_id` | — | Bale user, group, or channel identifier receiving notifications |
| `bale.name` | — | Human-readable output name |
| `bale.tags` | `[general]` | Routing tags accepted by the legacy/default Bale output |
| `bale.api_url` | `https://tapi.bale.ai` | Bale Telegram-compatible Bot API base URL |
| `bale_channels[]` | — | Additional Bale outputs, each with `name`, `tags`, `bot_token`, `chat_id`, and optional `api_url` |
| `webhook.url` | — | HTTP endpoint to POST alerts to; leave empty to disable |
| `webhook.headers` | — | Key/value map of HTTP headers (e.g. `Authorization`) |
| `notifications.queue_file` | beside `log_file` | Persistent pending-notification queue; messages are written here before delivery |
| `notifications.delivery_log` | beside `log_file` | JSONL audit log containing each channel's final `delivered` or `failed` result |
| `notifications.timeout` | `15s` | Maximum time allowed for one channel delivery attempt |
| `notifications.initial_retry` | `5s` | Delay after the first failed delivery; later delays grow exponentially |
| `notifications.max_retry` | `5m` | Maximum delay between delivery attempts |
| `notifications.max_attempts` | `10` | Attempts per channel before recording a permanent failure |
| `heartbeat.time` | `08:00` | Daily report time (HH:MM, 24-hour, local timezone) |
| `metrics.port` | `0` | Port for Gomon's Prometheus endpoint; zero disables it |
| `metrics.listen_address` | `0.0.0.0` | Address used when the metrics endpoint is enabled |
| `log_file` | `monitor.log` | Path to the append-only status log |
| `websites[].url` | — | Full URL or `host:port` (scheme inferred from `ssl`) |
| `websites[].method` | `GET` | HTTP request method such as `GET`, `POST`, or `HEAD` |
| `websites[].headers` | — | Request headers; use these for custom authentication schemes |
| `websites[].bearer_token` | — | Convenience field that sends an `Authorization: Bearer ...` header |
| `websites[].body` | — | Request body; JSON bodies automatically receive `application/json` when no content type is configured |
| `websites[].expected_status` | — | One exact acceptable HTTP status code |
| `websites[].expected_statuses` | — | Multiple acceptable HTTP status codes; without either field, all 2xx and 3xx responses are accepted |
| `websites[].response_type` | `html` | Response parser: `html`, `json`, `text`, or `any`; JSON assertions imply `json` when omitted |
| `websites[].response_contains` | — | Strings that must all occur in the response body |
| `websites[].response_not_contains` | — | Strings that must not occur in the response body |
| `websites[].json_assertions` | — | Map of dot-separated JSON paths to expected values; numeric path parts address array indexes |
| `websites[].tags` | `[general]` | Tags used to route this site's notifications |
| `websites[].cloudflare` | `false` | Ignore no-response network failures for Cloudflare-fronted sites; HTTP/content errors still affect availability |
| `websites[].interval` | `60s` | Check frequency — supports `s`, `m`, `h` suffixes |
| `websites[].timeout` | `15s` | Timeout for each HTTP attempt |
| `websites[].attempts` | `3` | Request attempts inside one check before that check fails |
| `websites[].retry_delay` | `1s` | Delay between request attempts |
| `websites[].failure_threshold` | `2` | Consecutive failed checks required before a Down alert |
| `websites[].recovery_threshold` | `2` | Consecutive successful checks required before recovery |
| `websites[].ssl` | `false` | Whether to check SSL certificate expiry |
| `websites[].ssl_min_days` | `30` | Alert threshold: days until cert expiry |
| `prometheus_targets[].name` | — | Unique label used for the monitored server |
| `prometheus_targets[].tags` | `[general]` | Tags used to route this exporter's notifications |
| `prometheus_targets[].url` | — | Prometheus exposition URL; `/metrics` is added when the URL has no path |
| `prometheus_targets[].type` | `node_exporter` | Exporter-specific analyzer; currently `node_exporter` |
| `prometheus_targets[].basic_auth` | — | Optional HTTP Basic Auth credentials (`username` and `password`) |
| `prometheus_targets[].headers` | — | Optional request headers for authentication |
| `prometheus_targets[].interval` | `60s` | Scrape frequency |
| `prometheus_targets[].timeout` | `10s` | Timeout for one exporter scrape |
| `prometheus_targets[].failure_threshold` | `2` | Consecutive bad scrapes required before alerting |
| `prometheus_targets[].recovery_threshold` | `2` | Consecutive healthy scrapes required before recovery |

Node exporter threshold defaults are CPU `90%`, RAM `90%`, filesystem `90%`, disk I/O busy `95%`, load per CPU `2`, and temperature `85°C`. Set an individual threshold to a negative number to disable that check.

Telegram, Bale, and webhook are optional — configure any combination. Alerts are delivered to all configured notifiers simultaneously.

Every alert is first committed to `notifications.queue_file`. Delivery is tracked independently for each matching channel, so one unavailable output does not cause successful outputs to receive the alert again. Pending deliveries are restored after a restart. Final per-channel results are appended to `notifications.delivery_log` without storing the message body there.

Every outgoing monitoring message ends with `#gomon` plus contextual tags. Examples include `#webcheck`, `#down`, `#timeout`, `#ssl`, `#prometheus`, `#nodeexporter`, `#cpu`, `#disk`, `#recovery`, and a normalized site or target name.

### Tag-based notification routing

Each website and Prometheus target has routing `tags`; each Telegram or Bale output has its own `tags`. A notification is sent to an output when at least one normalized tag matches. Missing or empty tags default to `general`, preserving the behavior of existing configurations. Routing tags are also included as hashtags in the delivered message.

Keep `general` on an entity to continue sending it to the current default channels. Remove `general` and assign a dedicated tag when the entity should only notify specialized channels.

## Usage

```bash
# Run with default config.yaml in current directory
./gomon

# Run with a custom config path
./gomon /etc/gomon/config.yaml
```

Stop with `Ctrl+C` or `SIGTERM`.

## Prometheus endpoint

When `metrics.port` is non-zero, Gomon serves its metrics at:

```text
http://HOST:PORT/metrics/me
```

Important exported series include:

- `gomon_up` and Go/process runtime metrics
- `gomon_site_up`, `gomon_site_alerting`, `gomon_site_check_suppressed`, check duration, attempts, HTTP status, and SSL days
- `gomon_prometheus_target_up` and active alert count
- `gomon_node_cpu_usage_percent`, memory, load, filesystem usage, disk I/O, temperature, and collector health

Example Prometheus scrape configuration:

```yaml
scrape_configs:
  - job_name: gomon
    metrics_path: /metrics/me
    static_configs:
      - targets: ["gomon-host:9091"]
```

## Node exporter monitoring

Gomon calculates CPU and disk I/O usage from consecutive counter samples, so those two values become available after the second successful scrape. Resource alerts and recoveries use the same consecutive-sample debouncing as website monitoring.

Filesystem read-only/device errors, failed node_exporter collectors, degraded software RAID, and hardware temperatures are checked when their metrics are present. SMART health is checked when the endpoint also exposes `smartmon_device_smart_healthy` or `smartctl_device_smart_status`; standard node_exporter installations do not expose complete SMART data by themselves.

## Telegram Setup

1. Message `@BotFather` on Telegram and run `/newbot` to create a bot — copy the token.
2. Message `@userinfobot` to get your personal chat ID, or add the bot to a group and use the group's ID (prefixed with `-100`).
3. Paste both values into `config.yaml`.

### Using a Telegram proxy

If Telegram is blocked on your network you have two options — use either or both together:

**SOCKS5 proxy** — routes all Telegram HTTP requests through the proxy:

```yaml
telegram:
  bot_token: "YOUR_TOKEN"
  chat_id: "YOUR_CHAT_ID"
  socks_proxy: "socks5://host:1080"           # no auth
  # socks_proxy: "socks5://user:pass@host:1080"  # with auth
```

**Custom API URL** — point at a self-hosted [Telegram Bot API server](https://github.com/tdlib/telegram-bot-api) or HTTP reverse proxy:

```yaml
telegram:
  bot_token: "YOUR_TOKEN"
  chat_id: "YOUR_CHAT_ID"
  api_url: "https://tg-proxy.example.com"
```

Both options can be used simultaneously — the SOCKS5 proxy tunnels the connection, while `api_url` changes the endpoint.

## Webhook Notifications

Gomon sends a `POST` request with a JSON body to `webhook.url` on every alert:

```json
{
  "text": "🚨 Alert: example.com\n\n🔴 Site is DOWN\nError: connection refused",
  "timestamp": "2026-06-21T10:00:00Z"
}
```

Use `webhook.headers` to pass authentication tokens:

```yaml
webhook:
  url: "https://your-server.com/notify"
  headers:
    Authorization: "Bearer secret123"
    X-Source: "gomon"
```

This makes it compatible with Slack incoming webhooks, Discord webhooks, ntfy.sh, custom endpoints, or any service that accepts a JSON POST.

## Alerts

**Site down or SSL expiring — single combined message:**
```
🚨 Alert: example.com

🔴 Site is DOWN
Cause: TIMEOUT
Error: request timed out
Confirmed: 2 consecutive failed checks; 3 request attempt(s) in the last check
Duration: 15s
Checked: 2026-06-21T10:00:00Z

⚠️ SSL Certificate Expiring
Days remaining: 8 (min allowed: 30)
```

**Recovery:**
```
✅ Site Recovered
`example.com` is stable after 2 consecutive successful checks
Outage duration: 2m1s
Current status: HTTP 200
Previous cause: TIMEOUT — request timed out
Checked: 2026-06-21T10:02:01Z
```

**Daily heartbeat (sent at configured time):**
```
📊 Daily Report — 2026-06-20

✅ `example.com`
   Uptime: 100.0% (2880/2880 checks)
   SSL: 42 days remaining

⚠️ `api.example.com:8443`
   Uptime: 98.6% (2839/2880 checks)
   SSL: 9 days remaining
```

Alert logic:
- A check retries transient failures up to `attempts` times.
- **Down alert** fires only after `failure_threshold` consecutive failed checks.
- **Recovery** fires only after `recovery_threshold` consecutive successful checks.
- **SSL alert** fires once when days drop below the threshold and resets on restart.

## Log File Format

One line per check, pipe-delimited:

```
2026-06-21T10:00:00Z|UP|example.com|HTTP 200|SSL:42|DURATION_MS:184|ATTEMPTS:1|REMOTE:203.0.113.10:443
2026-06-21T10:00:30Z|DOWN|api.example.com|connection refused|KIND:connection|DURATION_MS:15002|ATTEMPTS:3
```

Fields: `timestamp | UP/DOWN | url | status message` followed by optional diagnostic fields: `SSL`, `KIND`, `DURATION_MS`, `ATTEMPTS`, and `REMOTE`.

The daily heartbeat reads this file to build its report.

## Project Structure

```
gomon/
├── main.go          # Entry point
├── config.go        # YAML config loading
├── checker.go       # HTTP + SSL check logic
├── monitor.go       # Per-site monitoring loop and alert state
├── metrics.go       # Gomon Prometheus registry and /metrics/me server
├── exporter.go      # Remote Prometheus/node_exporter scraper and analyzer
├── notifier.go      # Notifier interface — fans out to all configured backends
├── notification_queue.go # Persistent per-channel delivery queue and retry worker
├── telegram.go      # Telegram Bot API client (supports custom/proxied API URL)
├── webhook.go       # Generic HTTP webhook notifier
├── logger.go        # Log file writer and reader
├── heartbeat.go     # Daily report scheduler and formatter
└── config.yaml      # Configuration file (gitignored — copy from config.example.yaml)
```
