package main

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestBaleSendUsesTelegramCompatibleAPI(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost || r.URL.Path != "/bottest-token/sendMessage" {
			t.Errorf("unexpected request: %s %s", r.Method, r.URL.Path)
		}
		var payload map[string]string
		if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
			t.Errorf("decode payload: %v", err)
		}
		if payload["chat_id"] != "channel-id" || payload["text"] != "hello" || payload["parse_mode"] != "Markdown" {
			t.Errorf("unexpected payload: %#v", payload)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"ok":true,"result":{}}`))
	}))
	defer server.Close()

	bale := newBale(BaleConfig{BotToken: "test-token", ChatID: "channel-id", APIURL: server.URL})
	if err := bale.send("hello"); err != nil {
		t.Fatal(err)
	}
}

func TestBaleSendReturnsAPIError(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"ok":false,"description":"chat not found"}`))
	}))
	defer server.Close()

	bale := newBale(BaleConfig{BotToken: "test-token", ChatID: "missing", APIURL: server.URL})
	if err := bale.send("hello"); err == nil {
		t.Fatal("expected Bale API error")
	}
}
