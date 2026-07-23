package controllers

import (
	"strings"
	"testing"
)

func TestDecodeInboxEventsRejectsDuplicateEnvelopeKey(t *testing.T) {
	_, err := decodeInboxEvents([]byte(`{"event_id":"evt_one","event_id":"evt_two"}`))
	if err == nil || !strings.Contains(err.Error(), "Duplicate key") {
		t.Fatalf("expected duplicate envelope key rejection, got %v", err)
	}
}

func TestDecodeInboxEventsRejectsDuplicateNestedBodyKey(t *testing.T) {
	_, err := decodeInboxEvents([]byte(`{"event_id":"evt_one","body":{"title":"one","title":"two"}}`))
	if err == nil || !strings.Contains(err.Error(), "Duplicate key") {
		t.Fatalf("expected duplicate body key rejection, got %v", err)
	}
}
