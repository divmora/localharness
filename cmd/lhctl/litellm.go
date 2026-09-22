package main

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"net/url"
	"os"
	"sort"
	"strings"
	"sync"
	"text/tabwriter"
	"time"

	"github.com/charmbracelet/x/term"
	"github.com/spf13/cobra"

	"github.com/divmora/localharness/internal/config"
	"github.com/divmora/localharness/internal/llm"
)

// newLiteLLMCommand returns the 'litellm' subcommand tree for lhctl.
func newLiteLLMCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:     "litellm",
		Aliases: []string{"endpoint", "endpoints"},
		Short:   "Manage LiteLLM endpoints and proxy configurations",
		Long: `Manage LiteLLM proxy endpoints defined in ~/.divmora/config/litellm.json.

Allows listing, adding, inspecting, updating, removing, and testing LiteLLM endpoints
for seamless multi-model routing and proxy failover.`,
	}

	cmd.AddCommand(newListLiteLLMCommand())
	cmd.AddCommand(newAddLiteLLMCommand())
	cmd.AddCommand(newShowLiteLLMCommand())
	cmd.AddCommand(newUpdateLiteLLMCommand())
	cmd.AddCommand(newSetDefaultLiteLLMCommand())
	cmd.AddCommand(newDeleteLiteLLMCommand())
	cmd.AddCommand(newTestLiteLLMCommand())

	return cmd
}

// newListLiteLLMCommand creates 'lhctl litellm list'.
func newListLiteLLMCommand() *cobra.Command {
	var (
		noCheck  bool
		jsonFlag bool
	)

	cmd := &cobra.Command{
		Use:   "list",
		Short: "List all configured LiteLLM endpoints",
		RunE: func(cmd *cobra.Command, args []string) error {
			cfg := config.LoadGlobalLiteLLMConfig(nil)
			if len(cfg.Endpoints) == 0 {
				if jsonFlag {
					cmd.Println("[]")
					return nil
				}
				cmd.Println("No LiteLLM endpoints configured. Use 'lhctl litellm add <name> --url <url>' to add one.")
				return nil
			}

			// Sort endpoint names for deterministic output
			names := make([]string, 0, len(cfg.Endpoints))
			for name := range cfg.Endpoints {
				names = append(names, name)
			}
			sort.Strings(names)

			type endpointInfo struct {
				Name         string `json:"name"`
				IsDefault    bool   `json:"is_default"`
				BaseURL      string `json:"base_url"`
				DefaultModel string `json:"default_model"`
				Status       string `json:"status"`
				LatencyMs    int64  `json:"latency_ms,omitempty"`
			}

			results := make([]endpointInfo, len(names))
			var wg sync.WaitGroup

			for i, name := range names {
				ep := cfg.Endpoints[name]
				results[i] = endpointInfo{
					Name:         name,
					IsDefault:    name == cfg.DefaultEndpoint,
					BaseURL:      ep.BaseURL,
					DefaultModel: ep.DefaultModel,
					Status:       "unknown",
				}

				if !noCheck {
					wg.Add(1)
					go func(idx int, endpoint config.LiteLLMEndpoint) {
						defer wg.Done()
						ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
						defer cancel()

						res, err := llm.CheckEndpointHealth(ctx, endpoint)
						if err != nil || !res.Healthy {
							errMsg := "offline"
							if res != nil && res.Error != "" {
								errMsg = fmt.Sprintf("offline (%s)", res.Error)
							} else if err != nil {
								errMsg = fmt.Sprintf("offline (%v)", err)
							}
							results[idx].Status = errMsg
						} else {
							results[idx].Status = fmt.Sprintf("online (%v)", res.Latency.Round(time.Millisecond))
							results[idx].LatencyMs = res.Latency.Milliseconds()
						}
					}(i, ep)
				}
			}

			if !noCheck {
				wg.Wait()
			}

			if jsonFlag {
				data, err := json.MarshalIndent(results, "", "  ")
				if err != nil {
					return err
				}
				cmd.Println(string(data))
				return nil
			}

			w := tabwriter.NewWriter(cmd.OutOrStdout(), 0, 0, 3, ' ', 0)
			if noCheck {
				fmt.Fprintln(w, "NAME\tDEFAULT\tBASE URL\tDEFAULT MODEL")
				for _, r := range results {
					defMarker := ""
					if r.IsDefault {
						defMarker = "*"
					}
					model := r.DefaultModel
					if model == "" {
						model = "-"
					}
					fmt.Fprintf(w, "%s\t%s\t%s\t%s\n", r.Name, defMarker, r.BaseURL, model)
				}
			} else {
				fmt.Fprintln(w, "NAME\tDEFAULT\tBASE URL\tDEFAULT MODEL\tSTATUS")
				for _, r := range results {
					defMarker := ""
					if r.IsDefault {
						defMarker = "*"
					}
					model := r.DefaultModel
					if model == "" {
						model = "-"
					}
					fmt.Fprintf(w, "%s\t%s\t%s\t%s\t%s\n", r.Name, defMarker, r.BaseURL, model, r.Status)
				}
			}
			return w.Flush()
		},
	}

	cmd.Flags().BoolVar(&noCheck, "no-check", false, "Skip live health checks")
	cmd.Flags().BoolVar(&jsonFlag, "json", false, "Output as JSON")
	return cmd
}

// newAddLiteLLMCommand creates 'lhctl litellm add'.
func newAddLiteLLMCommand() *cobra.Command {
	var (
		rawURL     string
		apiKey     string
		model      string
		timeout    int
		isDefault  bool
		skipVerify bool
	)

	cmd := &cobra.Command{
		Use:   "add <name>",
		Short: "Add a new LiteLLM endpoint configuration",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			name := strings.TrimSpace(args[0])
			if name == "" {
				return fmt.Errorf("endpoint name cannot be empty")
			}
			if strings.ContainsAny(name, " /\\:\t\n") {
				return fmt.Errorf("invalid endpoint name %q: cannot contain spaces or slashes", name)
			}

			if rawURL == "" {
				return fmt.Errorf("--url is required (e.g. --url http://localhost:4000/v1)")
			}

			u, err := url.Parse(rawURL)
			if err != nil || (u.Scheme != "http" && u.Scheme != "https") {
				return fmt.Errorf("invalid URL %q: must be a valid http:// or https:// URL", rawURL)
			}

			cfg := config.LoadGlobalLiteLLMConfig(nil)
			if cfg.Endpoints == nil {
				cfg.Endpoints = make(map[string]config.LiteLLMEndpoint)
			}

			if _, exists := cfg.Endpoints[name]; exists {
				return fmt.Errorf("endpoint %q already exists; use 'lhctl litellm update %s' to modify it", name, name)
			}

			endpoint := config.LiteLLMEndpoint{
				BaseURL:        strings.TrimRight(rawURL, "/"),
				APIKey:         apiKey,
				DefaultModel:   model,
				TimeoutSeconds: timeout,
			}

			if !skipVerify {
				cmd.Printf("Verifying connection to %s...\n", endpoint.BaseURL)
				ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
				defer cancel()

				res, err := llm.CheckEndpointHealth(ctx, endpoint)
				if err != nil || !res.Healthy {
					return fmt.Errorf("connection verification failed: %v\nUse --skip-verify to add anyway if server is currently offline", err)
				}
				cmd.Printf("✓ Connection successful (%v, HTTP %d)\n", res.Latency.Round(time.Millisecond), res.StatusCode)
			}

			cfg.Endpoints[name] = endpoint
			if isDefault || len(cfg.Endpoints) == 1 || cfg.DefaultEndpoint == "" {
				cfg.DefaultEndpoint = name
			}

			if err := config.SaveGlobalLiteLLMConfig(cfg, nil); err != nil {
				return fmt.Errorf("failed to save configuration: %w", err)
			}

			defMsg := ""
			if cfg.DefaultEndpoint == name {
				defMsg = " (set as default)"
			}
			cmd.Printf("✓ Added LiteLLM endpoint %q%s\n", name, defMsg)
			return nil
		},
	}

	cmd.Flags().StringVar(&rawURL, "url", "", "Base URL of LiteLLM proxy (e.g. http://localhost:4000/v1)")
	_ = cmd.MarkFlagRequired("url")
	cmd.Flags().StringVar(&apiKey, "api-key", "", "API key / Bearer token (optional for local Ollama/vLLM)")
	cmd.Flags().StringVar(&model, "model", "", "Default model identifier (e.g. gpt-4o, claude-3-5-sonnet)")
	cmd.Flags().IntVar(&timeout, "timeout", 0, "Request timeout in seconds (default 0 = provider default)")
	cmd.Flags().BoolVar(&isDefault, "default", false, "Set this endpoint as the default")
	cmd.Flags().BoolVar(&skipVerify, "skip-verify", false, "Skip pre-save connection verification")

	return cmd
}

// newShowLiteLLMCommand creates 'lhctl litellm show <name>'.
func newShowLiteLLMCommand() *cobra.Command {
	var (
		showKey  bool
		jsonFlag bool
	)

	cmd := &cobra.Command{
		Use:   "show <name>",
		Short: "Display details of a configured LiteLLM endpoint",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			name := args[0]
			cfg := config.LoadGlobalLiteLLMConfig(nil)
			ep, exists := cfg.Endpoints[name]
			if !exists {
				return fmt.Errorf("endpoint %q not found", name)
			}

			maskedKey := maskAPIKey(ep.APIKey)
			if showKey {
				maskedKey = ep.APIKey
			}

			if jsonFlag {
				out := map[string]interface{}{
					"name":               name,
					"is_default":         name == cfg.DefaultEndpoint,
					"base_url":           ep.BaseURL,
					"api_key":            maskedKey,
					"default_model":      ep.DefaultModel,
					"headers":            ep.Headers,
					"timeout_seconds":    ep.TimeoutSeconds,
					"fallback_endpoints": ep.FallbackEndpoints,
				}
				data, err := json.MarshalIndent(out, "", "  ")
				if err != nil {
					return err
				}
				cmd.Println(string(data))
				return nil
			}

			defTag := "No"
			if name == cfg.DefaultEndpoint {
				defTag = "Yes (active default)"
			}

			cmd.Printf("Endpoint:            %s\n", name)
			cmd.Printf("Default:             %s\n", defTag)
			cmd.Printf("Base URL:            %s\n", ep.BaseURL)
			if ep.APIKey != "" {
				if showKey {
					cmd.Printf("API Key:             %s\n", ep.APIKey)
				} else {
					cmd.Printf("API Key:             %s (use --show-key to view full key)\n", maskedKey)
				}
			} else {
				cmd.Printf("API Key:             (none configured)\n")
			}
			if ep.DefaultModel != "" {
				cmd.Printf("Default Model:       %s\n", ep.DefaultModel)
			}
			if ep.TimeoutSeconds > 0 {
				cmd.Printf("Timeout:             %ds\n", ep.TimeoutSeconds)
			}
			if len(ep.Headers) > 0 {
				cmd.Printf("Custom Headers:      %v\n", ep.Headers)
			}
			if len(ep.FallbackEndpoints) > 0 {
				cmd.Printf("Fallback Endpoints:  %s\n", strings.Join(ep.FallbackEndpoints, ", "))
			}
			return nil
		},
	}

	cmd.Flags().BoolVar(&showKey, "show-key", false, "Display the unmasked API key")
	cmd.Flags().BoolVar(&jsonFlag, "json", false, "Output as JSON")
	return cmd
}

// newUpdateLiteLLMCommand creates 'lhctl litellm update <name>'.
func newUpdateLiteLLMCommand() *cobra.Command {
	var (
		rawURL     string
		apiKey     string
		model      string
		timeout    int
		skipVerify bool
	)

	cmd := &cobra.Command{
		Use:   "update <name>",
		Short: "Update configuration for an existing LiteLLM endpoint",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			name := args[0]
			cfg := config.LoadGlobalLiteLLMConfig(nil)
			ep, exists := cfg.Endpoints[name]
			if !exists {
				return fmt.Errorf("endpoint %q not found", name)
			}

			updated := false
			if cmd.Flags().Changed("url") {
				u, err := url.Parse(rawURL)
				if err != nil || (u.Scheme != "http" && u.Scheme != "https") {
					return fmt.Errorf("invalid URL %q: must be http:// or https://", rawURL)
				}
				ep.BaseURL = strings.TrimRight(rawURL, "/")
				updated = true
			}
			if cmd.Flags().Changed("api-key") {
				ep.APIKey = apiKey
				updated = true
			}
			if cmd.Flags().Changed("model") {
				ep.DefaultModel = model
				updated = true
			}
			if cmd.Flags().Changed("timeout") {
				ep.TimeoutSeconds = timeout
				updated = true
			}

			if !updated {
				cmd.Println("No fields specified to update.")
				return nil
			}

			if !skipVerify && (cmd.Flags().Changed("url") || cmd.Flags().Changed("api-key")) {
				cmd.Printf("Verifying connection to %s...\n", ep.BaseURL)
				ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
				defer cancel()

				res, err := llm.CheckEndpointHealth(ctx, ep)
				if err != nil || !res.Healthy {
					return fmt.Errorf("connection verification failed: %v\nUse --skip-verify to update anyway", err)
				}
				cmd.Printf("✓ Connection successful (%v, HTTP %d)\n", res.Latency.Round(time.Millisecond), res.StatusCode)
			}

			cfg.Endpoints[name] = ep
			if err := config.SaveGlobalLiteLLMConfig(cfg, nil); err != nil {
				return fmt.Errorf("failed to save configuration: %w", err)
			}

			cmd.Printf("✓ Updated LiteLLM endpoint %q\n", name)
			return nil
		},
	}

	cmd.Flags().StringVar(&rawURL, "url", "", "New base URL")
	cmd.Flags().StringVar(&apiKey, "api-key", "", "New API key / Bearer token")
	cmd.Flags().StringVar(&model, "model", "", "New default model")
	cmd.Flags().IntVar(&timeout, "timeout", 0, "New request timeout in seconds")
	cmd.Flags().BoolVar(&skipVerify, "skip-verify", false, "Skip connection verification")
	return cmd
}

// newSetDefaultLiteLLMCommand creates 'lhctl litellm set-default <name>'.
func newSetDefaultLiteLLMCommand() *cobra.Command {
	return &cobra.Command{
		Use:   "set-default <name>",
		Short: "Switch the default LiteLLM endpoint used across sessions",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			name := args[0]
			cfg := config.LoadGlobalLiteLLMConfig(nil)
			if _, exists := cfg.Endpoints[name]; !exists {
				return fmt.Errorf("endpoint %q not found", name)
			}

			cfg.DefaultEndpoint = name
			if err := config.SaveGlobalLiteLLMConfig(cfg, nil); err != nil {
				return fmt.Errorf("failed to save configuration: %w", err)
			}

			cmd.Printf("✓ Default LiteLLM endpoint set to %q\n", name)
			return nil
		},
	}
}

// newDeleteLiteLLMCommand creates 'lhctl litellm delete <name>'.
func newDeleteLiteLLMCommand() *cobra.Command {
	var force bool

	cmd := &cobra.Command{
		Use:     "delete <name>",
		Aliases: []string{"remove", "rm"},
		Short:   "Delete a configured LiteLLM endpoint",
		Args:    cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			name := args[0]
			cfg := config.LoadGlobalLiteLLMConfig(nil)
			if _, exists := cfg.Endpoints[name]; !exists {
				return fmt.Errorf("endpoint %q not found", name)
			}

			if !force {
				if !term.IsTerminal(os.Stdin.Fd()) {
					return fmt.Errorf("deleting endpoint %q requires --force in non-interactive environments", name)
				}
				cmd.Printf("Are you sure you want to delete endpoint %q? [y/N]: ", name)
				reader := bufio.NewReader(os.Stdin)
				ans, _ := reader.ReadString('\n')
				ans = strings.TrimSpace(strings.ToLower(ans))
				if ans != "y" && ans != "yes" {
					cmd.Println("Aborted.")
					return nil
				}
			}

			delete(cfg.Endpoints, name)

			// If the deleted endpoint was default, choose a new default if any remain
			if cfg.DefaultEndpoint == name {
				cfg.DefaultEndpoint = ""
				for k := range cfg.Endpoints {
					cfg.DefaultEndpoint = k
					break
				}
			}

			if err := config.SaveGlobalLiteLLMConfig(cfg, nil); err != nil {
				return fmt.Errorf("failed to save configuration: %w", err)
			}

			cmd.Printf("✓ Deleted LiteLLM endpoint %q\n", name)
			if cfg.DefaultEndpoint != "" {
				cmd.Printf("  Active default is now %q\n", cfg.DefaultEndpoint)
			}
			return nil
		},
	}

	cmd.Flags().BoolVarP(&force, "force", "f", false, "Delete without confirmation prompt")
	return cmd
}

// newTestLiteLLMCommand creates 'lhctl litellm test [name]'.
func newTestLiteLLMCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "test [name]",
		Short: "Perform an on-demand latency test and catalog check against endpoints",
		Args:  cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			cfg := config.LoadGlobalLiteLLMConfig(nil)
			if len(cfg.Endpoints) == 0 {
				return fmt.Errorf("no LiteLLM endpoints configured. Use 'lhctl litellm add' to add one")
			}

			var targets []string
			if len(args) == 1 {
				name := args[0]
				if _, exists := cfg.Endpoints[name]; !exists {
					return fmt.Errorf("endpoint %q not found", name)
				}
				targets = append(targets, name)
			} else {
				for name := range cfg.Endpoints {
					targets = append(targets, name)
				}
				sort.Strings(targets)
			}

			for _, name := range targets {
				ep := cfg.Endpoints[name]
				defTag := ""
				if name == cfg.DefaultEndpoint {
					defTag = " [default]"
				}
				cmd.Printf("Testing endpoint %q%s (%s)...\n", name, defTag, ep.BaseURL)

				ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
				health, err := llm.CheckEndpointHealth(ctx, ep)
				cancel()

				if err != nil || !health.Healthy {
					cmd.Printf("  ✗ Health Check Failed: %v\n\n", err)
					continue
				}
				cmd.Printf("  ✓ Status: Healthy (HTTP %d, %v)\n", health.StatusCode, health.Latency.Round(time.Millisecond))

				ctxModel, cancelModel := context.WithTimeout(context.Background(), 5*time.Second)
				models, err := llm.FetchAvailableModels(ctxModel, ep)
				cancelModel()

				if err != nil {
					cmd.Printf("  ⚠ Models discovery (/models) failed: %v\n\n", err)
				} else {
					cmd.Printf("  ✓ Discovered %d model(s):\n", len(models))
					sampleCount := min(5, len(models))
					for i := 0; i < sampleCount; i++ {
						cmd.Printf("    - %s\n", models[i])
					}
					if len(models) > sampleCount {
						cmd.Printf("    ... and %d more\n", len(models)-sampleCount)
					}
					cmd.Println()
				}
			}

			return nil
		},
	}

	return cmd
}

// maskAPIKey masks all but the first 4 and last 4 characters of an API key.
func maskAPIKey(key string) string {
	if key == "" {
		return ""
	}
	if len(key) <= 8 {
		return "****"
	}
	return key[:4] + "..." + key[len(key)-4:]
}
