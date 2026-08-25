package config

import (
	"os"
	"path/filepath"
	"testing"
)

func TestLoadAppliesSecretEnvironmentOverrides(t *testing.T) {
	filename := filepath.Join(t.TempDir(), "worker.yaml")
	contents := []byte(`
worker:
  data_dir: /tmp/gonzb-worker-test
  node_id: node-1
qbittorrent:
  url: https://qb.example.invalid
transfer:
  ssh_host: seedbox.example.invalid
  ssh_user: worker
  source_root: /downloads
pesto:
  binary: /opt/pesto
gonzb:
  url: https://gonzb.example.invalid
  api_token: file-token
`)
	if err := os.WriteFile(filename, contents, 0o600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("GONZB_WORKER_GONZB_API_TOKEN", "environment-token")
	t.Setenv("GONZB_WORKER_QBITTORRENT_PASSWORD", "qbit-secret")
	t.Setenv("GONZB_WORKER_QBITTORRENT_HTTP_BASIC_USERNAME", "proxy-user")
	t.Setenv("GONZB_WORKER_QBITTORRENT_HTTP_BASIC_PASSWORD", "proxy-secret")
	cfg, err := Load(filename)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.GoNZB.APIToken != "environment-token" || cfg.QBittorrent.Password != "qbit-secret" {
		t.Fatalf("environment overrides were not applied")
	}
	if cfg.QBittorrent.HTTPBasicUsername != "proxy-user" || cfg.QBittorrent.HTTPBasicPassword != "proxy-secret" {
		t.Fatalf("qBittorrent HTTP Basic environment overrides were not applied")
	}
	if cfg.Pesto.Compression != "7z" || cfg.Worker.WorkspaceMultiplier != 2.5 {
		t.Fatalf("defaults not applied: %+v", cfg)
	}
}

func TestValidateRequiresCompleteQBittorrentHTTPBasicCredentials(t *testing.T) {
	cfg := &Config{
		Worker:      WorkerConfig{DataDir: "/var/lib/gonzb-worker", NodeID: "node", PollIntervalSeconds: 30, WorkspaceMultiplier: 2.5},
		QBittorrent: QBittorrentConfig{URL: "https://qb.example", HTTPBasicUsername: "proxy-user", TimeoutSecs: 30},
		Transfer: TransferConfig{
			Type: "rsync", RsyncBinary: "rsync", SSHHost: "seedbox.example", SSHUser: "worker", SSHPort: 22,
			SourceRoot: "/downloads",
		},
		Pesto: PestoConfig{Binary: "/opt/pesto", Compression: "7z", Obfuscation: "full", PAR2Percent: 10},
		GoNZB: GoNZBConfig{URL: "https://gonzb.example", APIToken: "token", TimeoutSecs: 60},
	}
	if err := cfg.Validate(); err == nil {
		t.Fatal("expected incomplete qBittorrent HTTP Basic credentials to be rejected")
	}
	cfg.QBittorrent.HTTPBasicPassword = "proxy-secret"
	if err := cfg.Validate(); err != nil {
		t.Fatal(err)
	}
}

func TestValidateSSHFSConfiguration(t *testing.T) {
	cfg := &Config{
		Worker:      WorkerConfig{DataDir: "/var/lib/gonzb-worker", NodeID: "node", PollIntervalSeconds: 30, WorkspaceMultiplier: 2.5},
		QBittorrent: QBittorrentConfig{URL: "https://qb.example", TimeoutSecs: 30},
		Transfer: TransferConfig{
			Type: "sshfs", SSHFSBinary: "sshfs", UnmountBinary: "fusermount3",
			SSHHost: "seedbox.example", SSHUser: "worker", SSHPort: 22,
			SourceRoot: "/downloads", MountPath: "/mnt/gonzb-seedbox", ManageMount: true, UnmountOnExit: true,
		},
		Pesto: PestoConfig{Binary: "/opt/pesto", Compression: "7z", Obfuscation: "full", PAR2Percent: 10},
		GoNZB: GoNZBConfig{URL: "https://gonzb.example", APIToken: "token", TimeoutSecs: 60},
	}
	if err := cfg.Validate(); err != nil {
		t.Fatal(err)
	}
	cfg.Transfer.MountPath = "/"
	if err := cfg.Validate(); err == nil {
		t.Fatal("expected root SSHFS mount path to be rejected")
	}
	cfg.Transfer.MountPath = "/var/lib/gonzb-worker/seedbox"
	if err := cfg.Validate(); err == nil {
		t.Fatal("expected overlapping workspace and SSHFS mount to be rejected")
	}
}
