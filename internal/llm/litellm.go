package llm

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"sort"
	"strings"
	"time"

	"github.com/divmora/localharness/internal/config"
)

// EndpointHealthResult contains health check and reachability diagnostics for a LiteLLM endpoint.
type EndpointHealthResult struct {
	EndpointName string        `json:"endpoint_name"`
	BaseURL      string        `json:"base_url"`
	Healthy      bool          `json:"healthy"`
	Latency      time.Duration `json:"latency"`
	StatusCode   int           `json:"status_code"`
	ModelsCount  int           `json:"models_count,omitempty"`
	Error        string        `json:"error,omitempty"`
}

// CheckEndpointHealth verifies connectivity, authentication, and liveness for a LiteLLM endpoint.
// It probes the /models endpoint with configured authentication headers.
func CheckEndpointHealth(ctx context.Context, endpoint config.LiteLLMEndpoint) (*EndpointHealthResult, error) {
	baseURL := strings.TrimRight(endpoint.BaseURL, "/")
	if baseURL == "" {
		return &EndpointHealthResult{
			Healthy: false,
			Error:   "base URL is empty",
		}, fmt.Errorf("base URL is empty")
	}

	client := &http.Client{}
	if endpoint.TimeoutSeconds > 0 {
		client.Timeout = time.Duration(endpoint.TimeoutSeconds) * time.Second
	} else {
		client.Timeout = 5 * time.Second
	}

	probeURL := baseURL + "/models"
	start := time.Now()

	req, err := http.NewRequestWithContext(ctx, "GET", probeURL, nil)
	if err != nil {
		return &EndpointHealthResult{
			BaseURL: baseURL,
			Healthy: false,
			Error:   fmt.Sprintf("failed to construct probe request: %v", err),
		}, err
	}

	req.Header.Set("Accept", "application/json")
	if endpoint.APIKey != "" {
		req.Header.Set("Authorization", "Bearer "+endpoint.APIKey)
	}
	for k, v := range endpoint.Headers {
		req.Header.Set(k, v)
	}

	resp, err := client.Do(req)
	latency := time.Since(start)

	if err != nil {
		// Network connection failure or timeout
		errMsg := err.Error()
		if strings.Contains(errMsg, "connection refused") {
			errMsg = "connection refused: server is offline or port is closed"
		} else if strings.Contains(errMsg, "context deadline exceeded") || strings.Contains(errMsg, "Client.Timeout") {
			errMsg = "request timed out"
		}
		return &EndpointHealthResult{
			BaseURL: baseURL,
			Healthy: false,
			Latency: latency,
			Error:   errMsg,
		}, fmt.Errorf("LiteLLM endpoint %s unreachable: %s", baseURL, errMsg)
	}
	defer resp.Body.Close()

	body, _ := io.ReadAll(io.LimitReader(resp.Body, 1024*1024))
	result := &EndpointHealthResult{
		BaseURL:    baseURL,
		Latency:    latency,
		StatusCode: resp.StatusCode,
	}

	switch resp.StatusCode {
	case http.StatusOK:
		result.Healthy = true
		// Try parsing model count
		var listResp struct {
			Data []struct {
				ID string `json:"id"`
			} `json:"data"`
		}
		if err := json.Unmarshal(body, &listResp); err == nil && len(listResp.Data) > 0 {
			result.ModelsCount = len(listResp.Data)
		}
		return result, nil

	case http.StatusUnauthorized, http.StatusForbidden:
		result.Healthy = false
		result.Error = fmt.Sprintf("HTTP %d: invalid or unauthorized API key", resp.StatusCode)
		return result, fmt.Errorf("authentication failed: %s", result.Error)

	case http.StatusNotFound:
		// Try fallback to /health or /health/liveness
		healthURL := baseURL + "/health"
		if strings.HasSuffix(baseURL, "/v1") {
			healthURL = strings.TrimSuffix(baseURL, "/v1") + "/health"
		}
		if hReq, hErr := http.NewRequestWithContext(ctx, "GET", healthURL, nil); hErr == nil {
			if hResp, doErr := client.Do(hReq); doErr == nil {
				defer hResp.Body.Close()
				if hResp.StatusCode == http.StatusOK || hResp.StatusCode == http.StatusNoContent {
					result.Healthy = true
					result.StatusCode = hResp.StatusCode
					result.Latency = time.Since(start)
					return result, nil
				}
			}
		}
		result.Healthy = false
		result.Error = "HTTP 404: /models endpoint not found on server"
		return result, fmt.Errorf("%s", result.Error)

	case http.StatusBadGateway, http.StatusServiceUnavailable, http.StatusGatewayTimeout:
		result.Healthy = false
		result.Error = fmt.Sprintf("HTTP %d: upstream LLM proxy or gateway error", resp.StatusCode)
		return result, fmt.Errorf("%s", result.Error)

	default:
		result.Healthy = false
		result.Error = fmt.Sprintf("HTTP %d (%s)", resp.StatusCode, resp.Status)
		return result, fmt.Errorf("unexpected status from LiteLLM endpoint: %s", result.Error)
	}
}

// FetchAvailableModels queries /models on the LiteLLM endpoint and returns available model identifiers.
func FetchAvailableModels(ctx context.Context, endpoint config.LiteLLMEndpoint) ([]string, error) {
	baseURL := strings.TrimRight(endpoint.BaseURL, "/")
	if baseURL == "" {
		return nil, fmt.Errorf("base URL is empty")
	}

	client := &http.Client{}
	if endpoint.TimeoutSeconds > 0 {
		client.Timeout = time.Duration(endpoint.TimeoutSeconds) * time.Second
	} else {
		client.Timeout = 5 * time.Second
	}

	probeURL := baseURL + "/models"
	req, err := http.NewRequestWithContext(ctx, "GET", probeURL, nil)
	if err != nil {
		return nil, fmt.Errorf("failed to construct request: %w", err)
	}

	req.Header.Set("Accept", "application/json")
	if endpoint.APIKey != "" {
		req.Header.Set("Authorization", "Bearer "+endpoint.APIKey)
	}
	for k, v := range endpoint.Headers {
		req.Header.Set(k, v)
	}

	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("failed to fetch models: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 1024))
		return nil, fmt.Errorf("failed to list models (HTTP %d): %s", resp.StatusCode, strings.TrimSpace(string(body)))
	}

	var listResp struct {
		Data []struct {
			ID string `json:"id"`
		} `json:"data"`
	}

	if err := json.NewDecoder(resp.Body).Decode(&listResp); err != nil {
		return nil, fmt.Errorf("failed to decode models response: %w", err)
	}

	var models []string
	seen := make(map[string]bool)
	for _, m := range listResp.Data {
		id := strings.TrimSpace(m.ID)
		if id != "" && !seen[id] {
			seen[id] = true
			models = append(models, id)
		}
	}

	sort.Strings(models)
	return models, nil
}

// ParseModelAndEndpoint checks if rawModel uses the "endpoint_name/model_name" syntax.
// If the prefix matches a configured endpoint in liteCfg, it returns (endpointName, modelName).
// Otherwise, it returns ("", rawModel).
func ParseModelAndEndpoint(rawModel string, liteCfg *config.GlobalLiteLLMConfig) (string, string) {
	if rawModel == "" || liteCfg == nil || len(liteCfg.Endpoints) == 0 {
		return "", rawModel
	}

	prefix, suffix, found := strings.Cut(rawModel, "/")
	if found && prefix != "" && suffix != "" {
		if _, exists := liteCfg.Endpoints[prefix]; exists {
			return prefix, suffix
		}
	}

	return "", rawModel
}
