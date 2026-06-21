# Gomon

A lightweight website monitoring tool written in Go. Monitors HTTP status, HTML content, and SSL certificate expiry — sending Telegram alerts and daily summary reports.

## Features

- Monitors multiple websites on independent intervals
- Detects site downtime (HTTP errors, no response, non-HTML content)
- Checks SSL certificate expiry against a configurable minimum days threshold
- Sends a single combined Telegram alert when a site is down or SSL is expiring
- Sends a recovery notification when a site comes back online
- Daily heartbeat report via Telegram with uptime % and SSL status per site
- Appends structured check results to a log file

## Requirements

- Go 1.22+
- A Telegram bot token and chat ID

## Installation

```bash
git clone <repo-url>
cd gomon
go mod tidy
go build -o gomon .
```

## Configuration

Copy and edit `config.yaml`:

```yaml
telegram:
  bot_token: "YOUR_BOT_TOKEN"
  chat_id: "YOUR_CHAT_ID"

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
| `heartbeat.time` | `08:00` | Daily report time (HH:MM, 24-hour, local timezone) |
| `log_file` | `monitor.log` | Path to the append-only status log |
| `websites[].url` | — | Full URL or `host:port` (scheme inferred from `ssl`) |
| `websites[].interval` | `60s` | Check frequency — supports `s`, `m`, `h` suffixes |
| `websites[].ssl` | `false` | Whether to check SSL certificate expiry |
| `websites[].ssl_min_days` | `30` | Alert threshold: days until cert expiry |

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
├── main.go        # Entry point
├── config.go      # YAML config loading
├── checker.go     # HTTP + SSL check logic
├── monitor.go     # Per-site monitoring loop and alert state
├── telegram.go    # Telegram Bot API client
├── logger.go      # Log file writer and reader
├── heartbeat.go   # Daily report scheduler and formatter
└── config.yaml    # Configuration file
```
