package config

import (
	"os"
	"path/filepath"

	"gopkg.in/yaml.v3"
)

// Config holds all iWorkerCenter configuration.
type Config struct {
	Server struct {
		Host string `yaml:"host"`
		Port int    `yaml:"port"`
	} `yaml:"server"`

	Database struct {
		DSN string `yaml:"dsn"`
	} `yaml:"database"`

	Cloud struct {
		BaseURL string `yaml:"base_url"`
	} `yaml:"cloud"`

	Logging struct {
		Level string `yaml:"level"`
	} `yaml:"logging"`
}

// Default returns a Config with sensible defaults.
func Default() *Config {
	cfg := &Config{}
	cfg.Server.Host = "0.0.0.0"
	cfg.Server.Port = 9377
	cfg.Database.DSN = "" // resolved at runtime from ~/.iworkercenter/center.db
	cfg.Logging.Level = "info"
	return cfg
}

// Load reads a YAML config file and merges with defaults.
// If path is empty, tries ~/.iworkercenter/config.yaml, then falls back to defaults.
func Load(path string) (*Config, error) {
	cfg := Default()

	if path == "" {
		home, err := os.UserHomeDir()
		if err == nil {
			path = filepath.Join(home, ".iworkercenter", "config.yaml")
		}
	}

	if path != "" {
		data, err := os.ReadFile(path)
		if err != nil {
			if os.IsNotExist(err) {
				return cfg, nil
			}
			return nil, err
		}
		if err := yaml.Unmarshal(data, cfg); err != nil {
			return nil, err
		}
	}

	// Ensure port stays at default if not set
	if cfg.Server.Port == 0 {
		cfg.Server.Port = 9377
	}

	return cfg, nil
}
