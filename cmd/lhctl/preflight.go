package main

import (
	"context"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/divmora/localharness/internal/config"
	"github.com/divmora/localharness/internal/llm"
)

// runPreflightCheck verifies that the active LiteLLM endpoint is reachable and responsive
// before starting an agent session turn. Returns an actionable error if unreachable.
func runPreflightCheck(flags runFlags) error {
	if flags.skipHealthCheck || flags.offline {
		return nil
	}

	liteCfg := config.LoadGlobalLiteLLMConfig(nil)

	// Resolve the active endpoint
	endpointName := flags.endpoint
	if endpointName == "" {
		if ep, _ := llm.ParseModelAndEndpoint(flags.model, liteCfg); ep != "" {
			endpointName = ep
		} else if liteCfg.DefaultEndpoint != "" {
			endpointName = liteCfg.DefaultEndpoint
		}
	}

	var endpoint config.LiteLLMEndpoint

	if endpointName != "" {
		ep, ok := liteCfg.Endpoints[endpointName]
		if !ok {
			return fmt.Errorf("LiteLLM endpoint %q not found in ~/.divmora/config/litellm.json.\nRun 'lhctl litellm list' to view available endpoints or 'lhctl litellm add %s' to create it", endpointName, endpointName)
		}
		endpoint = ep
	} else {
		// Check if inline environment variables are present
		envURL := os.Getenv("LITELLM_BASE_URL")
		if envURL == "" {
			envURL = os.Getenv("OPENAI_BASE_URL")
		}
		if envURL == "" {
			// No endpoint configured; let the first-time setup wizard or session init handle it
			return nil
		}
		endpoint = config.LiteLLMEndpoint{
			BaseURL: envURL,
			APIKey:  os.Getenv("LITELLM_API_KEY"),
		}
		if endpoint.APIKey == "" {
			endpoint.APIKey = os.Getenv("OPENAI_API_KEY")
		}
		endpointName = "environment"
	}

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()

	res, err := llm.CheckEndpointHealth(ctx, endpoint)
	if err != nil || (res != nil && !res.Healthy) {
		errDetail := "server unreachable"
		if res != nil && res.Error != "" {
			errDetail = res.Error
		} else if err != nil {
			errDetail = err.Error()
		}

		cleanBaseURL := strings.TrimRight(endpoint.BaseURL, "/")
		return fmt.Errorf(
			"LiteLLM pre-flight health check failed for endpoint %q (%s):\n  %s\n\n"+
				"Troubleshooting:\n"+
				"  1. Verify your LiteLLM proxy or local model server (Ollama/vLLM) is running:\n"+
				"     curl %s/models\n"+
				"  2. Inspect or test your endpoints:\n"+
				"     lhctl litellm test %s\n"+
				"  3. Bypass this check with --skip-health-check or --offline:\n"+
				"     lhctl --skip-health-check",
			endpointName, cleanBaseURL, errDetail, cleanBaseURL, endpointName,
		)
	}

	return nil
}
