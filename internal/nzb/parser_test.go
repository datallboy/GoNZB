package nzb

import (
	"strings"
	"testing"
)

func TestParserSupportsLegacyXMLCharset(t *testing.T) {
	raw := strings.NewReader(`<?xml version="1.0" encoding="iso-8859-1"?>
<nzb xmlns="http://www.newzbin.com/DTD/2003/nzb">
  <file poster="poster@example.test" date="1700000000" subject="caf&#233;.part01.rar yEnc (1/1)">
    <groups>
      <group>alt.test.example</group>
    </groups>
    <segments>
      <segment bytes="123" number="1">abc123@example.test</segment>
    </segments>
  </file>
</nzb>`)

	model, err := NewParser().Parse(raw)
	if err != nil {
		t.Fatalf("parse legacy charset nzb: %v", err)
	}
	if len(model.Files) != 1 {
		t.Fatalf("expected 1 file, got %d", len(model.Files))
	}
	if got := model.Files[0].Subject; got != "café.part01.rar yEnc (1/1)" {
		t.Fatalf("expected decoded subject, got %q", got)
	}
}
