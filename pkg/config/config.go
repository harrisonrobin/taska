package config

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
)

const (
	xdgAppName = "taska"
	configFile = "config.json"
)

type Config struct {
	Calendar string `json:"calendar"`
}

func GetConfigPath() (string, error) {
	xdgHome, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(xdgHome, ".config", xdgAppName, configFile), nil
}

func Load() (*Config, error) {
	path, err := GetConfigPath()
	if err != nil {
		return nil, err
	}

	f, err := os.Open(path)
	if err != nil {
		if os.IsNotExist(err) {
			return &Config{Calendar: "Tasks"}, nil // Default
		}
		return nil, err
	}
	defer f.Close()

	var cfg Config
	if err := json.NewDecoder(f).Decode(&cfg); err != nil {
		return nil, fmt.Errorf("failed to decode config: %w", err)
	}
	if cfg.Calendar == "" {
		cfg.Calendar = "Tasks"
	}
	return &cfg, nil
}

func Save(cfg *Config) error {
	path, err := GetConfigPath()
	if err != nil {
		return err
	}

	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0700); err != nil {
		return fmt.Errorf("failed to create config directory: %w", err)
	}

	f, err := os.OpenFile(path, os.O_RDWR|os.O_CREATE|os.O_TRUNC, 0600)
	if err != nil {
		return fmt.Errorf("failed to open config file for writing: %w", err)
	}
	defer f.Close()

	encoder := json.NewEncoder(f)
	encoder.SetIndent("", "  ")
	return encoder.Encode(cfg)
}
