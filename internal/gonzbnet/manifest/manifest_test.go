package manifest

import (
	"encoding/xml"
	"strings"
	"testing"
	"time"
)

func TestValidateRequestRejectsMissingAndStaleBindings(t *testing.T) {
	now := time.Date(2026, 8, 6, 12, 0, 0, 0, time.UTC)
	valid := Request{
		SchemaVersion: "1.0", Type: "ManifestRequest", RequestID: "req_1",
		ManifestID: "man_1", ReleaseID: "rel_1", PoolID: "pool_1",
		RequestingNodeID: "node_1", Reason: "user_get", CreatedAt: now.Format(time.RFC3339),
	}
	if err := ValidateRequest(valid, now, 2*time.Minute); err != nil {
		t.Fatalf("validate request: %v", err)
	}
	tests := []struct {
		name   string
		mutate func(*Request)
	}{
		{name: "missing release", mutate: func(in *Request) { in.ReleaseID = "" }},
		{name: "missing pool", mutate: func(in *Request) { in.PoolID = "" }},
		{name: "wrong type", mutate: func(in *Request) { in.Type = "OtherRequest" }},
		{name: "stale timestamp", mutate: func(in *Request) { in.CreatedAt = now.Add(-3 * time.Minute).Format(time.RFC3339) }},
		{name: "future timestamp", mutate: func(in *Request) { in.CreatedAt = now.Add(3 * time.Minute).Format(time.RFC3339) }},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			item := valid
			tt.mutate(&item)
			if err := ValidateRequest(item, now, 2*time.Minute); err == nil {
				t.Fatal("expected request validation failure")
			}
		})
	}
}

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

func TestArchivePasswordParticipatesInIDAndGeneratedNZB(t *testing.T) {
	item := testManifest(t)
	withoutPassword := item.ManifestID
	item.ManifestCore.ArchivePassword = " synthetic-secret "
	manifestID, _, err := ComputeID(item.ManifestCore)
	if err != nil {
		t.Fatalf("compute passworded manifest ID: %v", err)
	}
	if manifestID == withoutPassword {
		t.Fatal("archive password must participate in the manifest ID")
	}
	item.ManifestID = manifestID
	payload, err := GenerateNZB(item)
	if err != nil {
		t.Fatalf("generate passworded NZB: %v", err)
	}
	if !strings.Contains(string(payload), `<meta type="password"> synthetic-secret </meta>`) {
		t.Fatalf("generated NZB does not contain the archive password: %s", payload)
	}
}

func TestGenerateNZBPreservesPerFilePosters(t *testing.T) {
	item := testManifest(t)
	item.ManifestCore.Poster = "fallback@example.invalid"
	item.ManifestCore.Files[0].Poster = "first@example.invalid"
	item.ManifestCore.Files = append(item.ManifestCore.Files, ManifestFile{
		Name: "recovery.par2", Subject: `"recovery.par2" yEnc (1/1)`,
		Poster: "second@example.invalid", Date: "2026-07-09T12:02:00Z", SizeBytes: 250,
		Segments: []ManifestSegment{{Number: 1, Bytes: 250, MessageID: "<seg2@example.invalid>"}},
	})
	manifestID, _, err := ComputeID(item.ManifestCore)
	if err != nil {
		t.Fatal(err)
	}
	item.ManifestID = manifestID
	payload, err := GenerateNZB(item)
	if err != nil {
		t.Fatal(err)
	}
	text := string(payload)
	if !strings.Contains(text, `poster="first@example.invalid"`) || !strings.Contains(text, `poster="second@example.invalid"`) {
		t.Fatalf("generated NZB did not preserve per-file posters: %s", payload)
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
