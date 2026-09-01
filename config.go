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
	Port         int                    `yaml:"port"`
	Models       map[string]ModelConfig `yaml:"models"`
	VirtualKey   string                 `yaml:"-"`
	GeminiAPIKey string                 `yaml:"-"`
	AdminToken   string                 `yaml:"-"`
	DatabaseURL  string                 `yaml:"-"`
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

	cfg.VirtualKey = os.Getenv("SANMON_VIRTUAL_KEY")
	if cfg.VirtualKey == "" {
		return Config{}, fmt.Errorf("SANMON_VIRTUAL_KEY wajib diisi")
	}

	cfg.GeminiAPIKey = os.Getenv("GEMINI_API_KEY")
	if cfg.GeminiAPIKey == "" {
		return Config{}, fmt.Errorf("GEMINI_API_KEY wajib diisi")
	}

	cfg.AdminToken = os.Getenv("SANMON_ADMIN_TOKEN")
	if cfg.AdminToken == "" {
		return Config{}, fmt.Errorf("SANMON_ADMIN_TOKEN wajib diisi")
	}

	cfg.DatabaseURL = os.Getenv("SANMON_DATABASE_URL")
	if cfg.DatabaseURL == "" {
		return Config{}, fmt.Errorf("SANMON_DATABASE_URL wajib diisi")
	}

	return cfg, nil
}
