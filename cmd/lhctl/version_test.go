package main

import (
	"bytes"
	"encoding/json"
	"runtime"
	"strings"
	"testing"

	"github.com/divmora/localharness/internal/config"
)

func TestFormatVersion(t *testing.T) {
	tests := []struct {
		input    string
		expected string
	}{
		{"0.3.1", "v0.3.1"},
		{"v0.3.1", "v0.3.1"},
		{"", "v0.0.0-dev"},
		{"  0.1.0-beta  ", "v0.1.0-beta"},
		{"v1.0.0-dirty", "v1.0.0-dirty"},
	}

	for _, tc := range tests {
		got := formatVersion(tc.input)
		if got != tc.expected {
			t.Errorf("formatVersion(%q) = %q, want %q", tc.input, got, tc.expected)
		}
	}
}

func TestVersionCommand_Default(t *testing.T) {
	cmd := newRootCommand()
	buf := new(bytes.Buffer)
	cmd.SetOut(buf)
	cmd.SetErr(buf)
	cmd.SetArgs([]string{"version"})

	if err := cmd.Execute(); err != nil {
		t.Fatalf("version command failed: %v", err)
	}

	out := buf.String()
	expectedVer := formatVersion(config.HarnessVersion)
	if !strings.Contains(out, "lhctl version "+expectedVer) {
		t.Errorf("expected version output to contain %q, got %q", expectedVer, out)
	}
	if !strings.Contains(out, runtime.GOOS) || !strings.Contains(out, runtime.GOARCH) {
		t.Errorf("expected version output to contain OS/Arch, got %q", out)
	}
	if !strings.Contains(out, "LocalHarness daemon:") {
		t.Errorf("expected version output to contain daemon status, got %q", out)
	}
}

func TestVersionCommand_Short(t *testing.T) {
	for _, flag := range []string{"--short", "-s"} {
		cmd := newRootCommand()
		buf := new(bytes.Buffer)
		cmd.SetOut(buf)
		cmd.SetErr(buf)
		cmd.SetArgs([]string{"version", flag})

		if err := cmd.Execute(); err != nil {
			t.Fatalf("version %s command failed: %v", flag, err)
		}

		out := strings.TrimSpace(buf.String())
		expectedVer := formatVersion(config.HarnessVersion)
		if out != expectedVer {
			t.Errorf("version %s expected %q, got %q", flag, expectedVer, out)
		}
	}
}

func TestVersionCommand_JSON(t *testing.T) {
	cmd := newRootCommand()
	buf := new(bytes.Buffer)
	cmd.SetOut(buf)
	cmd.SetErr(buf)
	cmd.SetArgs([]string{"version", "--json"})

	if err := cmd.Execute(); err != nil {
		t.Fatalf("version --json command failed: %v", err)
	}

	var parsed VersionOutput
	if err := json.Unmarshal(buf.Bytes(), &parsed); err != nil {
		t.Fatalf("failed to parse version json: %v, raw output: %s", err, buf.String())
	}

	expectedVer := formatVersion(config.HarnessVersion)
	if parsed.Client.Version != expectedVer {
		t.Errorf("expected parsed.Client.Version %q, got %q", expectedVer, parsed.Client.Version)
	}
	if parsed.Client.OS != runtime.GOOS {
		t.Errorf("expected parsed.Client.OS %q, got %q", runtime.GOOS, parsed.Client.OS)
	}
	if parsed.Client.Arch != runtime.GOARCH {
		t.Errorf("expected parsed.Client.Arch %q, got %q", runtime.GOARCH, parsed.Client.Arch)
	}
	if parsed.Daemon == nil {
		t.Errorf("expected Daemon info object in JSON output")
	}
}

func TestVersionFlag(t *testing.T) {
	cmd := newRootCommand()
	buf := new(bytes.Buffer)
	cmd.SetOut(buf)
	cmd.SetErr(buf)
	cmd.SetArgs([]string{"--version"})

	if err := cmd.Execute(); err != nil {
		t.Fatalf("--version flag failed: %v", err)
	}

	out := buf.String()
	expectedVer := formatVersion(config.HarnessVersion)
	if !strings.Contains(out, "lhctl version "+expectedVer) {
		t.Errorf("expected output to contain 'lhctl version %s', got %q", expectedVer, out)
	}
}
