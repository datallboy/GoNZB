package nzb

import (
	"bytes"
	"strings"
	"testing"
)

func TestSanitizeBytesRemovesDescriptiveMetadataAndUsesCanonicalLayout(t *testing.T) {
	raw := []byte(`<?xml version="1.0" encoding="UTF-8"?>
<!DOCTYPE nzb PUBLIC "-//newzBin//DTD NZB 1.1//EN" "http://www.newzbin.com/DTD/nzb/nzb-1.1.dtd">
<nzb xmlns="http://www.newzbin.com/DTD/2003/nzb">
  <head>
    <meta type="title">Sensitive.Release.Name</meta>
    <meta type="password">synthetic-password</meta>
    <meta type="tag">obfuscated:full</meta>
  </head>
  <file poster="poster-two@example.invalid" date="1700000000" subject="&quot;random.7z&quot; yEnc (1/2)">
    <groups><group>alt.test.two</group><group>alt.test.one</group></groups>
    <segments>
      <segment bytes="22" number="2">&lt;two@example.invalid&gt;</segment>
      <segment bytes="11" number="1">one@example.invalid</segment>
    </segments>
  </file>
</nzb>`)

	payload, password, err := SanitizeBytes(raw, DefaultLimits())
	if err != nil {
		t.Fatal(err)
	}
	if password != "synthetic-password" {
		t.Fatalf("unexpected password result")
	}
	if bytes.Contains(payload, []byte("Sensitive.Release.Name")) || bytes.Contains(payload, []byte("obfuscated:full")) {
		t.Fatal("sanitized NZB retained descriptive metadata")
	}
	for _, expected := range []string{
		canonicalDoctype,
		`<meta type="password">synthetic-password</meta>`,
		`poster="poster-two@example.invalid"`,
		`subject="&quot;random.7z&quot; yEnc (1/2)"`,
		`<segment bytes="11" number="1">one@example.invalid</segment>`,
		`<segment bytes="22" number="2">two@example.invalid</segment>`,
	} {
		if !strings.Contains(string(payload), expected) {
			t.Fatalf("canonical NZB missing %q:\n%s", expected, payload)
		}
	}
	if strings.Index(string(payload), "alt.test.one") > strings.Index(string(payload), "alt.test.two") {
		t.Fatal("canonical NZB groups are not sorted")
	}
}
