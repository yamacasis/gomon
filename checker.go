package main

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httptrace"
	"strings"
	"time"
)

type CheckResult struct {
	URL        string
	Up         bool
	SSLDays    int // -1 if SSL not checked
	StatusMsg  string
	CheckedAt  time.Time
	Kind       string
	StatusCode int
	Duration   time.Duration
	Attempts   int
	RemoteAddr string
}

func checkSite(site Website) CheckResult {
	started := time.Now()
	attempts := site.Attempts
	if attempts <= 0 {
		attempts = defaultCheckAttempts
	}
	retryDelay := site.RetryDelay
	if retryDelay < 0 {
		retryDelay = 0
	}

	var result CheckResult
	for attempt := 1; attempt <= attempts; attempt++ {
		result = checkSiteOnce(site)
		result.Attempts = attempt
		if result.Up {
			result.Duration = time.Since(started)
			return result
		}
		if attempt < attempts && retryDelay > 0 {
			time.Sleep(retryDelay)
		}
	}
	result.Duration = time.Since(started)
	return result
}

func checkSiteOnce(site Website) (result CheckResult) {
	result = CheckResult{
		URL:       site.URL,
		SSLDays:   -1,
		CheckedAt: time.Now().UTC(),
	}
	started := time.Now()
	defer func() {
		result.Duration = time.Since(started)
	}()

	url := normalizeURL(site.URL, site.SSL)
	timeout := site.Timeout
	if timeout <= 0 {
		timeout = defaultCheckTimeout
	}

	transport := &http.Transport{
		TLSClientConfig:   &tls.Config{InsecureSkipVerify: false},
		DisableKeepAlives: true,
	}
	client := &http.Client{
		Timeout:   timeout,
		Transport: transport,
	}
	defer transport.CloseIdleConnections()

	req, err := http.NewRequest(http.MethodGet, url, nil)
	if err != nil {
		result.Kind = "configuration"
		result.StatusMsg = shortError(err)
		return result
	}
	req.Header.Set("User-Agent", "gomon")
	trace := &httptrace.ClientTrace{
		GotConn: func(info httptrace.GotConnInfo) {
			result.RemoteAddr = info.Conn.RemoteAddr().String()
		},
	}
	req = req.WithContext(httptrace.WithClientTrace(req.Context(), trace))

	resp, err := client.Do(req)
	if err != nil {
		result.Up = false
		result.Kind = classifyRequestError(err)
		result.StatusMsg = shortError(err)
		return result
	}
	defer resp.Body.Close()
	result.StatusCode = resp.StatusCode

	if resp.StatusCode < 200 || resp.StatusCode >= 400 {
		result.Up = false
		result.Kind = "http"
		result.StatusMsg = fmt.Sprintf("HTTP %d", resp.StatusCode)
		return result
	}

	body, err := io.ReadAll(io.LimitReader(resp.Body, 8192))
	if err != nil {
		result.Up = false
		result.Kind = classifyRequestError(err)
		if result.Kind == "request" {
			result.Kind = "body"
		}
		result.StatusMsg = "reading response body: " + shortError(err)
		return result
	}
	if !containsHTML(string(body)) {
		result.Up = false
		result.Kind = "content"
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

func classifyRequestError(err error) string {
	if errors.Is(err, context.DeadlineExceeded) {
		return "timeout"
	}
	var dnsErr *net.DNSError
	if errors.As(err, &dnsErr) {
		return "dns"
	}
	var netErr net.Error
	if errors.As(err, &netErr) && netErr.Timeout() {
		return "timeout"
	}
	var unknownAuthority x509.UnknownAuthorityError
	var hostnameError x509.HostnameError
	var certificateInvalid x509.CertificateInvalidError
	if errors.As(err, &unknownAuthority) || errors.As(err, &hostnameError) || errors.As(err, &certificateInvalid) {
		return "tls"
	}

	message := strings.ToLower(err.Error())
	switch {
	case strings.Contains(message, "tls"), strings.Contains(message, "certificate"):
		return "tls"
	case strings.Contains(message, "connection refused"),
		strings.Contains(message, "connection reset"),
		strings.Contains(message, "no route to host"):
		return "connection"
	default:
		return "request"
	}
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
