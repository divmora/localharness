package main

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/divmora/localharness/adk"
	"gopkg.in/yaml.v3"
)

// globalConfigRelPath is the relative path under $HOME for global Zenith config.
const globalConfigRelPath = ".divmora/agents/zenith/config.yml"

// ── Types ────────────────────────────────────────────────────────────────────

// ZenithConfig is the fully resolved configuration for a Zenith run.
// All fields are optional — empty means "use default".
type ZenithConfig struct {
	Endpoint      string `yaml:"endpoint"`       // LiteLLM endpoint name
	Model         string `yaml:"model"`          // Model name override
	BaseURL       string `yaml:"base_url"`       // Base URL override
	APIKey        string `yaml:"api_key"`        // API key override
	ThinkingLevel string `yaml:"thinking_level"` // off, low, medium, high (default: medium)
	Workspace     string `yaml:"workspace"`      // Workspace directory

	// Personas holds per-persona overrides. Each persona inherits the global
	// settings above and can override endpoint, model, base_url, api_key, and thinking_level.
	Personas map[string]PersonaConfig `yaml:"personas"`
}

// PersonaConfig holds per-persona configuration overrides.
type PersonaConfig struct {
	Endpoint      string `yaml:"endpoint"`
	Model         string `yaml:"model"`
	BaseURL       string `yaml:"base_url"`
	APIKey        string `yaml:"api_key"`
	ThinkingLevel string `yaml:"thinking_level"`
}

// CLIOverrides holds values explicitly set via CLI flags.
// Empty string means "not set" (don't override).
type CLIOverrides struct {
	Endpoint   string
	Model      string
	BaseURL    string
	APIKey     string
	Workspace  string
	ConfigFile string
}

// ── Core Methods ─────────────────────────────────────────────────────────────

// ForPersona returns a copy with persona-specific overrides applied.
// If the persona has no overrides, the global config is returned unchanged.
func (z ZenithConfig) ForPersona(name string) ZenithConfig {
	pc, ok := z.Personas[name]
	if !ok {
		return z
	}
	mergeLiteLLMFields(&z.Endpoint, &z.Model, &z.BaseURL, &z.APIKey, &z.ThinkingLevel, pc.Endpoint, pc.Model, pc.BaseURL, pc.APIKey, pc.ThinkingLevel)
	return z
}

// Validate checks the config for invalid values and returns all errors found.
func (z *ZenithConfig) Validate() error {
	// LiteLLM handles unknown endpoints; no specific validation needed here anymore.
	return nil
}

// ApplyTo configures the SDK agent config from the resolved Zenith config.
func (z *ZenithConfig) ApplyTo(cfg *adk.LocalAgentConfig) error {
	if z.Endpoint != "" {
		cfg.LitellmEndpoint = z.Endpoint
	}
	if z.Model != "" {
		cfg.LitellmModel = z.Model
	}
	if z.BaseURL != "" {
		cfg.LitellmBaseURL = z.BaseURL
	}
	if z.APIKey != "" {
		cfg.LitellmAPIKey = z.APIKey
	}
	if z.Workspace != "" {
		cfg.Workspaces = []adk.WorkspaceDef{{Directory: z.Workspace}}
	}
	return nil
}

// String returns a human-readable summary of the resolved config.
func (z ZenithConfig) String() string {
	var b strings.Builder
	b.WriteString("Resolved Zenith Config:\n")
	writeField(&b, "endpoint", z.Endpoint, "(auto-detect)")
	writeField(&b, "model", z.Model, "(endpoint default)")
	writeField(&b, "base_url", z.BaseURL, "(none)")
	writeField(&b, "api_key", z.APIKey, "(none)")
	writeField(&b, "workspace", z.Workspace, "(cwd)")

	if len(z.Personas) > 0 {
		b.WriteString("\n  personas:\n")
		for name, pc := range z.Personas {
			b.WriteString(fmt.Sprintf("    %s:\n", name))
			if pc.Endpoint != "" {
				b.WriteString(fmt.Sprintf("      endpoint: %s\n", pc.Endpoint))
			}
			if pc.Model != "" {
				b.WriteString(fmt.Sprintf("      model: %s\n", pc.Model))
			}
			if pc.BaseURL != "" {
				b.WriteString(fmt.Sprintf("      base_url: %s\n", pc.BaseURL))
			}
			if pc.APIKey != "" {
				b.WriteString(fmt.Sprintf("      api_key: %s\n", pc.APIKey))
			}
		}
	}
	return b.String()
}

// ── Resolution ───────────────────────────────────────────────────────────────

// ResolveConfig builds a fully resolved ZenithConfig by merging sources.
//
// Priority (highest wins):
//
//	CLI flags > --config file > .zenith/config.yml (workspace) > ~/.divmora/agents/zenith/config.yml (global)
func ResolveConfig(cli CLIOverrides) ZenithConfig {
	var cfg ZenithConfig

	// 1. Global config — lowest priority
	if home, err := os.UserHomeDir(); err == nil {
		globalPath := filepath.Join(home, globalConfigRelPath)
		if c, err := readConfigFile(globalPath); err == nil {
			cfg = c
		}
	}

	// 2. Workspace config (.zenith/config.yml) — overrides global
	//    Use CLI workspace first, then config workspace, then cwd
	wsDir := cli.Workspace
	if wsDir == "" {
		wsDir = cfg.Workspace // from global config
	}
	if wsDir == "" {
		wsDir, _ = os.Getwd()
	}
	if wsDir != "" {
		wsPath := filepath.Join(wsDir, ".zenith", "config.yml")
		if c, err := readConfigFile(wsPath); err == nil {
			mergeConfig(&cfg, &c)
		}
	}

	// 3. Explicit --config file — overrides workspace config
	if cli.ConfigFile != "" {
		if c, err := readConfigFile(cli.ConfigFile); err == nil {
			mergeConfig(&cfg, &c)
		} else {
			fmt.Fprintf(os.Stderr, "⚠️  Warning: could not read config file %s: %v\n", cli.ConfigFile, err)
		}
	}

	// 4. CLI flags — highest priority, override everything
	mergeLiteLLMFields(&cfg.Endpoint, &cfg.Model, &cfg.BaseURL, &cfg.APIKey, &cfg.ThinkingLevel, cli.Endpoint, cli.Model, cli.BaseURL, cli.APIKey, "")
	if cli.Workspace != "" {
		cfg.Workspace = cli.Workspace
	}

	return cfg
}

// ── Internal Helpers ─────────────────────────────────────────────────────────

// readConfigFile reads and parses a YAML config file.
func readConfigFile(path string) (ZenithConfig, error) {
	var cfg ZenithConfig
	data, err := os.ReadFile(path)
	if err != nil {
		return cfg, fmt.Errorf("read %s: %w", path, err)
	}
	if err := yaml.Unmarshal(data, &cfg); err != nil {
		return cfg, fmt.Errorf("parse %s: %w", path, err)
	}
	return cfg, nil
}

// mergeConfig merges src into dst, overriding only non-empty fields.
func mergeConfig(dst, src *ZenithConfig) {
	mergeLiteLLMFields(&dst.Endpoint, &dst.Model, &dst.BaseURL, &dst.APIKey, &dst.ThinkingLevel, src.Endpoint, src.Model, src.BaseURL, src.APIKey, src.ThinkingLevel)
	if src.Workspace != "" {
		dst.Workspace = src.Workspace
	}
	// Merge per-persona configs
	for name, pc := range src.Personas {
		if dst.Personas == nil {
			dst.Personas = make(map[string]PersonaConfig)
		}
		existing := dst.Personas[name]
		mergeLiteLLMFields(&existing.Endpoint, &existing.Model, &existing.BaseURL, &existing.APIKey, &existing.ThinkingLevel, pc.Endpoint, pc.Model, pc.BaseURL, pc.APIKey, pc.ThinkingLevel)
		dst.Personas[name] = existing
	}
}

// mergeLiteLLMFields is the single merge function for the config fields.
// It sets dst fields from src values, but only if the src value is non-empty.
func mergeLiteLLMFields(dstEndpoint, dstModel, dstBaseURL, dstAPIKey, dstThinkingLevel *string, srcEndpoint, srcModel, srcBaseURL, srcAPIKey, srcThinkingLevel string) {
	if srcEndpoint != "" {
		*dstEndpoint = srcEndpoint
	}
	if srcModel != "" {
		*dstModel = srcModel
	}
	if srcBaseURL != "" {
		*dstBaseURL = srcBaseURL
	}
	if srcAPIKey != "" {
		*dstAPIKey = srcAPIKey
	}
	if srcThinkingLevel != "" {
		*dstThinkingLevel = srcThinkingLevel
	}
}

// writeField writes a key-value line to the builder, showing a fallback label if empty.
func writeField(b *strings.Builder, key, value, fallback string) {
	if value != "" {
		b.WriteString(fmt.Sprintf("  %s: %s\n", key, value))
	} else {
		b.WriteString(fmt.Sprintf("  %s: %s\n", key, fallback))
	}
}
