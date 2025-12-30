package config

import (
	"errors"
	"fmt"
	"os"

	"gopkg.in/yaml.v3"
)

type Config struct {
	Servers  []ServerConfig `yaml:"servers"`
	Download DownloadConfig `yaml:"download"`
}

type ServerConfig struct {
	ID            string `yaml:"id"`
	Host          string `yaml:"host"`
	Port          int    `yaml:"port"`
	Username      string `yaml:"username"`
	Password      string `yaml:"password"`
	TLS           bool   `yaml:"tls"`
	MaxConnection int    `yaml:"max_connections"`
	Priority      int    `yaml:"priority"`
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
