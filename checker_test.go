package main

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"
)

func TestCheckSiteRetriesTransientFailure(t *testing.T) {
	var requests atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		if requests.Add(1) == 1 {
			http.Error(w, "temporary", http.StatusServiceUnavailable)
			return
		}
		fmt.Fprint(w, "<!doctype html><html><body>ok</body></html>")
	}))
	defer server.Close()

	result := checkSite(Website{
		URL:        server.URL,
		Timeout:    time.Second,
		Attempts:   3,
		RetryDelay: 0,
	})

	if !result.Up {
		t.Fatalf("expected site to recover during retries, got %+v", result)
	}
	if result.Attempts != 2 {
		t.Fatalf("expected 2 attempts, got %d", result.Attempts)
	}
	if result.StatusCode != http.StatusOK {
		t.Fatalf("expected HTTP 200, got %d", result.StatusCode)
	}
	if result.Duration <= 0 {
		t.Fatal("expected request duration to be recorded")
	}
}

func TestCheckSiteClassifiesTimeout(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		time.Sleep(100 * time.Millisecond)
		fmt.Fprint(w, "<html></html>")
	}))
	defer server.Close()

	result := checkSite(Website{
		URL:      server.URL,
		Timeout:  10 * time.Millisecond,
		Attempts: 1,
	})

	if result.Up {
		t.Fatal("expected timed out check to fail")
	}
	if result.Kind != "timeout" {
		t.Fatalf("expected timeout classification, got %q (%s)", result.Kind, result.StatusMsg)
	}
	if result.Duration <= 0 {
		t.Fatal("expected timeout duration to be recorded")
	}
}
