# Gomon

A lightweight website monitoring tool written in Go. Monitors HTTP status, HTML content, and SSL certificate expiry — sending alerts via Telegram and/or webhook with daily summary reports.

## Features

- Monitors multiple websites on independent intervals
- Detects site downtime (HTTP errors, no response, non-HTML content)
- Checks SSL certificate expiry against a configurable minimum days threshold
- Sends a single combined alert when a site is down or SSL is expiring
- Sends a recovery notification when a site comes back online
- Daily heartbeat report with uptime % and SSL status per site
- Appends structured check results to a log file
- **Telegram notifications** with optional custom/proxied API URL
- **Generic webhook notifications** with custom headers for any HTTP endpoint

## Requirements

- Go 1.22+ (build from source only)
- At least one notifier configured (Telegram and/or webhook)

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
  bot_token: "YOUR_BOT_TOKEN"
  chat_id: "YOUR_CHAT_ID"
  api_url: "https://api.telegram.org"  # change for proxy

webhook:
  url: "https://your-server.com/hook"  # leave empty to disable
  headers:
    Authorization: "Bearer YOUR_TOKEN"

heartbeat:
  time: "08:00"       # 24-hour local time for daily report

log_file: "monitor.log"

websites:
  - url: "https://example.com"
    interval: 30s
    ssl: true
    ssl_min_days: 30

  - url: "example.com:8443"   # scheme auto-added based on ssl flag
    interval: 1m
    ssl: true
    ssl_min_days: 14

  - url: "http://internal.example.com"
    interval: 5m
    ssl: false
```

### Config fields

| Field | Default | Description |
|---|---|---|
| `telegram.bot_token` | — | Telegram bot token from @BotFather |
| `telegram.chat_id` | — | Target chat ID (use @userinfobot to find yours) |
| `telegram.api_url` | `https://api.telegram.org` | Telegram API base URL — set to a proxy for restricted networks |
| `telegram.socks_proxy` | — | SOCKS5 proxy for Telegram requests, e.g. `socks5://user:pass@host:1080` |
| `webhook.url` | — | HTTP endpoint to POST alerts to; leave empty to disable |
| `webhook.headers` | — | Key/value map of HTTP headers (e.g. `Authorization`) |
| `heartbeat.time` | `08:00` | Daily report time (HH:MM, 24-hour, local timezone) |
| `log_file` | `monitor.log` | Path to the append-only status log |
| `websites[].url` | — | Full URL or `host:port` (scheme inferred from `ssl`) |
| `websites[].interval` | `60s` | Check frequency — supports `s`, `m`, `h` suffixes |
| `websites[].ssl` | `false` | Whether to check SSL certificate expiry |
| `websites[].ssl_min_days` | `30` | Alert threshold: days until cert expiry |

Both `telegram` and `webhook` are optional — configure one or both. Alerts are delivered to all configured notifiers simultaneously.

## Usage

```bash
# Run with default config.yaml in current directory
./gomon

# Run with a custom config path
./gomon /etc/gomon/config.yaml
```

Stop with `Ctrl+C` or `SIGTERM`.

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
Error: connection refused

⚠️ SSL Certificate Expiring
Days remaining: 8 (min allowed: 30)
```

**Recovery:**
```
✅ Site Recovered
`example.com` is back online
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
- **Down alert** fires once on state change, not on every failed check.
- **SSL alert** fires once when days drop below the threshold and resets on restart.
- **Recovery** fires once when a previously down site becomes reachable.

## Log File Format

One line per check, pipe-delimited:

```
2026-06-21T10:00:00Z|UP|example.com|HTTP 200|SSL:42
2026-06-21T10:00:30Z|DOWN|api.example.com|connection refused
```

Fields: `timestamp | UP/DOWN | url | status message | SSL:<days> (optional)`

The daily heartbeat reads this file to build its report.

## Project Structure

```
gomon/
├── main.go          # Entry point
├── config.go        # YAML config loading
├── checker.go       # HTTP + SSL check logic
├── monitor.go       # Per-site monitoring loop and alert state
├── notifier.go      # Notifier interface — fans out to all configured backends
├── telegram.go      # Telegram Bot API client (supports custom/proxied API URL)
├── webhook.go       # Generic HTTP webhook notifier
├── logger.go        # Log file writer and reader
├── heartbeat.go     # Daily report scheduler and formatter
└── config.yaml      # Configuration file (gitignored — copy from config.example.yaml)
```
