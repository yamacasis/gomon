package main

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"sync"
	"time"
)

type contextNotifier interface {
	sendContext(context.Context, string) error
}

type identifiedNotifier interface {
	notifierID() string
}

type queuedDelivery struct {
	Notifier    string    `json:"notifier"`
	Attempts    int       `json:"attempts"`
	NextAttempt time.Time `json:"next_attempt"`
	LastError   string    `json:"last_error,omitempty"`
}

type queuedNotification struct {
	ID         string           `json:"id"`
	Text       string           `json:"text"`
	Tags       []string         `json:"tags"`
	CreatedAt  time.Time        `json:"created_at"`
	Deliveries []queuedDelivery `json:"deliveries"`
}

type notificationDeliveryResult struct {
	MessageID string    `json:"message_id"`
	Notifier  string    `json:"notifier"`
	Status    string    `json:"status"`
	Attempts  int       `json:"attempts"`
	Error     string    `json:"error,omitempty"`
	Timestamp time.Time `json:"timestamp"`
}

type notificationDispatcher struct {
	cfg       NotificationConfig
	notifiers map[string]Notifier

	mu     sync.Mutex
	items  []queuedNotification
	wake   chan struct{}
	ctx    context.Context
	cancel context.CancelFunc
	wg     sync.WaitGroup
}

func newNotificationDispatcher(cfg NotificationConfig, notifiers []Notifier) (*notificationDispatcher, error) {
	if cfg.QueueFile == "" {
		cfg.QueueFile = "notification-queue.json"
	}
	if cfg.DeliveryLog == "" {
		cfg.DeliveryLog = "notification-deliveries.log"
	}
	if cfg.Timeout <= 0 {
		cfg.Timeout = defaultNotificationTimeout
	}
	if cfg.InitialRetry <= 0 {
		cfg.InitialRetry = defaultNotificationInitialRetry
	}
	if cfg.MaxRetry <= 0 {
		cfg.MaxRetry = defaultNotificationMaxRetry
	}
	if cfg.MaxAttempts <= 0 {
		cfg.MaxAttempts = defaultNotificationMaxAttempts
	}
	ctx, cancel := context.WithCancel(context.Background())
	d := &notificationDispatcher{
		cfg:       cfg,
		notifiers: make(map[string]Notifier, len(notifiers)),
		wake:      make(chan struct{}, 1),
		ctx:       ctx,
		cancel:    cancel,
	}
	for _, notifier := range notifiers {
		id := notifierIdentifier(notifier)
		if _, duplicate := d.notifiers[id]; duplicate {
			cancel()
			return nil, fmt.Errorf("duplicate notifier identifier %q", id)
		}
		d.notifiers[id] = notifier
	}
	if err := d.load(); err != nil {
		cancel()
		return nil, err
	}
	d.wg.Add(1)
	go d.run()
	return d, nil
}

func (d *notificationDispatcher) notify(text string, eventTags ...string) {
	if err := d.enqueue(text, eventTags); err != nil {
		log.Printf("notification queue error: %v", err)
	}
}

func (d *notificationDispatcher) enqueue(text string, eventTags []string) error {
	now := time.Now().UTC()
	item := queuedNotification{
		ID:        newNotificationID(now),
		Text:      text,
		Tags:      append([]string(nil), eventTags...),
		CreatedAt: now,
	}
	for id, notifier := range d.notifiers {
		if routingTagsMatch(notifier.routingTags(), eventTags) {
			item.Deliveries = append(item.Deliveries, queuedDelivery{Notifier: id, NextAttempt: now})
		}
	}
	if len(item.Deliveries) == 0 {
		log.Printf("[NOTIFY] no configured channel matched tags %v", normalizeRoutingTags(eventTags))
		return nil
	}

	d.mu.Lock()
	d.items = append(d.items, item)
	if err := d.persistLocked(); err != nil {
		d.items = d.items[:len(d.items)-1]
		d.mu.Unlock()
		return fmt.Errorf("persist queued notification: %w", err)
	}
	d.mu.Unlock()
	log.Printf("[NOTIFY] queued message=%s channels=%d", item.ID, len(item.Deliveries))
	select {
	case d.wake <- struct{}{}:
	default:
	}
	return nil
}

func (d *notificationDispatcher) run() {
	defer d.wg.Done()
	for {
		d.processDue()
		timer := time.NewTimer(d.delayUntilNextAttempt(time.Now().UTC()))
		select {
		case <-d.ctx.Done():
			if !timer.Stop() {
				select {
				case <-timer.C:
				default:
				}
			}
			return
		case <-d.wake:
			if !timer.Stop() {
				select {
				case <-timer.C:
				default:
				}
			}
		case <-timer.C:
		}
	}
}

func (d *notificationDispatcher) delayUntilNextAttempt(now time.Time) time.Duration {
	d.mu.Lock()
	defer d.mu.Unlock()
	var earliest time.Time
	for _, item := range d.items {
		for _, delivery := range item.Deliveries {
			if earliest.IsZero() || delivery.NextAttempt.Before(earliest) {
				earliest = delivery.NextAttempt
			}
		}
	}
	if earliest.IsZero() {
		return time.Hour
	}
	if delay := earliest.Sub(now); delay > 0 {
		return delay
	}
	return time.Millisecond
}

func (d *notificationDispatcher) processDue() {
	for {
		itemIndex, deliveryIndex, item, delivery, notifier, ok := d.nextDue(time.Now().UTC())
		if !ok {
			return
		}

		delivery.Attempts++
		var err error
		if notifier == nil {
			err = fmt.Errorf("notifier %q is no longer configured", delivery.Notifier)
		} else {
			ctx, cancel := context.WithTimeout(d.ctx, d.cfg.Timeout)
			err = sendNotifier(ctx, notifier, item.Text)
			cancel()
		}
		if err != nil && d.ctx.Err() != nil {
			return
		}

		d.mu.Lock()
		if itemIndex >= len(d.items) || deliveryIndex >= len(d.items[itemIndex].Deliveries) ||
			d.items[itemIndex].ID != item.ID || d.items[itemIndex].Deliveries[deliveryIndex].Notifier != delivery.Notifier {
			d.mu.Unlock()
			continue
		}
		current := &d.items[itemIndex].Deliveries[deliveryIndex]
		current.Attempts = delivery.Attempts
		if err == nil {
			d.recordResultLocked(notificationDeliveryResult{
				MessageID: item.ID, Notifier: delivery.Notifier, Status: "delivered",
				Attempts: delivery.Attempts, Timestamp: time.Now().UTC(),
			})
			log.Printf("[NOTIFY] delivered message=%s channel=%s attempts=%d", item.ID, delivery.Notifier, delivery.Attempts)
			d.removeDeliveryLocked(itemIndex, deliveryIndex)
		} else if delivery.Attempts >= d.cfg.MaxAttempts {
			current.LastError = err.Error()
			d.recordResultLocked(notificationDeliveryResult{
				MessageID: item.ID, Notifier: delivery.Notifier, Status: "failed",
				Attempts: delivery.Attempts, Error: err.Error(), Timestamp: time.Now().UTC(),
			})
			log.Printf("[NOTIFY] permanently failed message=%s channel=%s attempts=%d error=%v", item.ID, delivery.Notifier, delivery.Attempts, err)
			d.removeDeliveryLocked(itemIndex, deliveryIndex)
		} else {
			current.LastError = err.Error()
			current.NextAttempt = time.Now().UTC().Add(d.retryDelay(delivery.Attempts))
			log.Printf("[NOTIFY] retry scheduled message=%s channel=%s attempt=%d next=%s error=%v",
				item.ID, delivery.Notifier, delivery.Attempts, current.NextAttempt.Format(time.RFC3339), err)
		}
		if persistErr := d.persistLocked(); persistErr != nil {
			log.Printf("notification queue persist error: %v", persistErr)
		}
		d.mu.Unlock()
	}
}

func (d *notificationDispatcher) nextDue(now time.Time) (int, int, queuedNotification, queuedDelivery, Notifier, bool) {
	d.mu.Lock()
	defer d.mu.Unlock()
	for itemIndex := range d.items {
		for deliveryIndex := range d.items[itemIndex].Deliveries {
			delivery := d.items[itemIndex].Deliveries[deliveryIndex]
			if delivery.NextAttempt.After(now) {
				continue
			}
			item := d.items[itemIndex]
			return itemIndex, deliveryIndex, item, delivery, d.notifiers[delivery.Notifier], true
		}
	}
	return 0, 0, queuedNotification{}, queuedDelivery{}, nil, false
}

func (d *notificationDispatcher) removeDeliveryLocked(itemIndex, deliveryIndex int) {
	deliveries := d.items[itemIndex].Deliveries
	d.items[itemIndex].Deliveries = append(deliveries[:deliveryIndex], deliveries[deliveryIndex+1:]...)
	if len(d.items[itemIndex].Deliveries) == 0 {
		d.items = append(d.items[:itemIndex], d.items[itemIndex+1:]...)
	}
}

func (d *notificationDispatcher) retryDelay(attempt int) time.Duration {
	delay := d.cfg.InitialRetry
	for i := 1; i < attempt && delay < d.cfg.MaxRetry; i++ {
		if delay > d.cfg.MaxRetry/2 {
			return d.cfg.MaxRetry
		}
		delay *= 2
	}
	if delay > d.cfg.MaxRetry {
		return d.cfg.MaxRetry
	}
	return delay
}

func (d *notificationDispatcher) load() error {
	data, err := os.ReadFile(d.cfg.QueueFile)
	if os.IsNotExist(err) {
		return nil
	}
	if err != nil {
		return fmt.Errorf("read notification queue: %w", err)
	}
	if len(data) == 0 {
		return nil
	}
	if err := json.Unmarshal(data, &d.items); err != nil {
		return fmt.Errorf("parse notification queue %q: %w", d.cfg.QueueFile, err)
	}
	log.Printf("[NOTIFY] restored %d queued message(s)", len(d.items))
	return nil
}

func (d *notificationDispatcher) persistLocked() error {
	if err := ensureParentDirectory(d.cfg.QueueFile); err != nil {
		return err
	}
	data, err := json.MarshalIndent(d.items, "", "  ")
	if err != nil {
		return err
	}
	temporary := d.cfg.QueueFile + ".tmp"
	if err := os.WriteFile(temporary, data, 0600); err != nil {
		return err
	}
	if err := os.Rename(temporary, d.cfg.QueueFile); err != nil {
		_ = os.Remove(temporary)
		return err
	}
	return nil
}

func (d *notificationDispatcher) recordResultLocked(result notificationDeliveryResult) {
	if err := ensureParentDirectory(d.cfg.DeliveryLog); err != nil {
		log.Printf("notification delivery log error: %v", err)
		return
	}
	file, err := os.OpenFile(d.cfg.DeliveryLog, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0600)
	if err != nil {
		log.Printf("notification delivery log error: %v", err)
		return
	}
	defer file.Close()
	encoded, err := json.Marshal(result)
	if err == nil {
		_, err = file.Write(append(encoded, '\n'))
	}
	if err != nil {
		log.Printf("notification delivery log error: %v", err)
	}
}

func (d *notificationDispatcher) close() {
	d.cancel()
	d.wg.Wait()
	d.mu.Lock()
	defer d.mu.Unlock()
	if err := d.persistLocked(); err != nil {
		log.Printf("notification queue close error: %v", err)
	}
}

func sendNotifier(ctx context.Context, notifier Notifier, text string) error {
	if contextual, ok := notifier.(contextNotifier); ok {
		return contextual.sendContext(ctx, text)
	}
	result := make(chan error, 1)
	go func() { result <- notifier.send(text) }()
	select {
	case err := <-result:
		return err
	case <-ctx.Done():
		return ctx.Err()
	}
}

func notifierIdentifier(notifier Notifier) string {
	if identified, ok := notifier.(identifiedNotifier); ok {
		return identified.notifierID()
	}
	return fmt.Sprintf("%T", notifier)
}

func newNotificationID(now time.Time) string {
	random := make([]byte, 6)
	if _, err := rand.Read(random); err != nil {
		return fmt.Sprintf("%d", now.UnixNano())
	}
	return fmt.Sprintf("%d-%s", now.UnixNano(), hex.EncodeToString(random))
}

func ensureParentDirectory(path string) error {
	parent := filepath.Dir(path)
	if parent == "." || parent == "" {
		return nil
	}
	return os.MkdirAll(parent, 0755)
}
