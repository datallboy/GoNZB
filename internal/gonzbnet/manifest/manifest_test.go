package manifest

import (
	"encoding/xml"
	"testing"
)

func TestValidateManifestIDAndRejectTamper(t *testing.T) {
	item := testManifest(t)
	if _, err := Validate(item); err != nil {
		t.Fatalf("validate manifest: %v", err)
	}
	item.ManifestCore.Files[0].Segments[0].MessageID = "<tampered@example.invalid>"
	if _, err := Validate(item); err == nil {
		t.Fatalf("expected tampered manifest_core to fail manifest_id validation")
	}
}

func TestValidateRejectsMalformedMessageIDs(t *testing.T) {
	for _, messageID := range []string{
		"",
		"seg1@example.invalid",
		"<seg1>",
		"<@example.invalid>",
		"<seg1@>",
		"<seg 1@example.invalid>",
		"<seg1@example.invalid><extra@example.invalid>",
	} {
		item := testManifestWithMessageID(t, messageID)
		if _, err := Validate(item); err == nil {
			t.Fatalf("expected malformed Message-ID %q to fail validation", messageID)
		}
	}
}

func TestValidateRejectsUnimplementedManifestEncoding(t *testing.T) {
	compressed := testManifest(t)
	compressed.Compression = "zstd"
	if _, err := Validate(compressed); err == nil {
		t.Fatalf("expected unsupported compression rejection")
	}

	encrypted := testManifest(t)
	encrypted.Encrypted = true
	if _, err := Validate(encrypted); err == nil {
		t.Fatalf("expected unsupported encryption rejection")
	}
}

func TestGenerateNZBProducesParsableXML(t *testing.T) {
	payload, err := GenerateNZB(testManifest(t))
	if err != nil {
		t.Fatalf("generate nzb: %v", err)
	}
	var doc struct {
		XMLName xml.Name `xml:"nzb"`
		Files   []struct {
			Subject string `xml:"subject,attr"`
			Groups  []struct {
				Name string `xml:",chardata"`
			} `xml:"groups>group"`
			Segments []struct {
				Number int    `xml:"number,attr"`
				Bytes  int64  `xml:"bytes,attr"`
				ID     string `xml:",chardata"`
			} `xml:"segments>segment"`
		} `xml:"file"`
	}
	if err := xml.Unmarshal(payload, &doc); err != nil {
		t.Fatalf("parse generated nzb: %v", err)
	}
	if doc.XMLName.Local != "nzb" || len(doc.Files) != 1 || len(doc.Files[0].Segments) != 1 {
		t.Fatalf("unexpected nzb document: %+v", doc)
	}
}

func testManifest(t *testing.T) ResolutionManifest {
	t.Helper()
	return testManifestWithMessageID(t, "<seg1@example.invalid>")
}

func testManifestWithMessageID(t *testing.T, messageID string) ResolutionManifest {
	t.Helper()
	core := ManifestCore{
		Groups:   []string{"alt.binaries.example"},
		Poster:   "poster@example.invalid",
		PostedAt: "2026-07-09T12:00:00Z",
		Files: []ManifestFile{{
			Name:      "example.rar",
			Subject:   "Example example.rar yEnc",
			Date:      "2026-07-09T12:01:00Z",
			SizeBytes: 1000,
			Segments: []ManifestSegment{{
				Number:    1,
				Bytes:     1000,
				MessageID: messageID,
			}},
		}},
		NZB: NZBInfo{Generator: "GoNZBNet", XMLCharset: "utf-8"},
	}
	manifestID, _, err := ComputeID(core)
	if err != nil {
		t.Fatalf("compute id: %v", err)
	}
	return ResolutionManifest{
		SchemaVersion: "1.0",
		Type:          Type,
		ManifestID:    manifestID,
		ReleaseID:     "rel_1",
		ManifestCore:  core,
	}
}
