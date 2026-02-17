// Package config loads and manages application configuration.
//
// Configuration is loaded from multiple sources with the following precedence:
// flags > environment variables > config file > defaults
package config

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"gopkg.in/yaml.v3"
)

// Config holds all application configuration.
type Config struct {
	DefaultProvider string              `yaml:"default_provider"`
	DefaultModel    string              `yaml:"default_model"`
	Providers       map[string]Provider `yaml:"providers"`
}

// Provider holds provider-specific configuration.
type Provider struct {
	APIKey string `yaml:"api_key"`
}

// DefaultConfig returns the default configuration.
func DefaultConfig() *Config {
	return &Config{
		DefaultProvider: "openai",
		DefaultModel:    "gpt-4o",
		Providers: map[string]Provider{
			"openai":    {},
			"anthropic": {},
		},
	}
}

// Load reads configuration from the config file and environment variables.
// Environment variables take precedence over the config file.
func Load() (*Config, error) {
	cfg := DefaultConfig()

	// Try to load config file
	configPath, err := getConfigPath()
	if err == nil {
		if data, err := os.ReadFile(configPath); err == nil {
			if err := yaml.Unmarshal(data, cfg); err != nil {
				return nil, fmt.Errorf("failed to parse config file %s: %w", configPath, err)
			}
		}
	}

	// Apply environment overrides
	cfg.applyEnvOverrides()

	return cfg, nil
}

// getConfigPath returns the path to the config file.
func getConfigPath() (string, error) {
	configDir, err := os.UserConfigDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(configDir, "ask", "config.yaml"), nil
}

// applyEnvOverrides applies environment variable overrides to the config.
func (c *Config) applyEnvOverrides() {
	// Override default provider
	if v := os.Getenv("ASK_PROVIDER"); v != "" {
		c.DefaultProvider = v
	}

	// Override default model
	if v := os.Getenv("ASK_MODEL"); v != "" {
		c.DefaultModel = v
	}

	// Override API keys
	if v := os.Getenv("OPENAI_API_KEY"); v != "" {
		p := c.Providers["openai"]
		p.APIKey = v
		c.Providers["openai"] = p
	}

	if v := os.Getenv("ANTHROPIC_API_KEY"); v != "" {
		p := c.Providers["anthropic"]
		p.APIKey = v
		c.Providers["anthropic"] = p
	}

	// Resolve environment variable references in config file API keys
	for name, provider := range c.Providers {
		if strings.HasPrefix(provider.APIKey, "${") && strings.HasSuffix(provider.APIKey, "}") {
			envVar := strings.TrimSuffix(strings.TrimPrefix(provider.APIKey, "${"), "}")
			if v := os.Getenv(envVar); v != "" {
				provider.APIKey = v
				c.Providers[name] = provider
			}
		}
	}
}

// GetAPIKey returns the API key for the specified provider.
func (c *Config) GetAPIKey(providerName string) string {
	if p, ok := c.Providers[providerName]; ok {
		return p.APIKey
	}
	return ""
}

// GetDataDir returns the data directory for storing history and other data.
func GetDataDir() (string, error) {
	configDir, err := os.UserConfigDir()
	if err != nil {
		return "", fmt.Errorf("failed to get user config dir: %w", err)
	}

	dataDir := filepath.Join(configDir, "ask")

	if err := os.MkdirAll(dataDir, 0750); err != nil {
		return "", fmt.Errorf("failed to create data dir: %w", err)
	}

	return dataDir, nil
}
