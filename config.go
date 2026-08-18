package main

import (
	"fmt"
	"os"

	"gopkg.in/yaml.v3"
)

type ModelConfig struct {
	Provider           string `yaml:"provider"`
	Model              string `yaml:"model"`
	PriceInPerMillion  int64  `yaml:"price_in_per_million"`
	PriceOutPerMillion int64  `yaml:"price_out_per_million"`
}

type Config struct {
	Port   int                    `yaml:"port"`
	Models map[string]ModelConfig `yaml:"models"`
}

func loadConfig(path string) (Config, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return Config{}, fmt.Errorf("read config: %w", err)
	}

	var cfg Config
	if err := yaml.Unmarshal(data, &cfg); err != nil {
		return Config{}, fmt.Errorf("parse config: %w", err)
	}

	if v := os.Getenv("SANMON_PORT"); v != "" {
		var port int
		if _, err := fmt.Sscanf(v, "%d", &port); err != nil {
			return Config{}, fmt.Errorf("parse SANMON_PORT: %w", err)
		}
		cfg.Port = port
	}

	return cfg, nil
}
