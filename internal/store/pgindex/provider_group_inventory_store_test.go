package pgindex

import (
	"strings"
	"testing"
	"unicode/utf8"
)

func TestSanitizeProviderGroupInventoryItemRepairsInvalidUTF8(t *testing.T) {
	row := sanitizeProviderGroupInventoryItem(IndexerProviderGroupInventoryItem{
		ProviderID:   " easynews ",
		ProviderName: "news\x00.easynews.com",
		GroupName:    "alt.binaries.\xe8quebec",
		Status:       " y ",
		ScannedAt:    " 2026-07-06T13:30:00Z ",
	})

	if !utf8.ValidString(row.GroupName) {
		t.Fatalf("expected valid UTF-8 group name, got %q", row.GroupName)
	}
	if strings.Contains(row.GroupName, "\xe8") {
		t.Fatalf("expected invalid byte to be removed, got %q", row.GroupName)
	}
	if row.GroupName != "alt.binaries.quebec" {
		t.Fatalf("unexpected sanitized group name %q", row.GroupName)
	}
	if row.ProviderName != "news.easynews.com" {
		t.Fatalf("expected NUL removed from provider name, got %q", row.ProviderName)
	}
	if row.ProviderID != "easynews" || row.Status != "y" || row.ScannedAt != "2026-07-06T13:30:00Z" {
		t.Fatalf("unexpected sanitized row: %+v", row)
	}
}
