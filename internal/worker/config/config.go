package config

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/spf13/viper"
)

type Config struct {
	Worker      WorkerConfig      `mapstructure:"worker"`
	QBittorrent QBittorrentConfig `mapstructure:"qbittorrent"`
	Transfer    TransferConfig    `mapstructure:"transfer"`
	Pesto       PestoConfig       `mapstructure:"pesto"`
	GoNZB       GoNZBConfig       `mapstructure:"gonzb"`
}

type WorkerConfig struct {
	DataDir             string  `mapstructure:"data_dir"`
	NodeID              string  `mapstructure:"node_id"`
	PollIntervalSeconds int     `mapstructure:"poll_interval_seconds"`
	MinFreeSpaceGB      float64 `mapstructure:"min_free_space_gb"`
	WorkspaceMultiplier float64 `mapstructure:"workspace_multiplier"`
	CleanupOnSuccess    bool    `mapstructure:"cleanup_on_success"`
}

type QBittorrentConfig struct {
	URL               string `mapstructure:"url"`
	Username          string `mapstructure:"username"`
	Password          string `mapstructure:"password"`
	HTTPBasicUsername string `mapstructure:"http_basic_username"`
	HTTPBasicPassword string `mapstructure:"http_basic_password"`
	CandidateTag      string `mapstructure:"candidate_tag"`
	TimeoutSecs       int    `mapstructure:"timeout_seconds"`
}

type TransferConfig struct {
	Type          string   `mapstructure:"type"`
	RsyncBinary   string   `mapstructure:"rsync_binary"`
	SSHFSBinary   string   `mapstructure:"sshfs_binary"`
	UnmountBinary string   `mapstructure:"unmount_binary"`
	SSHHost       string   `mapstructure:"ssh_host"`
	SSHUser       string   `mapstructure:"ssh_user"`
	SSHPort       int      `mapstructure:"ssh_port"`
	SSHKey        string   `mapstructure:"ssh_key"`
	SourceRoot    string   `mapstructure:"source_root"`
	MountPath     string   `mapstructure:"mount_path"`
	ManageMount   bool     `mapstructure:"manage_mount"`
	UnmountOnExit bool     `mapstructure:"unmount_on_exit"`
	ExtraArgs     []string `mapstructure:"extra_args"`
	SSHFSOptions  []string `mapstructure:"sshfs_options"`
}

type PestoConfig struct {
	Binary      string   `mapstructure:"binary"`
	ConfigPath  string   `mapstructure:"config_path"`
	Compression string   `mapstructure:"compression"`
	Encryption  bool     `mapstructure:"encryption"`
	Obfuscation string   `mapstructure:"obfuscation"`
	PAR2Percent int      `mapstructure:"par2_percent"`
	ExtraArgs   []string `mapstructure:"extra_args"`
}

type GoNZBConfig struct {
	URL         string `mapstructure:"url"`
	APIToken    string `mapstructure:"api_token"`
	TimeoutSecs int    `mapstructure:"timeout_seconds"`
}

func Load(filename string) (*Config, error) {
	if strings.TrimSpace(filename) == "" {
		filename = "gonzb-worker-config.yaml"
	}
	if _, err := os.Stat(filename); err != nil {
		return nil, fmt.Errorf("worker config: %w", err)
	}

	v := viper.New()
	v.SetConfigFile(filename)
	v.SetConfigType("yaml")
	setDefaults(v)
	v.SetEnvPrefix("GONZB_WORKER")
	v.SetEnvKeyReplacer(strings.NewReplacer(".", "_"))
	v.AutomaticEnv()
	for _, key := range []string{
		"worker.data_dir", "worker.node_id", "worker.poll_interval_seconds", "worker.min_free_space_gb",
		"worker.workspace_multiplier", "worker.cleanup_on_success", "qbittorrent.url", "qbittorrent.username",
		"qbittorrent.password", "qbittorrent.http_basic_username", "qbittorrent.http_basic_password",
		"qbittorrent.candidate_tag", "qbittorrent.timeout_seconds", "transfer.type",
		"transfer.rsync_binary", "transfer.sshfs_binary", "transfer.unmount_binary", "transfer.ssh_host",
		"transfer.ssh_user", "transfer.ssh_port", "transfer.ssh_key", "transfer.source_root", "transfer.mount_path",
		"transfer.manage_mount", "transfer.unmount_on_exit", "pesto.binary", "pesto.config_path", "pesto.compression", "pesto.encryption",
		"pesto.obfuscation", "pesto.par2_percent", "gonzb.url", "gonzb.api_token", "gonzb.timeout_seconds",
	} {
		if err := v.BindEnv(key); err != nil {
			return nil, fmt.Errorf("bind %s environment override: %w", key, err)
		}
	}
	if err := v.ReadInConfig(); err != nil {
		return nil, fmt.Errorf("read worker config: %w", err)
	}

	var cfg Config
	if err := v.Unmarshal(&cfg); err != nil {
		return nil, fmt.Errorf("decode worker config: %w", err)
	}
	if err := cfg.Validate(); err != nil {
		return nil, err
	}
	return &cfg, nil
}

func setDefaults(v *viper.Viper) {
	v.SetDefault("worker.data_dir", "/var/lib/gonzb-worker")
	v.SetDefault("worker.node_id", "gonzb-worker")
	v.SetDefault("worker.poll_interval_seconds", 30)
	v.SetDefault("worker.min_free_space_gb", 30.0)
	v.SetDefault("worker.workspace_multiplier", 2.5)
	v.SetDefault("worker.cleanup_on_success", true)
	v.SetDefault("qbittorrent.candidate_tag", "gonzb-candidate")
	v.SetDefault("qbittorrent.timeout_seconds", 30)
	v.SetDefault("transfer.type", "rsync")
	v.SetDefault("transfer.rsync_binary", "rsync")
	v.SetDefault("transfer.sshfs_binary", "sshfs")
	v.SetDefault("transfer.unmount_binary", "fusermount3")
	v.SetDefault("transfer.ssh_port", 22)
	v.SetDefault("transfer.manage_mount", true)
	v.SetDefault("transfer.unmount_on_exit", true)
	v.SetDefault("pesto.binary", "/usr/local/bin/pesto")
	v.SetDefault("pesto.compression", "7z")
	v.SetDefault("pesto.encryption", true)
	v.SetDefault("pesto.obfuscation", "full")
	v.SetDefault("pesto.par2_percent", 10)
	v.SetDefault("gonzb.timeout_seconds", 60)
}

func (c *Config) Validate() error {
	if c == nil {
		return errors.New("worker config is nil")
	}
	var missing []string
	for name, value := range map[string]string{
		"worker.data_dir": c.Worker.DataDir, "worker.node_id": c.Worker.NodeID,
		"qbittorrent.url": c.QBittorrent.URL, "transfer.ssh_host": c.Transfer.SSHHost,
		"transfer.ssh_user": c.Transfer.SSHUser, "transfer.source_root": c.Transfer.SourceRoot,
		"pesto.binary": c.Pesto.Binary, "gonzb.url": c.GoNZB.URL, "gonzb.api_token": c.GoNZB.APIToken,
	} {
		if strings.TrimSpace(value) == "" {
			missing = append(missing, name)
		}
	}
	if len(missing) > 0 {
		return fmt.Errorf("worker config missing required values: %s", strings.Join(missing, ", "))
	}
	if c.Worker.PollIntervalSeconds <= 0 || c.QBittorrent.TimeoutSecs <= 0 || c.GoNZB.TimeoutSecs <= 0 {
		return errors.New("worker and HTTP timeout/interval values must be positive")
	}
	if c.Worker.MinFreeSpaceGB < 0 || c.Worker.WorkspaceMultiplier < 1 {
		return errors.New("worker disk guard values are invalid")
	}
	if (strings.TrimSpace(c.QBittorrent.HTTPBasicUsername) == "") != (strings.TrimSpace(c.QBittorrent.HTTPBasicPassword) == "") {
		return errors.New("qbittorrent.http_basic_username and qbittorrent.http_basic_password must be set together")
	}
	switch c.Transfer.Type {
	case "rsync":
		if strings.TrimSpace(c.Transfer.RsyncBinary) == "" {
			return errors.New("transfer.rsync_binary is required for rsync mode")
		}
	case "sshfs":
		if strings.TrimSpace(c.Transfer.SSHFSBinary) == "" {
			return errors.New("transfer.sshfs_binary is required for sshfs mode")
		}
		if !filepath.IsAbs(c.Transfer.MountPath) || filepath.Clean(c.Transfer.MountPath) == string(filepath.Separator) {
			return errors.New("transfer.mount_path must be an absolute non-root path for sshfs mode")
		}
		if pathsOverlap(c.Worker.DataDir, c.Transfer.MountPath) {
			return errors.New("worker.data_dir and transfer.mount_path must not overlap in sshfs mode")
		}
		if c.Transfer.ManageMount && c.Transfer.UnmountOnExit && strings.TrimSpace(c.Transfer.UnmountBinary) == "" {
			return errors.New("transfer.unmount_binary is required when managed SSHFS mounts are unmounted on exit")
		}
	default:
		return fmt.Errorf("unsupported transfer type %q (supported: rsync, sshfs)", c.Transfer.Type)
	}
	if !filepath.IsAbs(c.Transfer.SourceRoot) {
		return errors.New("transfer.source_root must be an absolute seedbox path")
	}
	if c.Transfer.SSHPort < 1 || c.Transfer.SSHPort > 65535 {
		return errors.New("transfer.ssh_port must be between 1 and 65535")
	}
	switch c.Pesto.Compression {
	case "7z", "zip", "rar":
	default:
		return fmt.Errorf("unsupported pesto.compression %q", c.Pesto.Compression)
	}
	switch c.Pesto.Obfuscation {
	case "none", "full", "paranoid":
	default:
		return fmt.Errorf("unsupported pesto.obfuscation %q", c.Pesto.Obfuscation)
	}
	if c.Pesto.PAR2Percent < 0 || c.Pesto.PAR2Percent > 100 {
		return errors.New("pesto.par2_percent must be between 0 and 100")
	}
	return nil
}

func pathsOverlap(first, second string) bool {
	first, errFirst := filepath.Abs(first)
	second, errSecond := filepath.Abs(second)
	if errFirst != nil || errSecond != nil {
		return true
	}
	within := func(parent, child string) bool {
		rel, err := filepath.Rel(parent, child)
		return err == nil && (rel == "." || (rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator))))
	}
	return within(first, second) || within(second, first)
}

func (c *Config) PollInterval() time.Duration {
	return time.Duration(c.Worker.PollIntervalSeconds) * time.Second
}

func (c *Config) QBittorrentTimeout() time.Duration {
	return time.Duration(c.QBittorrent.TimeoutSecs) * time.Second
}

func (c *Config) GoNZBTimeout() time.Duration {
	return time.Duration(c.GoNZB.TimeoutSecs) * time.Second
}
