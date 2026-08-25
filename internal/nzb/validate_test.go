package nzb

import (
	"strings"
	"testing"
)

const syntheticNZB = `<?xml version="1.0" encoding="UTF-8"?>
<nzb xmlns="http://www.newzbin.com/DTD/2003/nzb">
  <head>
    <meta type="name">Synthetic.Release</meta>
    <meta type="password">test-only-password</meta>
  </head>
  <file poster="fixture@example.invalid" date="1700000000" subject="&quot;synthetic.bin&quot; yEnc (1/2)">
    <groups><group>alt.test.gonzb</group><group>alt.test.gonzb</group></groups>
    <segments>
      <segment bytes="128" number="1">segment-1@example.invalid</segment>
      <segment bytes="64" number="2">&lt;segment-2@example.invalid&gt;</segment>
    </segments>
  </file>
</nzb>`

func TestValidateBytesDerivesFactsAndNormalizesMessageIDs(t *testing.T) {
	doc, err := ValidateBytes([]byte(syntheticNZB), DefaultLimits())
	if err != nil {
		t.Fatalf("validate synthetic NZB: %v", err)
	}
	if doc.Facts.Title != "Synthetic.Release" || doc.Facts.Password != "test-only-password" {
		t.Fatalf("unexpected metadata facts: %#v", doc.Facts)
	}
	if doc.Facts.SizeBytes != 192 || doc.Facts.FileCount != 1 || doc.Facts.SegmentCount != 2 {
		t.Fatalf("unexpected counts: %#v", doc.Facts)
	}
	if len(doc.Facts.Groups) != 1 || doc.Facts.Groups[0] != "alt.test.gonzb" {
		t.Fatalf("expected stable group deduplication, got %#v", doc.Facts.Groups)
	}
	if got := doc.Model.Files[0].Segments[0].MessageID; got != "<segment-1@example.invalid>" {
		t.Fatalf("expected bracketed message ID, got %q", got)
	}
	if got := doc.Facts.Files[0].Name; got != "synthetic.bin" {
		t.Fatalf("expected subject filename, got %q", got)
	}
}

func TestValidateBytesRejectsUnsafeAndIncompleteDocuments(t *testing.T) {
	tests := map[string]string{
		"empty":             "",
		"wrong root":        `<rss></rss>`,
		"trailing document": syntheticNZB + `<nzb></nzb>`,
		"missing segments":  `<nzb><file poster="x" date="1" subject="x"><groups><group>alt.test</group></groups></file></nzb>`,
		"bad message id":    strings.Replace(syntheticNZB, "segment-1@example.invalid", "has space@example.invalid", 1),
		"duplicate number":  strings.Replace(syntheticNZB, `number="2"`, `number="1"`, 1),
		"zero bytes":        strings.Replace(syntheticNZB, `bytes="128"`, `bytes="0"`, 1),
	}
	for name, raw := range tests {
		t.Run(name, func(t *testing.T) {
			if _, err := ValidateBytes([]byte(raw), DefaultLimits()); err == nil {
				t.Fatal("expected validation error")
			}
		})
	}
}

func TestValidateBytesEnforcesConfiguredLimits(t *testing.T) {
	limits := DefaultLimits()
	limits.MaxSegments = 1
	if _, err := ValidateBytes([]byte(syntheticNZB), limits); err == nil || !strings.Contains(err.Error(), "segment limit") {
		t.Fatalf("expected segment limit error, got %v", err)
	}

	limits = DefaultLimits()
	limits.MaxXMLDepth = 2
	if _, err := ValidateBytes([]byte(syntheticNZB), limits); err == nil || !strings.Contains(err.Error(), "depth limit") {
		t.Fatalf("expected depth limit error, got %v", err)
	}
}
