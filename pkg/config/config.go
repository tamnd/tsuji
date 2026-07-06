// Package config loads tsuji server configuration from a TOML file,
// environment variables, and defaults, in that order of precedence
// (env over file over defaults).
package config

import (
	"os"
	"path/filepath"
)

// Config holds everything the server needs to start.
type Config struct {
	// Addr is the listen address for the HTTP server.
	Addr string
	// DBPath is the sqlite database file.
	DBPath string
	// Providers maps provider slug to its upstream credentials.
	Providers map[string]Provider
}

// Provider is one upstream the gateway can route to.
type Provider struct {
	// APIKey is the shared upstream key, if the operator holds one.
	APIKey string
	// BaseURL overrides the default upstream endpoint.
	BaseURL string
}

// Load builds a Config from defaults and environment variables.
// A config file loader lands with the provider registry milestone.
func Load() (*Config, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return nil, err
	}
	cfg := &Config{
		Addr:      ":4780",
		DBPath:    filepath.Join(home, ".tsuji", "tsuji.db"),
		Providers: map[string]Provider{},
	}
	if v := os.Getenv("TSUJI_ADDR"); v != "" {
		cfg.Addr = v
	}
	if v := os.Getenv("TSUJI_DB"); v != "" {
		cfg.DBPath = v
	}
	return cfg, nil
}
