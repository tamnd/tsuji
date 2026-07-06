// Package config loads tsuji server configuration from a TOML file,
// environment variables, and defaults, in that order of precedence
// (env over file over defaults).
package config

import (
	"os"
	"path/filepath"
	"strings"
)

// Config holds everything the server needs to start.
type Config struct {
	// Addr is the listen address for the HTTP server.
	Addr string
	// DBPath is the sqlite database file.
	DBPath string
	// Providers maps provider slug to its upstream credentials.
	Providers map[string]Provider
	// Fusion overrides the built-in fusion preset tiers.
	Fusion map[string]FusionPreset
}

// FusionPreset is one fusion tier: the panel model list plus the judge
// and writer models.
type FusionPreset struct {
	Panel  []string
	Judge  string
	Writer string
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
		Fusion:    map[string]FusionPreset{},
	}
	if v := os.Getenv("TSUJI_ADDR"); v != "" {
		cfg.Addr = v
	}
	if v := os.Getenv("TSUJI_DB"); v != "" {
		cfg.DBPath = v
	}
	// Fusion presets: TSUJI_FUSION_<NAME>_PANEL (comma-separated model
	// ids), _JUDGE, _WRITER, e.g. TSUJI_FUSION_BUDGET_PANEL. Preset names
	// map lower-cased, so BUDGET overrides the budget tier and GENERAL_HIGH
	// becomes general-high.
	for _, kv := range os.Environ() {
		name, val, _ := strings.Cut(kv, "=")
		rest, ok := strings.CutPrefix(name, "TSUJI_FUSION_")
		if !ok || val == "" {
			continue
		}
		preset, field := "", ""
		switch {
		case strings.HasSuffix(rest, "_PANEL"):
			preset, field = strings.TrimSuffix(rest, "_PANEL"), "panel"
		case strings.HasSuffix(rest, "_JUDGE"):
			preset, field = strings.TrimSuffix(rest, "_JUDGE"), "judge"
		case strings.HasSuffix(rest, "_WRITER"):
			preset, field = strings.TrimSuffix(rest, "_WRITER"), "writer"
		default:
			continue
		}
		key := strings.ReplaceAll(strings.ToLower(preset), "_", "-")
		p := cfg.Fusion[key]
		switch field {
		case "panel":
			p.Panel = nil
			for id := range strings.SplitSeq(val, ",") {
				if id = strings.TrimSpace(id); id != "" {
					p.Panel = append(p.Panel, id)
				}
			}
		case "judge":
			p.Judge = val
		case "writer":
			p.Writer = val
		}
		cfg.Fusion[key] = p
	}

	// Provider keys: TSUJI_PROVIDER_<SLUG>_KEY and optional _BASE_URL,
	// e.g. TSUJI_PROVIDER_OPENAI_KEY, TSUJI_PROVIDER_DEEPSEEK_BASE_URL.
	for _, kv := range os.Environ() {
		name, val, _ := strings.Cut(kv, "=")
		rest, ok := strings.CutPrefix(name, "TSUJI_PROVIDER_")
		if !ok || val == "" {
			continue
		}
		slug, field := "", ""
		switch {
		case strings.HasSuffix(rest, "_KEY"):
			slug, field = strings.TrimSuffix(rest, "_KEY"), "key"
		case strings.HasSuffix(rest, "_BASE_URL"):
			slug, field = strings.TrimSuffix(rest, "_BASE_URL"), "base"
		default:
			continue
		}
		p := cfg.Providers[strings.ToLower(slug)]
		if field == "key" {
			p.APIKey = val
		} else {
			p.BaseURL = val
		}
		cfg.Providers[strings.ToLower(slug)] = p
	}
	return cfg, nil
}
