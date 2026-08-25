package transfer

import (
	"os"
	"path/filepath"
	"testing"
)

func TestResolveSourceConfinesTorrentToConfiguredRoot(t *testing.T) {
	client, err := New(Config{Host: "seedbox.example", User: "worker", Port: 22, SourceRoot: "/downloads"})
	if err != nil {
		t.Fatal(err)
	}
	remote, name, err := client.ResolveSource("/downloads/tv/Release.Name", "", "ignored")
	if err != nil || remote != "/downloads/tv/Release.Name" || name != "Release.Name" {
		t.Fatalf("remote=%q name=%q err=%v", remote, name, err)
	}
	if _, _, err := client.ResolveSource("/other/private", "", "ignored"); err == nil {
		t.Fatal("expected source-root containment error")
	}
	if _, _, err := client.ResolveSource("/downloads", "", "ignored"); err == nil {
		t.Fatal("expected whole source root to be rejected")
	}
}

func TestRsyncRejectsSourceMutatingExtraArguments(t *testing.T) {
	_, err := New(Config{
		Host: "seedbox.example", User: "worker", Port: 22, SourceRoot: "/downloads",
		ExtraArgs: []string{"--remove-source-files"},
	})
	if err == nil {
		t.Fatal("expected unsafe rsync option to be rejected")
	}
}

func TestTreeSize(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "one"), []byte("123"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "two"), []byte("4567"), 0o600); err != nil {
		t.Fatal(err)
	}
	size, err := treeSize(dir)
	if err != nil || size != 7 {
		t.Fatalf("size=%d err=%v", size, err)
	}
}
