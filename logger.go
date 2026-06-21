package main

import (
	"bufio"
	"fmt"
	"os"
	"strings"
	"sync"
	"time"
)

// Log line format: timestamp|UP|url|statusMsg
// With SSL:       timestamp|UP|url|statusMsg|SSL:45
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
	if !result.Up {
		status = "DOWN"
	}

	line := fmt.Sprintf("%s|%s|%s|%s",
		result.CheckedAt.Format(time.RFC3339),
		status,
		result.URL,
		result.StatusMsg,
	)
	if result.SSLDays >= 0 {
		line += fmt.Sprintf("|SSL:%d", result.SSLDays)
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
	Timestamp time.Time
	Status    string
	URL       string
	Msg       string
	SSLDays   int // -1 if not present
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
		parts := strings.SplitN(line, logDelimiter, 5)
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
		if len(parts) == 5 && strings.HasPrefix(parts[4], "SSL:") {
			fmt.Sscanf(parts[4], "SSL:%d", &e.SSLDays)
		}
		entries = append(entries, e)
	}
	return entries, scanner.Err()
}
