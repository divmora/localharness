// Package config provides configuration types for the LocalHarness server.
// This file handles global Divmora config from ~/.divmora/config/.

package config

import (
	"encoding/json"
	"log/slog"
	"os"
	"path/filepath"
	"strings"

	pb "github.com/divmora/localharness/gen/go/localharness/v1"
	"github.com/divmora/localharness/internal/errors"
	"github.com/divmora/localharness/internal/util"
)

// DefaultDivmoraConfigDir is the shared config directory for all Divmora products.
const DefaultDivmoraConfigDir = ".divmora/config"

// GlobalSettings represents the shared settings from ~/.divmora/config/settings.json.
type GlobalSettings struct {
	// EnableTelemetry controls whether usage telemetry is sent.
	EnableTelemetry bool `json:"enableTelemetry"`

	// TrustedWorkspaces is a list of directory paths the user has explicitly trusted.
	TrustedWorkspaces []string `json:"trustedWorkspaces"`

	// AllowedCommands is a list of shell commands or command prefixes permanently allowed without confirmation.
	AllowedCommands []string `json:"allowedCommands,omitempty"`

	// AllowedTools is a list of tool names permanently allowed without confirmation.
	AllowedTools []string `json:"allowedTools,omitempty"`
}

// DivmoraConfigDir returns the resolved path to ~/.divmora/config/.
func DivmoraConfigDir() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", errors.Wrap(err, errors.ErrCodeConfiguration,
			"cannot determine home directory").
			WithComponent("config")
	}
	return filepath.Join(home, DefaultDivmoraConfigDir), nil
}

// LoadGlobalSettings reads ~/.divmora/config/settings.json.
// Returns default settings if the file doesn't exist.
func LoadGlobalSettings(logger *slog.Logger) *GlobalSettings {
	configDir, err := DivmoraConfigDir()
	if err != nil {
		logger.Warn("cannot resolve divmora config dir", "error", err)
		return &GlobalSettings{}
	}
	return LoadGlobalSettingsFrom(filepath.Join(configDir, "settings.json"), logger)
}

// LoadGlobalSettingsFrom reads settings from a specific path.
// Returns default settings if the file doesn't exist.
func LoadGlobalSettingsFrom(path string, logger *slog.Logger) *GlobalSettings {
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			logger.Debug("no global settings file found", "path", path)
		} else {
			logger.Warn("failed to read global settings", "path", path, "error", err)
		}
		return &GlobalSettings{}
	}

	var settings GlobalSettings
	if err := json.Unmarshal(data, &settings); err != nil {
		logger.Warn("failed to parse global settings", "path", path, "error", err)
		return &GlobalSettings{}
	}

	logger.Info("loaded global settings",
		"path", path,
		"telemetry", settings.EnableTelemetry,
		"trusted_workspaces", len(settings.TrustedWorkspaces),
	)
	return &settings
}

// mcpConfigFile is the JSON structure of ~/.divmora/config/mcp_config.json.
type mcpConfigFile struct {
	McpServers map[string]mcpServerEntry `json:"mcpServers"`
}

// mcpServerEntry is a single MCP server in the global config file.
type mcpServerEntry struct {
	// Stdio transport
	Command string   `json:"command,omitempty"`
	Args    []string `json:"args,omitempty"`

	// HTTP/SSE transport
	URL     string            `json:"url,omitempty"`
	Headers map[string]string `json:"headers,omitempty"`

	// Common
	Env          map[string]string `json:"env,omitempty"`
	EnabledTools []string          `json:"enabledTools,omitempty"`
}

// LoadGlobalMcpConfig reads ~/.divmora/config/mcp_config.json.
// Returns an empty slice if the file doesn't exist (not an error).
// Supports ${ENV_VAR} expansion in string values.
func LoadGlobalMcpConfig(logger *slog.Logger) []*pb.McpServerConfig {
	configDir, err := DivmoraConfigDir()
	if err != nil {
		logger.Warn("cannot resolve divmora config dir", "error", err)
		return nil
	}
	return LoadGlobalMcpConfigFrom(filepath.Join(configDir, "mcp_config.json"), logger)
}

// LoadGlobalMcpConfigFrom reads MCP config from a specific path.
// Returns an empty slice if the file doesn't exist.
func LoadGlobalMcpConfigFrom(path string, logger *slog.Logger) []*pb.McpServerConfig {
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			logger.Debug("no global MCP config file found", "path", path)
		} else {
			logger.Warn("failed to read global MCP config", "path", path, "error", err)
		}
		return nil
	}

	var cfg mcpConfigFile
	if err := json.Unmarshal(data, &cfg); err != nil {
		logger.Warn("failed to parse global MCP config", "path", path, "error", err)
		return nil
	}

	var servers []*pb.McpServerConfig
	for name, entry := range cfg.McpServers {
		server := &pb.McpServerConfig{
			Name:         name,
			EnabledTools: entry.EnabledTools,
			Env:          expandEnvMap(entry.Env),
		}

		if entry.Command != "" {
			// Stdio transport
			server.Transport = &pb.McpServerConfig_Stdio{
				Stdio: &pb.McpStdioTransport{
					Command: expandEnvString(entry.Command),
					Args:    expandEnvSlice(entry.Args),
				},
			}
		} else if entry.URL != "" {
			// HTTP/SSE transport
			server.Transport = &pb.McpServerConfig_Http{
				Http: &pb.McpHttpTransport{
					Url:     expandEnvString(entry.URL),
					Headers: expandEnvMap(entry.Headers),
				},
			}
		} else {
			logger.Warn("MCP server has no transport configured, skipping", "name", name)
			continue
		}

		servers = append(servers, server)
		logger.Info("loaded global MCP server", "name", name)
	}

	return servers
}

// MergeMcpConfigs merges global and agent-level MCP configs.
// Agent-level configs override global configs by server name.
func MergeMcpConfigs(global, agent []*pb.McpServerConfig) []*pb.McpServerConfig {
	if len(global) == 0 {
		return agent
	}
	if len(agent) == 0 {
		return global
	}

	// Build agent name set for override detection
	agentNames := make(map[string]bool, len(agent))
	for _, s := range agent {
		agentNames[s.Name] = true
	}

	// Start with global servers not overridden by agent
	var merged []*pb.McpServerConfig
	for _, s := range global {
		if !agentNames[s.Name] {
			merged = append(merged, s)
		}
	}

	// Append all agent-level servers
	merged = append(merged, agent...)
	return merged
}

// expandEnvString replaces ${VAR} and $VAR patterns in a string with
// their environment variable values.
func expandEnvString(s string) string {
	return os.Expand(s, func(key string) string {
		if val, ok := os.LookupEnv(key); ok {
			return val
		}
		return "${" + key + "}" // Preserve unresolved vars
	})
}

// expandEnvSlice expands environment variables in each string.
func expandEnvSlice(ss []string) []string {
	if ss == nil {
		return nil
	}
	out := make([]string, len(ss))
	for i, s := range ss {
		out[i] = expandEnvString(s)
	}
	return out
}

// expandEnvMap expands environment variables in map values.
func expandEnvMap(m map[string]string) map[string]string {
	if m == nil {
		return nil
	}
	out := make(map[string]string, len(m))
	for k, v := range m {
		out[k] = expandEnvString(v)
	}
	return out
}

// IsWorkspaceTrusted checks if a workspace directory is in the trusted list.
func (s *GlobalSettings) IsWorkspaceTrusted(dir string) bool {
	absDir, err := filepath.Abs(dir)
	if err != nil {
		return false
	}
	for _, trusted := range s.TrustedWorkspaces {
		absTrusted, err := filepath.Abs(trusted)
		if err != nil {
			continue
		}
		if absDir == absTrusted || strings.HasPrefix(absDir, absTrusted+string(filepath.Separator)) {
			return true
		}
	}
	return false
}

// IsCommandAllowed checks if a shell command matches allowed patterns,
// correctly verifying all sub-commands in chains (&&, ||, ;, |, &).
func (s *GlobalSettings) IsCommandAllowed(cmd string) bool {
	return util.IsCommandAllowedAgainstRules(cmd, s.AllowedCommands)
}

// IsToolAllowed checks if a tool is in the globally allowed tools list.
func (s *GlobalSettings) IsToolAllowed(toolName string) bool {
	trimmed := strings.TrimSpace(toolName)
	if trimmed == "" {
		return false
	}
	for _, allowed := range s.AllowedTools {
		a := strings.TrimSpace(allowed)
		if a == "*" || a == trimmed {
			return true
		}
	}
	return false
}

// AddAllowedCommand adds a command or prefix pattern to the globally allowed list and saves.
func AddAllowedCommand(cmd string, logger *slog.Logger) error {
	trimmed := strings.TrimSpace(cmd)
	if trimmed == "" {
		return nil
	}

	settings := LoadGlobalSettings(logger)
	for _, a := range settings.AllowedCommands {
		if a == trimmed {
			return nil
		}
	}

	settings.AllowedCommands = append(settings.AllowedCommands, trimmed)
	return SaveGlobalSettings(settings, logger)
}

// AddAllowedTool adds a tool name to the globally allowed list and saves.
func AddAllowedTool(toolName string, logger *slog.Logger) error {
	trimmed := strings.TrimSpace(toolName)
	if trimmed == "" {
		return nil
	}

	settings := LoadGlobalSettings(logger)
	for _, a := range settings.AllowedTools {
		if a == trimmed {
			return nil
		}
	}

	settings.AllowedTools = append(settings.AllowedTools, trimmed)
	return SaveGlobalSettings(settings, logger)
}

// AddTrustedWorkspace adds a directory to the trusted list and saves to settings.json.
func AddTrustedWorkspace(dir string, logger *slog.Logger) error {
	absDir, err := filepath.Abs(dir)
	if err != nil {
		return errors.Wrap(err, errors.ErrCodeConfiguration, "invalid workspace path")
	}

	settings := LoadGlobalSettings(logger)
	if settings.IsWorkspaceTrusted(absDir) {
		return nil
	}

	settings.TrustedWorkspaces = append(settings.TrustedWorkspaces, absDir)
	return SaveGlobalSettings(settings, logger)
}

// RemoveTrustedWorkspace removes a directory from the trusted list and saves to settings.json.
func RemoveTrustedWorkspace(dir string, logger *slog.Logger) error {
	absDir, err := filepath.Abs(dir)
	if err != nil {
		return errors.Wrap(err, errors.ErrCodeConfiguration, "invalid workspace path")
	}

	settings := LoadGlobalSettings(logger)
	var updated []string
	for _, t := range settings.TrustedWorkspaces {
		absT, err := filepath.Abs(t)
		if err == nil && absT != absDir {
			updated = append(updated, t)
		}
	}
	settings.TrustedWorkspaces = updated
	return SaveGlobalSettings(settings, logger)
}

// SaveGlobalSettings writes settings to ~/.divmora/config/settings.json.
func SaveGlobalSettings(settings *GlobalSettings, logger *slog.Logger) error {
	configDir, err := DivmoraConfigDir()
	if err != nil {
		return errors.Wrap(err, errors.ErrCodeConfiguration, "cannot resolve config dir")
	}

	if err := os.MkdirAll(configDir, 0755); err != nil {
		return errors.Wrap(err, errors.ErrCodeConfiguration, "cannot create config dir")
	}

	filePath := filepath.Join(configDir, "settings.json")
	data, err := json.MarshalIndent(settings, "", "  ")
	if err != nil {
		return errors.Wrap(err, errors.ErrCodeConfiguration, "cannot marshal settings")
	}

	if err := os.WriteFile(filePath, data, 0644); err != nil {
		return errors.Wrap(err, errors.ErrCodeConfiguration, "cannot write settings file")
	}

	if logger != nil {
		logger.Info("saved global settings", "path", filePath, "trusted_workspaces", len(settings.TrustedWorkspaces))
	}
	return nil
}


// LiteLLMEndpoint represents a single LiteLLM server configuration.
type LiteLLMEndpoint struct {
BaseURL      string `json:"baseUrl"`
APIKey       string `json:"apiKey"`
DefaultModel string `json:"defaultModel"`
}

// GlobalLiteLLMConfig represents the ~/.divmora/config/litellm.json file.
type GlobalLiteLLMConfig struct {
Endpoints       map[string]LiteLLMEndpoint `json:"endpoints"`
DefaultEndpoint string                     `json:"defaultEndpoint"`
}

// LoadGlobalLiteLLMConfig reads ~/.divmora/config/litellm.json.
// Returns an empty config if the file doesn't exist.
func LoadGlobalLiteLLMConfig(logger *slog.Logger) *GlobalLiteLLMConfig {
configDir, err := DivmoraConfigDir()
if err != nil {
logger.Warn("cannot resolve divmora config dir", "error", err)
return &GlobalLiteLLMConfig{}
}
return LoadGlobalLiteLLMConfigFrom(filepath.Join(configDir, "litellm.json"), logger)
}

// LoadGlobalLiteLLMConfigFrom reads LiteLLM config from a specific path.
func LoadGlobalLiteLLMConfigFrom(path string, logger *slog.Logger) *GlobalLiteLLMConfig {
data, err := os.ReadFile(path)
if err != nil {
if os.IsNotExist(err) {
logger.Debug("no global litellm config file found", "path", path)
} else {
logger.Warn("failed to read global litellm config", "path", path, "error", err)
}
return &GlobalLiteLLMConfig{}
}

var cfg GlobalLiteLLMConfig
if err := json.Unmarshal(data, &cfg); err != nil {
logger.Warn("failed to parse global litellm config", "path", path, "error", err)
return &GlobalLiteLLMConfig{}
}

// Expand environment variables
cfg.DefaultEndpoint = expandEnvString(cfg.DefaultEndpoint)
for name, endpoint := range cfg.Endpoints {
cfg.Endpoints[name] = LiteLLMEndpoint{
BaseURL:      expandEnvString(endpoint.BaseURL),
APIKey:       expandEnvString(endpoint.APIKey),
DefaultModel: expandEnvString(endpoint.DefaultModel),
}
}

logger.Info("loaded global litellm config",
"path", path,
"endpoints", len(cfg.Endpoints),
"default", cfg.DefaultEndpoint,
)
return &cfg
}
