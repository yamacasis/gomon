package main

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

func TestCheckSiteSupportsAuthenticatedJSONAPI(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			t.Errorf("expected POST, got %s", r.Method)
		}
		if r.Header.Get("Authorization") != "Bearer test-token" || r.Header.Get("X-Tenant") != "acme" {
			t.Errorf("unexpected request headers: %#v", r.Header)
		}
		var request map[string]interface{}
		if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
			t.Errorf("decode request: %v", err)
		}
		if request["ping"] != "pong" {
			t.Errorf("unexpected request body: %#v", request)
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusCreated)
		_, _ = fmt.Fprint(w, `{"status":"ok","data":{"items":[{"ready":true}]}}`)
	}))
	defer server.Close()

	result := checkSite(Website{
		URL:              server.URL,
		Method:           http.MethodPost,
		Headers:          map[string]string{"X-Tenant": "acme"},
		BearerToken:      "test-token",
		Body:             `{"ping":"pong"}`,
		ExpectedStatus:   http.StatusCreated,
		ResponseType:     "json",
		ResponseContains: []string{`"status":"ok"`},
		JSONAssertions: map[string]interface{}{
			"status":             "ok",
			"data.items.0.ready": true,
		},
		Timeout:  time.Second,
		Attempts: 1,
	})
	if !result.Up || result.StatusCode != http.StatusCreated {
		t.Fatalf("expected API check to pass, got %+v", result)
	}
}

func TestCheckSiteFailsJSONAssertion(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = fmt.Fprint(w, `{"status":"degraded"}`)
	}))
	defer server.Close()

	result := checkSite(Website{
		URL:            server.URL,
		ResponseType:   "json",
		JSONAssertions: map[string]interface{}{"status": "ok"},
		Timeout:        time.Second,
		Attempts:       1,
	})
	if result.Up || result.Kind != "content" || !strings.Contains(result.StatusMsg, `"status"`) {
		t.Fatalf("expected JSON assertion failure, got %+v", result)
	}
}

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
