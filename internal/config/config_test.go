package config

import (
	"strings"
	"testing"
)

func TestDefaultBuiltinTools(t *testing.T) {
	cfg := DefaultBuiltinTools()
	if cfg == nil {
		t.Fatal("DefaultBuiltinTools returned nil")
	}

	// All tools should be enabled except RunCommand
	if !cfg.ViewFile {
		t.Error("ViewFile should be enabled by default")
	}
	if !cfg.CreateFile {
		t.Error("CreateFile should be enabled by default")
	}
	if !cfg.EditFile {
		t.Error("EditFile should be enabled by default")
	}
	if !cfg.ListDir {
		t.Error("ListDir should be enabled by default")
	}
	if !cfg.SearchDir {
		t.Error("SearchDir should be enabled by default")
	}
	if !cfg.FindFile {
		t.Error("FindFile should be enabled by default")
	}
	if !cfg.Finish {
		t.Error("Finish should be enabled by default")
	}

	// RunCommand should be disabled by default for safety
	if cfg.RunCommand {
		t.Error("RunCommand should be DISABLED by default")
	}
}

func TestHarnessVersion(t *testing.T) {
	if HarnessVersion == "" {
		t.Error("HarnessVersion should not be empty")
	}
	// Should be a semver-like string (MAJOR.MINOR.PATCH)
	parts := strings.Split(HarnessVersion, ".")
	if len(parts) != 3 {
		t.Errorf("HarnessVersion should be MAJOR.MINOR.PATCH, got: %s", HarnessVersion)
	}
}

func TestDefaultAppDataDir(t *testing.T) {
	if DefaultAppDataDir == "" {
		t.Error("DefaultAppDataDir should not be empty")
	}
	if DefaultAppDataDir != ".divmora/localharness" {
		t.Errorf("unexpected default app data dir: %s", DefaultAppDataDir)
	}
}

func TestServerConfigDefaults(t *testing.T) {
	// Verify the struct can be zero-initialized
	cfg := &ServerConfig{}
	if cfg.APIKey != "" {
		t.Error("zero-value APIKey should be empty")
	}
	if cfg.Debug {
		t.Error("zero-value Debug should be false")
	}
	if cfg.Version {
		t.Error("zero-value Version should be false")
	}
}
