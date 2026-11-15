package main

import (
	"strings"
	"testing"
)

type recordingNotifier struct {
	tags     []string
	messages []string
}

func (n *recordingNotifier) send(text string) error {
	n.messages = append(n.messages, text)
	return nil
}

func (n *recordingNotifier) routingTags() []string {
	return n.tags
}

func TestWithAlertTagsNormalizesAndDeduplicates(t *testing.T) {
	message := withAlertTags("alert\n", "Node Exporter", "node-exporter", "Usa 137", "#CPU")
	if !strings.HasSuffix(message, "#gomon #nodeexporter #usa137 #cpu") {
		t.Fatalf("unexpected tags: %s", message)
	}
	if strings.Count(message, "#gomon") != 1 || strings.Count(message, "#nodeexporter") != 1 {
		t.Fatalf("tags must be unique: %s", message)
	}
}

func TestBuildNotifiersIncludesBale(t *testing.T) {
	notifiers, err := buildNotifiers(&Config{Bale: BaleConfig{BotToken: "token", ChatID: "channel"}})
	if err != nil {
		t.Fatal(err)
	}
	if len(notifiers) != 1 {
		t.Fatalf("expected one notifier, got %d", len(notifiers))
	}
	if _, ok := notifiers[0].(*Bale); !ok {
		t.Fatalf("expected Bale notifier, got %T", notifiers[0])
	}
}

func TestNotifyRoutesToAnyMatchingChannelTag(t *testing.T) {
	general := &recordingNotifier{tags: []string{"general"}}
	infra := &recordingNotifier{tags: []string{"infra", "critical"}}
	web := &recordingNotifier{tags: []string{"web"}}
	notifiers := []Notifier{general, infra, web}

	notify(notifiers, "infra alert", "infra")
	if len(general.messages) != 0 || len(infra.messages) != 1 || len(web.messages) != 0 {
		t.Fatalf("unexpected infra routing: general=%d infra=%d web=%d", len(general.messages), len(infra.messages), len(web.messages))
	}

	notify(notifiers, "shared alert", "general", "web")
	if len(general.messages) != 1 || len(infra.messages) != 1 || len(web.messages) != 1 {
		t.Fatalf("unexpected shared routing: general=%d infra=%d web=%d", len(general.messages), len(infra.messages), len(web.messages))
	}
}

func TestNotifyDefaultsMissingTagsToGeneral(t *testing.T) {
	general := &recordingNotifier{}
	other := &recordingNotifier{tags: []string{"other"}}
	notify([]Notifier{general, other}, "default alert")
	if len(general.messages) != 1 || len(other.messages) != 0 {
		t.Fatalf("unexpected default routing: general=%d other=%d", len(general.messages), len(other.messages))
	}
}
