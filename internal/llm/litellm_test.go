package llm

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/divmora/localharness/internal/config"
)

func TestParseModelAndEndpoint(t *testing.T) {
	cfg := &config.GlobalLiteLLMConfig{
		DefaultEndpoint: "default",
		Endpoints: map[string]config.LiteLLMEndpoint{
			"default":      {BaseURL: "http://localhost:4000/v1"},
			"local-ollama": {BaseURL: "http://localhost:11434/v1"},
			"cloud-proxy":  {BaseURL: "https://proxy.example.com/v1"},
		},
	}

	tests := []struct {
		rawModel     string
		wantEndpoint string
		wantModel    string
	}{
		{"gpt-4o", "", "gpt-4o"},
		{"local-ollama/llama3.3", "local-ollama", "llama3.3"},
		{"cloud-proxy/claude-3-5-sonnet", "cloud-proxy", "claude-3-5-sonnet"},
		{"unknown-endpoint/gpt-4o", "", "unknown-endpoint/gpt-4o"},
		{"anthropic/claude-3-7-sonnet", "", "anthropic/claude-3-7-sonnet"}, // anthropic is not an endpoint
		{"", "", ""},
	}

	for _, tt := range tests {
		ep, mod := ParseModelAndEndpoint(tt.rawModel, cfg)
		if ep != tt.wantEndpoint || mod != tt.wantModel {
			t.Errorf("ParseModelAndEndpoint(%q) = (%q, %q); want (%q, %q)",
				tt.rawModel, ep, mod, tt.wantEndpoint, tt.wantModel)
		}
	}
}

func TestCheckEndpointHealth_Success(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/models" {
			http.NotFound(w, r)
			return
		}
		if r.Header.Get("Authorization") != "Bearer test-key" {
			http.Error(w, "unauthorized", http.StatusUnauthorized)
			return
		}
		if r.Header.Get("X-Custom") != "custom-val" {
			http.Error(w, "missing header", http.StatusBadRequest)
			return
		}

		resp := map[string]interface{}{
			"object": "list",
			"data": []map[string]interface{}{
				{"id": "gpt-4o"},
				{"id": "claude-3-5-sonnet"},
			},
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(resp)
	}))
	defer ts.Close()

	endpoint := config.LiteLLMEndpoint{
		BaseURL:        ts.URL,
		APIKey:         "test-key",
		Headers:        map[string]string{"X-Custom": "custom-val"},
		TimeoutSeconds: 5,
	}

	res, err := CheckEndpointHealth(context.Background(), endpoint)
	if err != nil {
		t.Fatalf("expected healthy, got error: %v", err)
	}
	if !res.Healthy {
		t.Fatalf("expected healthy status, got unhealthy")
	}
	if res.ModelsCount != 2 {
		t.Errorf("expected 2 models, got %d", res.ModelsCount)
	}
	if res.StatusCode != http.StatusOK {
		t.Errorf("expected status 200, got %d", res.StatusCode)
	}
}

func TestCheckEndpointHealth_AuthFailure(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "invalid api key", http.StatusUnauthorized)
	}))
	defer ts.Close()

	endpoint := config.LiteLLMEndpoint{
		BaseURL: ts.URL,
		APIKey:  "bad-key",
	}

	res, err := CheckEndpointHealth(context.Background(), endpoint)
	if err == nil {
		t.Fatal("expected error on 401, got nil")
	}
	if res.Healthy {
		t.Fatal("expected unhealthy status")
	}
	if res.StatusCode != http.StatusUnauthorized {
		t.Errorf("expected status 401, got %d", res.StatusCode)
	}
}

func TestCheckEndpointHealth_Offline(t *testing.T) {
	// Point to an unused local port
	endpoint := config.LiteLLMEndpoint{
		BaseURL:        "http://127.0.0.1:59999",
		TimeoutSeconds: 1,
	}

	ctx, cancel := context.WithTimeout(context.Background(), 200*time.Millisecond)
	defer cancel()

	res, err := CheckEndpointHealth(ctx, endpoint)
	if err == nil {
		t.Fatal("expected error for offline server, got nil")
	}
	if res.Healthy {
		t.Fatal("expected unhealthy status")
	}
}

func TestFetchAvailableModels(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		resp := map[string]interface{}{
			"object": "list",
			"data": []map[string]interface{}{
				{"id": "gpt-4o"},
				{"id": "gemini-2.0-flash"},
				{"id": "claude-3-5-sonnet"},
			},
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(resp)
	}))
	defer ts.Close()

	endpoint := config.LiteLLMEndpoint{
		BaseURL: ts.URL,
	}

	models, err := FetchAvailableModels(context.Background(), endpoint)
	if err != nil {
		t.Fatalf("FetchAvailableModels failed: %v", err)
	}
	if len(models) != 3 {
		t.Fatalf("expected 3 models, got %d", len(models))
	}
	// Models should be sorted
	expected := []string{"claude-3-5-sonnet", "gemini-2.0-flash", "gpt-4o"}
	for i, m := range models {
		if m != expected[i] {
			t.Errorf("model[%d] = %q, want %q", i, m, expected[i])
		}
	}
}
