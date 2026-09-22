package main

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/divmora/localharness/internal/config"
)

func setupTestLiteLLMConfig(t *testing.T) (string, func()) {
	t.Helper()
	dir := t.TempDir()
	origHome := os.Getenv("HOME")
	t.Setenv("HOME", dir)

	configDir := filepath.Join(dir, ".divmora", "config")
	_ = os.MkdirAll(configDir, 0755)

	return configDir, func() {
		_ = os.Setenv("HOME", origHome)
	}
}

func TestLiteLLMCommands_CRUD(t *testing.T) {
	configDir, cleanup := setupTestLiteLLMConfig(t)
	defer cleanup()

	// Mock HTTP server for health verification
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if r.URL.Path == "/models" {
			resp := map[string]interface{}{
				"object": "list",
				"data": []map[string]interface{}{
					{"id": "gpt-4o"},
					{"id": "claude-3-5-sonnet"},
				},
			}
			_ = json.NewEncoder(w).Encode(resp)
			return
		}
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	// 1. Initial list - should be empty
	rootCmd := newRootCommand()
	buf := new(bytes.Buffer)
	rootCmd.SetOut(buf)
	rootCmd.SetErr(buf)
	rootCmd.SetArgs([]string{"litellm", "list"})

	if err := rootCmd.Execute(); err != nil {
		t.Fatalf("litellm list failed: %v", err)
	}
	if !strings.Contains(buf.String(), "No LiteLLM endpoints configured") {
		t.Errorf("expected empty message, got: %s", buf.String())
	}

	// 2. Add endpoint with pre-save verification
	buf.Reset()
	rootCmd.SetArgs([]string{
		"litellm", "add", "my-proxy",
		"--url", server.URL,
		"--api-key", "sk-secret12345678",
		"--model", "gpt-4o",
	})
	if err := rootCmd.Execute(); err != nil {
		t.Fatalf("litellm add failed: %v", err)
	}
	if !strings.Contains(buf.String(), "Added LiteLLM endpoint \"my-proxy\"") {
		t.Errorf("expected add confirmation, got: %s", buf.String())
	}

	// Verify persistence in litellm.json
	cfgPath := filepath.Join(configDir, "litellm.json")
	savedCfg := config.LoadGlobalLiteLLMConfigFrom(cfgPath, nil)
	if savedCfg.DefaultEndpoint != "my-proxy" {
		t.Errorf("expected defaultEndpoint 'my-proxy', got %q", savedCfg.DefaultEndpoint)
	}
	if ep, exists := savedCfg.Endpoints["my-proxy"]; !exists || ep.BaseURL != server.URL {
		t.Errorf("endpoint not saved properly: %+v", savedCfg.Endpoints)
	}

	// 3. Show endpoint with masked key
	buf.Reset()
	rootCmd.SetArgs([]string{"litellm", "show", "my-proxy"})
	if err := rootCmd.Execute(); err != nil {
		t.Fatalf("litellm show failed: %v", err)
	}
	output := buf.String()
	if !strings.Contains(output, "sk-s...5678") {
		t.Errorf("expected masked API key in output, got: %s", output)
	}
	if strings.Contains(output, "sk-secret12345678") {
		t.Errorf("full API key leaked in show output: %s", output)
	}

	// Show with --show-key
	buf.Reset()
	rootCmd.SetArgs([]string{"litellm", "show", "my-proxy", "--show-key"})
	if err := rootCmd.Execute(); err != nil {
		t.Fatalf("litellm show --show-key failed: %v", err)
	}
	if !strings.Contains(buf.String(), "sk-secret12345678") {
		t.Errorf("expected full API key with --show-key, got: %s", buf.String())
	}

	// 4. Update endpoint model
	buf.Reset()
	rootCmd.SetArgs([]string{"litellm", "update", "my-proxy", "--model", "claude-3-7-sonnet", "--skip-verify"})
	if err := rootCmd.Execute(); err != nil {
		t.Fatalf("litellm update failed: %v", err)
	}
	savedCfg = config.LoadGlobalLiteLLMConfigFrom(cfgPath, nil)
	if savedCfg.Endpoints["my-proxy"].DefaultModel != "claude-3-7-sonnet" {
		t.Errorf("expected updated model claude-3-7-sonnet, got %s", savedCfg.Endpoints["my-proxy"].DefaultModel)
	}

	// 5. Add second endpoint with --skip-verify
	buf.Reset()
	rootCmd.SetArgs([]string{
		"litellm", "add", "ollama",
		"--url", "http://localhost:11434/v1",
		"--model", "llama3",
		"--skip-verify",
	})
	if err := rootCmd.Execute(); err != nil {
		t.Fatalf("litellm add ollama failed: %v", err)
	}

	// 6. Set default endpoint
	buf.Reset()
	rootCmd.SetArgs([]string{"litellm", "set-default", "ollama"})
	if err := rootCmd.Execute(); err != nil {
		t.Fatalf("litellm set-default failed: %v", err)
	}
	savedCfg = config.LoadGlobalLiteLLMConfigFrom(cfgPath, nil)
	if savedCfg.DefaultEndpoint != "ollama" {
		t.Errorf("expected defaultEndpoint 'ollama', got %q", savedCfg.DefaultEndpoint)
	}

	// 7. Test endpoint
	buf.Reset()
	rootCmd.SetArgs([]string{"litellm", "test", "my-proxy"})
	if err := rootCmd.Execute(); err != nil {
		t.Fatalf("litellm test failed: %v", err)
	}
	testOut := buf.String()
	if !strings.Contains(testOut, "Status: Healthy") || !strings.Contains(testOut, "Discovered 2 model(s)") {
		t.Errorf("expected test success, got: %s", testOut)
	}

	// 8. Delete endpoint with --force
	buf.Reset()
	rootCmd.SetArgs([]string{"litellm", "delete", "my-proxy", "--force"})
	if err := rootCmd.Execute(); err != nil {
		t.Fatalf("litellm delete failed: %v", err)
	}
	savedCfg = config.LoadGlobalLiteLLMConfigFrom(cfgPath, nil)
	if _, exists := savedCfg.Endpoints["my-proxy"]; exists {
		t.Errorf("expected my-proxy to be deleted")
	}
}
