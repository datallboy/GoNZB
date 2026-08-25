package pesto

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestArgumentsUseVerifiedPestoFlagsAndKeepPasswordOutOfCommand(t *testing.T) {
	client, err := New(Config{
		Binary: "/opt/pesto", ConfigPath: "/etc/pesto.toml", Compression: "7z",
		Encryption: true, Obfuscation: "full", PAR2Percent: 10,
		ExtraArgs: []string{"--groups", "alt.binaries.example"},
	})
	if err != nil {
		t.Fatal(err)
	}
	args, err := client.arguments(PostRequest{
		InputPath: "/var/lib/gonzb-worker/jobs/id/source/release",
		OutputNZB: "/var/lib/gonzb-worker/jobs/id/output/release.nzb", Name: "Release.Name",
	})
	if err != nil {
		t.Fatal(err)
	}
	joined := strings.Join(args, " ")
	for _, expected := range []string{
		"--config=/etc/pesto.toml", "--compress=7z", "--password=", "--obfuscate=full",
		"--par2 10", "--output-format json", "--nzb-conflict=fail", "--no-hooks",
	} {
		if !strings.Contains(joined, expected) {
			t.Fatalf("arguments missing %q: %s", expected, joined)
		}
	}
	if strings.Contains(joined, "Release.Name") || strings.Contains(joined, "--nzb-title") {
		t.Fatalf("release title leaked into Pesto NZB arguments: %s", joined)
	}
}

func TestParseEventsAndNZBPassword(t *testing.T) {
	result := &PostResult{NZBPath: "/tmp/release.nzb"}
	events := strings.Join([]string{
		`{"type":"started","total_files":1}`,
		`archive password: AbCdEfGhIjKlMnOpQrStUv12`,
		`{"type":"segment_done","bytes":123,"ok":true}`,
		`{"type":"finished","failures":0,"progress_pct":91.2,"ok":true}`,
		`{"type":"nzb_written","path":"/tmp/release.nzb"}`,
	}, "\n")
	if err := parseEvents(strings.NewReader(events), result); err != nil {
		t.Fatal(err)
	}
	if result.BytesPosted != 123 || result.ArticleCount != 1 {
		t.Fatalf("unexpected result: %+v", result)
	}

	filename := filepath.Join(t.TempDir(), "release.nzb")
	rawNZB := `<?xml version="1.0"?><nzb><head><meta type="title">Release.Name</meta><meta type="password">secret-value</meta><meta type="tag">obfuscated:full</meta></head><file poster="poster@example.invalid" date="1700000000" subject="&quot;random.rar&quot; yEnc (1/1)"><groups><group>alt.test</group></groups><segments><segment bytes="123" number="1">one@example.invalid</segment></segments></file></nzb>`
	if err := os.WriteFile(filename, []byte(rawNZB), 0o600); err != nil {
		t.Fatal(err)
	}
	password, err := sanitizeNZBFile(filename)
	if err != nil || password != "secret-value" {
		t.Fatalf("password=%q err=%v", password, err)
	}
	sanitized, err := os.ReadFile(filename)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(sanitized), "Release.Name") || strings.Contains(string(sanitized), "obfuscated:full") {
		t.Fatalf("sanitized NZB retained descriptive metadata: %s", sanitized)
	}
}

func TestParseEventsRejectsUnexpectedUnstructuredOutput(t *testing.T) {
	result := &PostResult{NZBPath: "/tmp/release.nzb"}
	events := strings.Join([]string{
		`archive password: too-short`,
		`{"type":"finished","failures":0,"progress_pct":100,"ok":true}`,
	}, "\n")
	if err := parseEvents(strings.NewReader(events), result); err == nil {
		t.Fatal("expected malformed unstructured output to be rejected")
	}
}

func TestManagedPestoArgumentsCannotBeOverridden(t *testing.T) {
	_, err := New(Config{Binary: "pesto", ExtraArgs: []string{"--post-hook=/tmp/hook"}})
	if err == nil {
		t.Fatal("expected managed hook option to be rejected")
	}
}
