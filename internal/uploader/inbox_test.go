package uploader_test

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/datallboy/gonzb/internal/nzb"
	"github.com/datallboy/gonzb/internal/store/sqliteuploader"
	"github.com/datallboy/gonzb/internal/uploader"
)

const inboxNZB = `<?xml version="1.0"?><nzb><file poster="fixture@example.invalid" date="1700000000" subject="fixture.bin yEnc"><groups><group>alt.test.gonzb</group></groups><segments><segment bytes="16" number="1">fixture@example.invalid</segment></segments></file></nzb>`

func TestInboxScannerRecursesWithoutMutatingProducerOutput(t *testing.T) {
	root := t.TempDir()
	inbox := filepath.Join(root, "inbox")
	nested := filepath.Join(inbox, "any", "producer", "layout")
	if err := os.MkdirAll(nested, 0755); err != nil {
		t.Fatal(err)
	}
	nzbPath := filepath.Join(nested, "fixture.nzb")
	if err := os.WriteFile(nzbPath, []byte(inboxNZB), 0444); err != nil {
		t.Fatal(err)
	}
	old := time.Now().Add(-2 * time.Minute)
	if err := os.Chtimes(nzbPath, old, old); err != nil {
		t.Fatal(err)
	}
	store, err := sqliteuploader.NewStore(filepath.Join(root, "metadata.db"), filepath.Join(root, "blobs"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	service := uploader.NewService(store, nzb.DefaultLimits())
	scanner := uploader.NewInboxScanner(service, uploader.InboxOptions{
		Root: inbox, SettleAge: time.Minute, MaxNZBBytes: 1 << 20,
	})

	first, err := scanner.ScanOnce(t.Context())
	if err != nil {
		t.Fatalf("scan inbox: %v", err)
	}
	if first.Created != 1 || first.Eligible != 1 {
		t.Fatalf("unexpected first scan: %#v", first)
	}
	if _, err := os.Stat(nzbPath); err != nil {
		t.Fatalf("producer NZB was mutated: %v", err)
	}
	second, err := scanner.ScanOnce(t.Context())
	if err != nil {
		t.Fatalf("rescan inbox: %v", err)
	}
	if second.Duplicate != 1 || second.Created != 0 {
		t.Fatalf("expected duplicate rescan: %#v", second)
	}
}

func TestInboxScannerSuppressesUnchangedFailuresAndRetriesChangedFiles(t *testing.T) {
	root := t.TempDir()
	inbox := filepath.Join(root, "inbox")
	if err := os.MkdirAll(inbox, 0755); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(inbox, "changing.nzb")
	if err := os.WriteFile(path, []byte("not an nzb"), 0644); err != nil {
		t.Fatal(err)
	}
	old := time.Now().Add(-2 * time.Minute)
	if err := os.Chtimes(path, old, old); err != nil {
		t.Fatal(err)
	}
	store, err := sqliteuploader.NewStore(filepath.Join(root, "metadata.db"), filepath.Join(root, "blobs"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	scanner := uploader.NewInboxScanner(uploader.NewService(store, nzb.DefaultLimits()), uploader.InboxOptions{
		Root: inbox, SettleAge: time.Minute, MaxNZBBytes: 1 << 20,
	})
	first, err := scanner.ScanOnce(t.Context())
	if err != nil || first.Failed != 1 {
		t.Fatalf("first invalid scan: result=%#v err=%v", first, err)
	}
	second, err := scanner.ScanOnce(t.Context())
	if err != nil || second.Failed != 0 || second.Eligible != 0 {
		t.Fatalf("unchanged failure should be suppressed: result=%#v err=%v", second, err)
	}
	if err := os.WriteFile(path, []byte(inboxNZB), 0644); err != nil {
		t.Fatal(err)
	}
	changedOld := old.Add(time.Second)
	if err := os.Chtimes(path, changedOld, changedOld); err != nil {
		t.Fatal(err)
	}
	third, err := scanner.ScanOnce(t.Context())
	if err != nil || third.Created != 1 {
		t.Fatalf("changed file should retry: result=%#v err=%v", third, err)
	}
}
