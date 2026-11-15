package main

import (
	"bufio"
	"fmt"
	"os"
	"strings"
	"sync"
	"time"
)

// Log line format: timestamp|UP|url|statusMsg|SSL:45|KIND:http|DURATION_MS:123|ATTEMPTS:2|REMOTE:1.2.3.4:443
// Status can be UP, DOWN, or IGNORED. IGNORED retains diagnostics without affecting uptime.
const logDelimiter = "|"

type Logger struct {
	mu      sync.Mutex
	file    *os.File
	writer  *bufio.Writer
	logPath string
}

func newLogger(path string) (*Logger, error) {
	f, err := os.OpenFile(path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0644)
	if err != nil {
		return nil, err
	}
	return &Logger{
		file:    f,
		writer:  bufio.NewWriter(f),
		logPath: path,
	}, nil
}

func (l *Logger) write(result CheckResult) {
	l.mu.Lock()
	defer l.mu.Unlock()

	status := "UP"
	if result.Suppressed {
		status = "IGNORED"
	} else if !result.Up {
		status = "DOWN"
	}

	line := fmt.Sprintf("%s|%s|%s|%s",
		result.CheckedAt.Format(time.RFC3339),
		status,
		sanitizeLogField(result.URL),
		sanitizeLogField(result.StatusMsg),
	)
	if result.SSLDays >= 0 {
		line += fmt.Sprintf("|SSL:%d", result.SSLDays)
	}
	if result.Kind != "" {
		line += "|KIND:" + sanitizeLogField(result.Kind)
	}
	line += fmt.Sprintf("|DURATION_MS:%d|ATTEMPTS:%d", result.Duration.Milliseconds(), result.Attempts)
	if result.RemoteAddr != "" {
		line += "|REMOTE:" + sanitizeLogField(result.RemoteAddr)
	}
	line += "\n"

	l.writer.WriteString(line)
	l.writer.Flush()
}

func (l *Logger) close() {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.writer.Flush()
	l.file.Close()
}

// LogEntry holds a parsed log line.
type LogEntry struct {
	Timestamp  time.Time
	Status     string
	URL        string
	Msg        string
	SSLDays    int // -1 if not present
	Kind       string
	Duration   time.Duration
	Attempts   int
	RemoteAddr string
}

func (l *Logger) readDate(date string) ([]LogEntry, error) {
	l.mu.Lock()
	path := l.logPath
	l.mu.Unlock()

	f, err := os.Open(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}
	defer f.Close()

	var entries []LogEntry
	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		line := scanner.Text()
		if !strings.HasPrefix(line, date) {
			continue
		}
		parts := strings.Split(line, logDelimiter)
		if len(parts) < 4 {
			continue
		}
		ts, err := time.Parse(time.RFC3339, parts[0])
		if err != nil {
			continue
		}
		e := LogEntry{
			Timestamp: ts,
			Status:    parts[1],
			URL:       parts[2],
			Msg:       parts[3],
			SSLDays:   -1,
		}
		for _, field := range parts[4:] {
			switch {
			case strings.HasPrefix(field, "SSL:"):
				fmt.Sscanf(field, "SSL:%d", &e.SSLDays)
			case strings.HasPrefix(field, "KIND:"):
				e.Kind = strings.TrimPrefix(field, "KIND:")
			case strings.HasPrefix(field, "DURATION_MS:"):
				var milliseconds int64
				fmt.Sscanf(field, "DURATION_MS:%d", &milliseconds)
				e.Duration = time.Duration(milliseconds) * time.Millisecond
			case strings.HasPrefix(field, "ATTEMPTS:"):
				fmt.Sscanf(field, "ATTEMPTS:%d", &e.Attempts)
			case strings.HasPrefix(field, "REMOTE:"):
				e.RemoteAddr = strings.TrimPrefix(field, "REMOTE:")
			}
		}
		entries = append(entries, e)
	}
	return entries, scanner.Err()
}

func sanitizeLogField(value string) string {
	value = strings.ReplaceAll(value, "|", "/")
	value = strings.ReplaceAll(value, "\n", " ")
	return strings.ReplaceAll(value, "\r", " ")
}
