// Package config provides configuration types for the LocalHarness server.
package config

import (
	"flag"
	"fmt"
	"os"
	"path/filepath"

	pb "github.com/divmora/localharness/gen/go/localharness/v1"
)

// ServerConfig holds the server-level configuration from CLI flags
// and the pipe handshake.
type ServerConfig struct {
	Workspace  string
	AppDataDir string // ~/.divmora/localharness/ by default
	Debug      bool
	Version    bool
	APIKey     string // Set during pipe handshake (crypto-random)
}

// HarnessVersion is the current version of the harness binary.
// In development this is "0.0.0-dev". Release builds inject the real version
// via: -ldflags="-X github.com/divmora/localharness/internal/config.HarnessVersion=X.Y.Z"
// The release-please GitHub Action manages version bumps automatically.
var HarnessVersion = "0.0.0-dev"

// DefaultAppDataDir is the default data directory for conversation state,
// brain artifacts, and other persistent data.
const DefaultAppDataDir = ".divmora/localharness"

// DefaultCompactionThreshold is the token count that triggers context compaction.
// ADK clients can override via HarnessConfig.compaction_threshold.
const DefaultCompactionThreshold = 400000

// DefaultKeepRecentMessages is the number of recent messages preserved during compaction.
const DefaultKeepRecentMessages = 10

// ParseFlags parses CLI flags and returns a ServerConfig.
// Note: --port and --host are no longer supported. The binary exclusively
// uses pipe-based handshake (stdin/stdout) for port assignment and auth.
func ParseFlags() *ServerConfig {
	cfg := &ServerConfig{}

	flag.StringVar(&cfg.Workspace, "workspace", "", "Default workspace directory (defaults to cwd)")
	flag.StringVar(&cfg.AppDataDir, "data-dir", "", "Data directory for conversations and brain (defaults to ~/.divmora/localharness/)")
	flag.BoolVar(&cfg.Debug, "debug", false, "Enable debug logging")
	flag.BoolVar(&cfg.Version, "version", false, "Print version and exit")

	flag.Parse()

	// Resolve workspace
	if cfg.Workspace == "" {
		cwd, err := os.Getwd()
		if err != nil {
			fmt.Fprintf(os.Stderr, "error: cannot determine cwd: %v\n", err)
			os.Exit(1)
		}
		cfg.Workspace = cwd
	}
	if abs, err := filepath.Abs(cfg.Workspace); err == nil {
		cfg.Workspace = abs
	}

	// Resolve appDataDir → ~/.divmora/localharness/
	if cfg.AppDataDir == "" {
		home, err := os.UserHomeDir()
		if err != nil {
			fmt.Fprintf(os.Stderr, "error: cannot determine home dir: %v\n", err)
			os.Exit(1)
		}
		cfg.AppDataDir = filepath.Join(home, DefaultAppDataDir)
	}
	if abs, err := filepath.Abs(cfg.AppDataDir); err == nil {
		cfg.AppDataDir = abs
	}

	return cfg
}

// DefaultBuiltinTools returns the default BuiltinToolsConfig.
// All tools enabled except run_command (for safety).
func DefaultBuiltinTools() *pb.BuiltinToolsConfig {
	return &pb.BuiltinToolsConfig{
		ViewFile:   true,
		CreateFile: true,
		EditFile:   true,
		ListDir:    true,
		SearchDir:  true,
		FindFile:   true,
		RunCommand: false, // Denied by default for safety
		Finish:     true,
		WebSearch:  true,
		WebFetch:   true,
	}
}
