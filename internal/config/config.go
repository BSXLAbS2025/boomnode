package config

import (
	"fmt"
	"os"

	"gopkg.in/yaml.v3"
)

type Config struct {
	Node    NodeConfig    `yaml:"node"`
	Storage StorageConfig `yaml:"storage"`
	Relay   RelayConfig   `yaml:"relay"`
}

type NodeConfig struct {
	Name string `yaml:"name"`
	Geo  string `yaml:"geo"`
	Mode string `yaml:"mode"` // "client" или "relay"
}

type StorageConfig struct {
	DataDir string `yaml:"data_dir"`
}

type RelayConfig struct {
	Enabled     bool     `yaml:"enabled"`
	Whitelist   []string `yaml:"whitelist"`
	MaxStorageMB int     `yaml:"max_storage_mb"`
}

func Load(path string) (*Config, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("не могу прочитать конфиг %s: %w", path, err)
	}
	cfg := &Config{
		Storage: StorageConfig{DataDir: "./data"},
	}
	if err := yaml.Unmarshal(data, cfg); err != nil {
		return nil, fmt.Errorf("не могу распарсить конфиг: %w", err)
	}
	return cfg, nil
}
