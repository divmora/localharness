package main

import (
	"bufio"
	"context"
	"fmt"
	"io"
	"net/url"
	"os"
	"strings"
	"time"

	"github.com/charmbracelet/x/term"

	"github.com/divmora/localharness/internal/config"
	"github.com/divmora/localharness/internal/llm"
)

// shouldRunFirstTimeWizard checks if no LiteLLM credentials or endpoints exist anywhere.
func shouldRunFirstTimeWizard(flags runFlags) bool {
	if flags.offline || flags.endpoint != "" {
		return false
	}

	liteCfg := config.LoadGlobalLiteLLMConfig(nil)
	if len(liteCfg.Endpoints) > 0 {
		return false
	}

	if os.Getenv("LITELLM_BASE_URL") != "" ||
		os.Getenv("OPENAI_BASE_URL") != "" ||
		os.Getenv("LITELLM_API_KEY") != "" ||
		os.Getenv("OPENAI_API_KEY") != "" {
		return false
	}

	return true
}

// runFirstTimeSetupWizard interactively prompts the user for LiteLLM credentials and persists them.
func runFirstTimeSetupWizard(in io.Reader, out io.Writer) (*config.GlobalLiteLLMConfig, error) {
	fmt.Fprintln(out, "")
	fmt.Fprintln(out, "╭─────────────────────────────────────────────────────────────╮")
	fmt.Fprintln(out, "│ Welcome to LocalHarness!                                    │")
	fmt.Fprintln(out, "│ No LLM proxy or model endpoints are configured yet.        │")
	fmt.Fprintln(out, "│ Let's configure your LiteLLM proxy or local server.         │")
	fmt.Fprintln(out, "╰─────────────────────────────────────────────────────────────╯")
	fmt.Fprintln(out, "")

	scanner := bufio.NewScanner(in)

	// 1. Endpoint name
	fmt.Fprint(out, "Endpoint Name [default]: ")
	name := "default"
	if scanner.Scan() {
		input := strings.TrimSpace(scanner.Text())
		if input != "" {
			name = input
		}
	}

	// 2. Base URL
	fmt.Fprint(out, "Base URL [http://localhost:4000/v1]: ")
	baseURL := "http://localhost:4000/v1"
	if scanner.Scan() {
		input := strings.TrimSpace(scanner.Text())
		if input != "" {
			baseURL = input
		}
	}
	baseURL = strings.TrimRight(baseURL, "/")

	u, err := url.Parse(baseURL)
	if err != nil || (u.Scheme != "http" && u.Scheme != "https") {
		return nil, fmt.Errorf("invalid base URL %q: must start with http:// or https://", baseURL)
	}

	// 3. API Key
	fmt.Fprint(out, "API Key (leave empty for local Ollama/vLLM): ")
	var apiKey string
	// If reading from an actual interactive terminal file descriptor, mask input
	if file, ok := in.(*os.File); ok && term.IsTerminal(file.Fd()) {
		byteKey, termErr := term.ReadPassword(file.Fd())
		fmt.Fprintln(out, "")
		if termErr == nil {
			apiKey = strings.TrimSpace(string(byteKey))
		}
	} else {
		if scanner.Scan() {
			apiKey = strings.TrimSpace(scanner.Text())
		}
	}

	// 4. Default Model
	fmt.Fprint(out, "Default Model [gpt-4o]: ")
	model := "gpt-4o"
	if scanner.Scan() {
		input := strings.TrimSpace(scanner.Text())
		if input != "" {
			model = input
		}
	}

	endpoint := config.LiteLLMEndpoint{
		BaseURL:      baseURL,
		APIKey:       apiKey,
		DefaultModel: model,
	}

	fmt.Fprintln(out, "")
	fmt.Fprintf(out, "Testing connection to %s...\n", baseURL)

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	health, hErr := llm.CheckEndpointHealth(ctx, endpoint)
	cancel()

	if hErr != nil || (health != nil && !health.Healthy) {
		fmt.Fprintf(out, "⚠ Connection check failed: %v\n", hErr)
		fmt.Fprint(out, "Save configuration anyway? [y/N]: ")
		saveAnyway := false
		if scanner.Scan() {
			ans := strings.ToLower(strings.TrimSpace(scanner.Text()))
			if ans == "y" || ans == "yes" {
				saveAnyway = true
			}
		}
		if !saveAnyway {
			return nil, fmt.Errorf("setup cancelled")
		}
	} else {
		fmt.Fprintf(out, "✓ Connected successfully (%v, HTTP %d)\n", health.Latency.Round(time.Millisecond), health.StatusCode)
	}

	cfg := &config.GlobalLiteLLMConfig{
		DefaultEndpoint: name,
		Endpoints: map[string]config.LiteLLMEndpoint{
			name: endpoint,
		},
	}

	if err := config.SaveGlobalLiteLLMConfig(cfg, nil); err != nil {
		return nil, fmt.Errorf("failed to save ~/.divmora/config/litellm.json: %w", err)
	}

	fmt.Fprintln(out, "✓ Configuration saved to ~/.divmora/config/litellm.json")
	fmt.Fprintln(out, "")
	return cfg, nil
}
