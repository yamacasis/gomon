package main

import (
	"bytes"
	"context"
	"crypto/tls"
	"crypto/x509"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httptrace"
	"reflect"
	"strconv"
	"strings"
	"time"
)

type CheckResult struct {
	URL        string
	Up         bool
	Suppressed bool // excluded from availability state by a configured policy
	SSLDays    int  // -1 if SSL not checked
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

	method := strings.ToUpper(strings.TrimSpace(site.Method))
	if method == "" {
		method = http.MethodGet
	}
	req, err := http.NewRequest(method, url, strings.NewReader(site.Body))
	if err != nil {
		result.Kind = "configuration"
		result.StatusMsg = shortError(err)
		return result
	}
	req.Header.Set("User-Agent", "gomon")
	for key, value := range site.Headers {
		req.Header.Set(key, value)
	}
	if site.BearerToken != "" {
		req.Header.Set("Authorization", "Bearer "+site.BearerToken)
	}
	if site.Body != "" && req.Header.Get("Content-Type") == "" {
		if json.Valid([]byte(site.Body)) {
			req.Header.Set("Content-Type", "application/json")
		} else {
			req.Header.Set("Content-Type", "text/plain; charset=utf-8")
		}
	}
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
	if site.SSL && resp.TLS != nil && len(resp.TLS.PeerCertificates) > 0 {
		cert := resp.TLS.PeerCertificates[0]
		result.SSLDays = int(time.Until(cert.NotAfter).Hours() / 24)
	}

	if !statusAccepted(site, resp.StatusCode) {
		result.Up = false
		result.Kind = "http"
		result.StatusMsg = fmt.Sprintf("HTTP %d (unexpected status)", resp.StatusCode)
		return result
	}

	body, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if err != nil {
		result.Up = false
		result.Kind = classifyRequestError(err)
		if result.Kind == "request" {
			result.Kind = "body"
		}
		result.StatusMsg = "reading response body: " + shortError(err)
		return result
	}
	responseType := strings.ToLower(strings.TrimSpace(site.ResponseType))
	if responseType == "" {
		if len(site.JSONAssertions) > 0 {
			responseType = "json"
		} else {
			responseType = "html"
		}
	}
	var decodedJSON interface{}
	switch responseType {
	case "html":
		if !containsHTML(string(body)) {
			result.Up = false
			result.Kind = "content"
			result.StatusMsg = "no HTML content in response"
			return result
		}
	case "json":
		if !json.Valid(body) {
			result.Up = false
			result.Kind = "content"
			result.StatusMsg = "invalid JSON response"
			return result
		}
		decoder := json.NewDecoder(bytes.NewReader(body))
		decoder.UseNumber()
		if err := decoder.Decode(&decodedJSON); err != nil {
			result.Up = false
			result.Kind = "content"
			result.StatusMsg = "invalid JSON response: " + shortError(err)
			return result
		}
	}
	for _, expected := range site.ResponseContains {
		if !bytes.Contains(body, []byte(expected)) {
			result.Up = false
			result.Kind = "content"
			result.StatusMsg = fmt.Sprintf("response does not contain %q", expected)
			return result
		}
	}
	for _, forbidden := range site.ResponseNotContains {
		if bytes.Contains(body, []byte(forbidden)) {
			result.Up = false
			result.Kind = "content"
			result.StatusMsg = fmt.Sprintf("response contains forbidden value %q", forbidden)
			return result
		}
	}
	if len(site.JSONAssertions) > 0 {
		if decodedJSON == nil {
			decoder := json.NewDecoder(bytes.NewReader(body))
			decoder.UseNumber()
			if err := decoder.Decode(&decodedJSON); err != nil {
				result.Up = false
				result.Kind = "content"
				result.StatusMsg = "invalid JSON response: " + shortError(err)
				return result
			}
		}
		for path, expected := range site.JSONAssertions {
			actual, ok := lookupJSONPath(decodedJSON, path)
			if !ok {
				result.Up = false
				result.Kind = "content"
				result.StatusMsg = fmt.Sprintf("JSON path %q was not found", path)
				return result
			}
			if !jsonValuesEqual(actual, expected) {
				result.Up = false
				result.Kind = "content"
				result.StatusMsg = fmt.Sprintf("JSON path %q did not match expected value", path)
				return result
			}
		}
	}

	result.Up = true
	result.StatusMsg = fmt.Sprintf("HTTP %d", resp.StatusCode)

	return result
}

func statusAccepted(site Website, status int) bool {
	if site.ExpectedStatus != 0 && status == site.ExpectedStatus {
		return true
	}
	for _, expected := range site.ExpectedStatuses {
		if status == expected {
			return true
		}
	}
	if site.ExpectedStatus != 0 || len(site.ExpectedStatuses) > 0 {
		return false
	}
	return status >= 200 && status < 400
}

func lookupJSONPath(value interface{}, path string) (interface{}, bool) {
	if strings.TrimSpace(path) == "" {
		return value, true
	}
	current := value
	for _, part := range strings.Split(path, ".") {
		switch typed := current.(type) {
		case map[string]interface{}:
			var ok bool
			current, ok = typed[part]
			if !ok {
				return nil, false
			}
		case []interface{}:
			index, err := strconv.Atoi(part)
			if err != nil || index < 0 || index >= len(typed) {
				return nil, false
			}
			current = typed[index]
		default:
			return nil, false
		}
	}
	return current, true
}

func jsonValuesEqual(actual, expected interface{}) bool {
	normalize := func(value interface{}) interface{} {
		encoded, err := json.Marshal(value)
		if err != nil {
			return value
		}
		var decoded interface{}
		decoder := json.NewDecoder(bytes.NewReader(encoded))
		decoder.UseNumber()
		if err := decoder.Decode(&decoded); err != nil {
			return value
		}
		return decoded
	}
	return reflect.DeepEqual(normalize(actual), normalize(expected))
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
