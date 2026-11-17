package main

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

type retryingNotifier struct {
	calls    atomic.Int32
	failures int32
}

func (n *retryingNotifier) send(string) error {
	if n.calls.Add(1) <= n.failures {
		return errors.New("temporary delivery failure")
	}
	return nil
}

func (n *retryingNotifier) routingTags() []string { return []string{"general"} }
func (n *retryingNotifier) notifierID() string    { return "test:primary" }

type timeoutNotifier struct{}

func (n *timeoutNotifier) send(string) error     { return nil }
func (n *timeoutNotifier) routingTags() []string { return []string{"general"} }
func (n *timeoutNotifier) notifierID() string    { return "test:timeout" }
func (n *timeoutNotifier) sendContext(ctx context.Context, _ string) error {
	<-ctx.Done()
	return ctx.Err()
}

func TestNotificationQueueRestoresAndRetriesDelivery(t *testing.T) {
	directory := t.TempDir()
	cfg := NotificationConfig{
		QueueFile:    filepath.Join(directory, "queue.json"),
		DeliveryLog:  filepath.Join(directory, "deliveries.log"),
		Timeout:      100 * time.Millisecond,
		InitialRetry: 5 * time.Millisecond,
		MaxRetry:     10 * time.Millisecond,
		MaxAttempts:  3,
	}
	notifier := &retryingNotifier{failures: 1}

	seed := &notificationDispatcher{
		cfg:       cfg,
		notifiers: map[string]Notifier{notifier.notifierID(): notifier},
		wake:      make(chan struct{}, 1),
	}
	if err := seed.enqueue("persist me", []string{"general"}); err != nil {
		t.Fatal(err)
	}
	queued, err := os.ReadFile(cfg.QueueFile)
	if err != nil || !strings.Contains(string(queued), "persist me") {
		t.Fatalf("notification was not persisted before delivery: data=%s err=%v", queued, err)
	}

	dispatcher, err := newNotificationDispatcher(cfg, []Notifier{notifier})
	if err != nil {
		t.Fatal(err)
	}
	defer dispatcher.close()

	waitForCondition(t, 2*time.Second, func() bool {
		data, err := os.ReadFile(cfg.DeliveryLog)
		return err == nil && notifier.calls.Load() == 2 && strings.Contains(string(data), `"status":"delivered"`) && strings.Contains(string(data), `"attempts":2`)
	})
	remaining, err := os.ReadFile(cfg.QueueFile)
	if err != nil || strings.TrimSpace(string(remaining)) != "[]" {
		t.Fatalf("delivered notification should leave the queue: data=%s err=%v", remaining, err)
	}
}

func TestNotificationQueueAppliesDeliveryTimeout(t *testing.T) {
	directory := t.TempDir()
	cfg := NotificationConfig{
		QueueFile:    filepath.Join(directory, "queue.json"),
		DeliveryLog:  filepath.Join(directory, "deliveries.log"),
		Timeout:      20 * time.Millisecond,
		InitialRetry: time.Millisecond,
		MaxRetry:     time.Millisecond,
		MaxAttempts:  1,
	}
	dispatcher, err := newNotificationDispatcher(cfg, []Notifier{&timeoutNotifier{}})
	if err != nil {
		t.Fatal(err)
	}
	defer dispatcher.close()
	dispatcher.notify("time out", "general")

	waitForCondition(t, 2*time.Second, func() bool {
		data, err := os.ReadFile(cfg.DeliveryLog)
		return err == nil && strings.Contains(string(data), `"status":"failed"`) && strings.Contains(string(data), "deadline exceeded")
	})
}

func waitForCondition(t *testing.T, timeout time.Duration, condition func() bool) {
	t.Helper()
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		if condition() {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatal("condition was not met before timeout")
}
