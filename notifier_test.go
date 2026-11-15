package main

import (
	"strings"
	"testing"
)

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
