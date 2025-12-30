package config

import (
	"fmt"
	"os"

	"gopkg.in/yaml.v3"
)

type Config struct {
	Server   ServerConfig   `yaml:"server"`
	Download DownloadConfig `yaml:"download"`
}

type ServerConfig struct {
	Host     string `yaml:"host"`
	Port     int    `yaml:"port"`
	Username string `yaml:"username"`
	Password string `yaml:"password"`
	TLS      bool   `yaml:"tls"`
}

type DownloadConfig struct {
	OutDir     string `yaml:"out_dir"`
	MaxWorkers int    `yaml:"max_workers"`
	TempDir    string `yaml:"temp_dir"`
}

func Load(path string) (*Config, error) {
	cfg := &Config{}

	file, err := os.Open(path)
	if err != nil {
		return nil, fmt.Errorf("could not open config file: %w", err)
	}
	defer file.Close()

	d := yaml.NewDecoder(file)
	if err := d.Decode(&cfg); err != nil {
		return nil, fmt.Errorf("could not decode config: %w", err)
	}

	if err := cfg.validate(); err != nil {
		return nil, err
	}

	return cfg, nil
}

func (c *Config) validate() error {
	if c.Server.Host == "" {
		return fmt.Errorf("server host is required")
	}
	if c.Server.TLS && c.Server.Port == 119 {
		fmt.Println("Warning: TLS is enabled but port is set to 119 (standard non-TLS)")
	}

	return nil
}
