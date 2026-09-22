package main

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"

	"github.com/divmora/localharness/internal/config"
)

func TestPreflightCheck_HealthyEndpoint(t *testing.T) {
	configDir, cleanup := setupTestLiteLLMConfig(t)
	defer cleanup()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]interface{}{
			"object": "list",
			"data":   []map[string]interface{}{{"id": "gpt-4o"}},
		})
	}))
	defer server.Close()

	cfg := &config.GlobalLiteLLMConfig{
		DefaultEndpoint: "local",
		Endpoints: map[string]config.LiteLLMEndpoint{
			"local": {
				BaseURL: server.URL,
			},
		},
	}
	_ = config.SaveGlobalLiteLLMConfigTo(filepath.Join(configDir, "litellm.json"), cfg, nil)

	err := runPreflightCheck(runFlags{endpoint: "local"})
	if err != nil {
		t.Fatalf("expected preflight check to pass, got: %v", err)
	}
}

func TestPreflightCheck_UnhealthyEndpoint(t *testing.T) {
	configDir, cleanup := setupTestLiteLLMConfig(t)
	defer cleanup()

	cfg := &config.GlobalLiteLLMConfig{
		DefaultEndpoint: "offline",
		Endpoints: map[string]config.LiteLLMEndpoint{
			"offline": {
				BaseURL:        "http://127.0.0.1:59998",
				TimeoutSeconds: 1,
			},
		},
	}
	_ = config.SaveGlobalLiteLLMConfigTo(filepath.Join(configDir, "litellm.json"), cfg, nil)

	err := runPreflightCheck(runFlags{endpoint: "offline"})
	if err == nil {
		t.Fatal("expected preflight check to fail for offline endpoint, got nil")
	}

	errMsg := err.Error()
	if !strings.Contains(errMsg, "LiteLLM pre-flight health check failed") {
		t.Errorf("expected failure message, got: %s", errMsg)
	}
	if !strings.Contains(errMsg, "--skip-health-check") {
		t.Errorf("expected troubleshooting flag advice, got: %s", errMsg)
	}
}

func TestPreflightCheck_SkipHealthCheck(t *testing.T) {
	configDir, cleanup := setupTestLiteLLMConfig(t)
	defer cleanup()

	cfg := &config.GlobalLiteLLMConfig{
		DefaultEndpoint: "offline",
		Endpoints: map[string]config.LiteLLMEndpoint{
			"offline": {
				BaseURL: "http://127.0.0.1:59998",
			},
		},
	}
	_ = config.SaveGlobalLiteLLMConfigTo(filepath.Join(configDir, "litellm.json"), cfg, nil)

	// With skipHealthCheck = true
	err := runPreflightCheck(runFlags{endpoint: "offline", skipHealthCheck: true})
	if err != nil {
		t.Fatalf("expected skipped check to return nil, got: %v", err)
	}

	// With offline = true
	err = runPreflightCheck(runFlags{endpoint: "offline", offline: true})
	if err != nil {
		t.Fatalf("expected offline mode to skip check, got: %v", err)
	}
}

func TestFirstTimeSetupWizard_Interactive(t *testing.T) {
	configDir, cleanup := setupTestLiteLLMConfig(t)
	defer cleanup()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]interface{}{
			"object": "list",
			"data":   []map[string]interface{}{{"id": "test-model"}},
		})
	}))
	defer server.Close()

	// Simulate user input: name, url, api-key, model
	input := "test-endpoint\n" + server.URL + "\nsecret-key\ncustom-gpt\n"
	inBuf := strings.NewReader(input)
	outBuf := new(bytes.Buffer)

	cfg, err := runFirstTimeSetupWizard(inBuf, outBuf)
	if err != nil {
		t.Fatalf("runFirstTimeSetupWizard failed: %v", err)
	}

	if cfg.DefaultEndpoint != "test-endpoint" {
		t.Errorf("expected defaultEndpoint 'test-endpoint', got %q", cfg.DefaultEndpoint)
	}
	ep, exists := cfg.Endpoints["test-endpoint"]
	if !exists {
		t.Fatalf("expected endpoint 'test-endpoint' to exist")
	}
	if ep.BaseURL != server.URL {
		t.Errorf("expected baseUrl %s, got %s", server.URL, ep.BaseURL)
	}
	if ep.APIKey != "secret-key" {
		t.Errorf("expected apiKey secret-key, got %s", ep.APIKey)
	}
	if ep.DefaultModel != "custom-gpt" {
		t.Errorf("expected model custom-gpt, got %s", ep.DefaultModel)
	}

	// Verify litellm.json was written
	saved := config.LoadGlobalLiteLLMConfigFrom(filepath.Join(configDir, "litellm.json"), nil)
	if saved.DefaultEndpoint != "test-endpoint" {
		t.Errorf("expected saved defaultEndpoint 'test-endpoint', got %q", saved.DefaultEndpoint)
	}
}

func TestShouldRunFirstTimeWizard(t *testing.T) {
	_, cleanup := setupTestLiteLLMConfig(t)
	defer cleanup()

	// Initially empty: should run wizard
	if !shouldRunFirstTimeWizard(runFlags{}) {
		t.Error("expected shouldRunFirstTimeWizard to be true when no endpoints exist")
	}

	// Offline flag bypasses
	if shouldRunFirstTimeWizard(runFlags{offline: true}) {
		t.Error("expected shouldRunFirstTimeWizard to be false when offline=true")
	}

	// Explicit endpoint bypasses
	if shouldRunFirstTimeWizard(runFlags{endpoint: "ollama"}) {
		t.Error("expected shouldRunFirstTimeWizard to be false when endpoint flag is set")
	}

	// Env var bypasses
	t.Setenv("LITELLM_API_KEY", "some-key")
	if shouldRunFirstTimeWizard(runFlags{}) {
		t.Error("expected shouldRunFirstTimeWizard to be false when LITELLM_API_KEY is set")
	}
}
