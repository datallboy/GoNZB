package config

import (
	"errors"
	"fmt"
	"os"
	"strings"

	"github.com/spf13/viper"
)

type Config struct {
	Servers  []ServerConfig  `mapstructure:"servers" yaml:"servers"`
	Indexers []IndexerConfig `mapstructure:"indexers" yaml:"indexers"`
	Download DownloadConfig  `mapstructure:"download" yaml:"download"`
	Log      LogConfig       `mapstructure:"log" yaml:"log"`
	Store    StoreConfig     `mapstructure:"store" yaml:"store"`

	Port string `mapstructure:"port" yaml:"port"`
}

type ServerConfig struct {
	ID            string `mapstructure:"id" yaml:"id"`
	Host          string `mapstructure:"host" yaml:"host"`
	Port          int    `mapstructure:"port" yaml:"port"`
	Username      string `mapstructure:"username" yaml:"username"`
	Password      string `mapstructure:"password" yaml:"password"`
	TLS           bool   `mapstructure:"tls" yaml:"tls"`
	MaxConnection int    `mapstructure:"max_connections" yaml:"max_connections"`
	Priority      int    `mapstructure:"priority" yaml:"priority"`
}

type IndexerConfig struct {
	ID       string `mapstructure:"id" yaml:"id"`
	BaseUrl  string `mapstructure:"base_url" yaml:"base_url"`
	ApiKey   string `mapstructure:"api_key" yaml:"api_key"`
	Redirect bool   `mapstructure:"redirect" yaml:"redirect"`
}

type DownloadConfig struct {
	OutDir            string   `mapstructure:"out_dir" yaml:"out_dir"`
	CompletedDir      string   `mapstructure:"completed_dir" yaml:"completed_dir"`
	CleanupExtensions []string `mapstructure:"cleanup_extensions" yaml:"cleanup_extensions"`
}

type LogConfig struct {
	Path          string `mapstructure:"path" yaml:"path"`
	Level         string `mapstructure:"level" yaml:"level"`
	IncludeStdout bool   `mapstructure:"include_stdout" yaml:"include_stdout"`
}

type StoreConfig struct {
	SQLitePath string `mapstructure:"sqlite_path" yaml:"sqlite_path"`
	BlobDir    string `mapstructure:"blob_dir" yaml:"blob_dir"`
}

func Load(path string) (*Config, error) {

	if path == "" {
		path = "config.yaml"
	}

	// 1. Check if the file exists
	if _, err := os.Stat(path); os.IsNotExist(err) {
		// FALLBACK: If we are in Docker (or similar) and didn't provide a flag, check /config/config.yaml
		if path == "config.yaml" {
			if _, errEx := os.Stat("/config/config.yaml"); errEx == nil {
				path = "/config/config.yaml"
			} else if _, errEx := os.Stat("config.yaml.example"); errEx == nil {
				// If config.yaml is missing but example exists, give a helpful error
				return nil, fmt.Errorf("configuration file 'config.yaml' not found\n\n" +
					"To fix this, run:\n" +
					"  cp config.yaml.example config.yaml\n" +
					"Then edit it with your Usenet credentials.")
			} else {
				return nil, fmt.Errorf("config file not found: %s", path)
			}
		} else {
			return nil, fmt.Errorf("config file not found: %s", path)
		}
	}

	v := viper.New()

	// Set Defaults
	v.SetDefault("port", "8080")
	v.SetDefault("download.out_dir", "./downloads")
	v.SetDefault("download.completed_dir", "./downloads/completed")
	v.SetDefault("download.cleanup_extensions", []string{"nzb", "par2", "sfv", "nfo"}) // sane default for completed cleanup
	v.SetDefault("log.path", "gonzb.log")
	v.SetDefault("log.level", "info")
	v.SetDefault("log.include_stdout", true)

	// Read config File
	v.SetConfigFile(path)
	v.SetConfigType("yaml")

	if err := v.ReadInConfig(); err != nil {
		return nil, fmt.Errorf("error reading config file %s: %w", path, err)
	}

	// Support Environment Variables
	v.SetEnvPrefix("GONZB")
	v.SetEnvKeyReplacer(strings.NewReplacer(".", "_"))
	v.AutomaticEnv()

	var cfg Config
	if err := v.Unmarshal(&cfg); err != nil {
		return nil, err
	}

	if err := cfg.validate(); err != nil {
		return nil, err
	}

	return &cfg, nil
}

func (c *Config) validate() error {
	if len(c.Servers) == 0 {
		return errors.New("at least one server must be configured")
	}

	for i, s := range c.Servers {
		if s.ID == "" {
			return fmt.Errorf("server[%d] requires a unique ID", i)
		}

		if s.Host == "" {
			return fmt.Errorf("server %s: host is required", s.ID)
		}

		if s.Port == 0 {
			return fmt.Errorf("server %s: port is required", s.ID)
		}

		if s.TLS && s.Port == 119 {
			fmt.Println("Warning: TLS is enabled but port is set to 119 (standard non-TLS)")
		}

		if s.MaxConnection <= 0 {
			// Default to a sane value
			c.Servers[i].MaxConnection = 10
		}

		if s.Priority == 0 {
			// Default to same priority
			c.Servers[i].Priority = 1
		}
	}

	if c.Download.OutDir == "" {
		c.Download.OutDir = "./downloads"
	}

	return nil
}
