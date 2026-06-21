package main

import (
	"crypto/tls"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

type CheckResult struct {
	URL       string
	Up        bool
	SSLDays   int // -1 if SSL not checked
	StatusMsg string
	CheckedAt time.Time
}

func checkSite(site Website) CheckResult {
	result := CheckResult{
		URL:       site.URL,
		SSLDays:   -1,
		CheckedAt: time.Now().UTC(),
	}

	url := normalizeURL(site.URL, site.SSL)

	client := &http.Client{
		Timeout: 15 * time.Second,
		Transport: &http.Transport{
			TLSClientConfig:   &tls.Config{InsecureSkipVerify: false},
			DisableKeepAlives: true,
		},
	}

	resp, err := client.Get(url)
	if err != nil {
		result.Up = false
		result.StatusMsg = shortError(err)
		return result
	}
	defer resp.Body.Close()

	if resp.StatusCode < 200 || resp.StatusCode >= 400 {
		result.Up = false
		result.StatusMsg = fmt.Sprintf("HTTP %d", resp.StatusCode)
		return result
	}

	body, _ := io.ReadAll(io.LimitReader(resp.Body, 8192))
	if !containsHTML(string(body)) {
		result.Up = false
		result.StatusMsg = "no HTML content in response"
		return result
	}

	result.Up = true
	result.StatusMsg = fmt.Sprintf("HTTP %d", resp.StatusCode)

	if site.SSL && resp.TLS != nil && len(resp.TLS.PeerCertificates) > 0 {
		cert := resp.TLS.PeerCertificates[0]
		result.SSLDays = int(time.Until(cert.NotAfter).Hours() / 24)
	}

	return result
}

func normalizeURL(raw string, ssl bool) string {
	if strings.HasPrefix(raw, "http://") || strings.HasPrefix(raw, "https://") {
		return raw
	}
	if ssl {
		return "https://" + raw
	}
	return "http://" + raw
}

func containsHTML(body string) bool {
	lower := strings.ToLower(body)
	return strings.Contains(lower, "<html") ||
		strings.Contains(lower, "<body") ||
		strings.Contains(lower, "<head") ||
		strings.Contains(lower, "<!doctype")
}

func shortError(err error) string {
	s := err.Error()
	// Keep the message readable but not excessively long
	if len(s) > 120 {
		return s[:120] + "..."
	}
	return s
}
