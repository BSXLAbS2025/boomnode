package config

import (
	"fmt"
	"os"

	"gopkg.in/yaml.v3"
)

type Config struct {
	Node    NodeConfig    `yaml:"node"`
	Storage StorageConfig `yaml:"storage"`
}

type NodeConfig struct {
	Name string `yaml:"name"`
	Geo  string `yaml:"geo"` // RU, DE, ON и т.д.
}

type StorageConfig struct {
	DataDir string `yaml:"data_dir"`
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
