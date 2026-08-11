// Zenith — AI-powered codebase improvement agent.
//
// Run a focused agent by persona name:
//
//	zenith bolt                # ⚡ Performance optimization
//	zenith sentinel            # 🛡️ Security vulnerability audit
//	zenith palette             # 🎨 UX/accessibility improvement
//
// Each persona follows the same scan → pick → fix → verify → present pattern
// but targets a different domain.
//
// Usage:
//
//	export LITELLM_API_KEY=your-key
//	go run ./agents/zenith <persona> [prompt]
//	go build -o bin/zenith ./agents/zenith && bin/zenith bolt
package main

import (
	"context"
	"flag"
	"fmt"
	"log"
	"os"
	"sort"
	"strings"

	"github.com/divmora/localharness/agents/zenith/personas"
	"github.com/divmora/localharness/adk"
	"github.com/divmora/localharness/adk/policy"
)

func main() {
	// CLI flags
	workspace := flag.String("workspace", "", "Workspace directory (default: cwd)")
	endpoint := flag.String("endpoint", "", "LiteLLM endpoint name")
	model := flag.String("model", "", "Model name override")
	baseURL := flag.String("base-url", "", "Base URL override")
	apiKey := flag.String("api-key", "", "API key override")
	configFile := flag.String("config", "", "Path to YAML config file")
	verbose := flag.Bool("verbose", false, "Enable verbose debug logging")
	flag.BoolVar(verbose, "v", false, "Enable verbose debug logging (shorthand)")

	flag.Usage = printUsage
	flag.Parse()

	args := flag.Args()
	if len(args) < 1 {
		printUsage()
		os.Exit(1)
	}

	// Handle "config show" subcommand
	if args[0] == "config" && len(args) >= 2 && args[1] == "show" {
		resolved := ResolveConfig(CLIOverrides{
			Endpoint:   *endpoint,
			Model:      *model,
			BaseURL:    *baseURL,
			APIKey:     *apiKey,
			Workspace:  *workspace,
			ConfigFile: *configFile,
		})
		fmt.Print(resolved.String())
		os.Exit(0)
	}

	name := strings.ToLower(args[0])

	p, ok := personas.Get(name)
	if !ok {
		fmt.Fprintf(os.Stderr, "Unknown persona: %q\n\n", name)
		printUsage()
		os.Exit(1)
	}

	// Optional custom prompt; otherwise use the persona's default.
	prompt := p.DefaultMessage()
	if len(args) >= 2 {
		prompt = strings.Join(args[1:], " ")
	}

	cfg := adk.NewLocalAgentConfig()
	cfg.Capabilities = adk.AllTools()
	cfg.Policies = []policy.Policy{policy.AllowAll()}
	cfg.EnablePlanningMode = false
	cfg.StructuredPrompt = p.Prompt()
	cfg.Verbose = *verbose

	// Resolve config: CLI flags > --config file > .zenith/config.yml > ~/.divmora/agents/zenith/config.yml
	resolved := ResolveConfig(CLIOverrides{
		Endpoint:   *endpoint,
		Model:      *model,
		BaseURL:    *baseURL,
		APIKey:     *apiKey,
		Workspace:  *workspace,
		ConfigFile: *configFile,
	})

	// Apply per-persona overrides (e.g., bolt uses a different model)
	resolved = resolved.ForPersona(name)

	// Validate config before applying
	if err := resolved.Validate(); err != nil {
		log.Fatal(err)
	}

	// Apply resolved config to SDK
	if err := resolved.ApplyTo(cfg); err != nil {
		log.Fatal(err)
	}

	agent, err := adk.NewAgent(cfg)
	if err != nil {
		log.Fatalf("Failed to create agent: %v", err)
	}

	ctx := context.Background()
	if err := agent.Start(ctx); err != nil {
		log.Fatalf("Failed to start: %v", err)
	}
	defer agent.Close()

	endpointName := resolved.Endpoint
	if endpointName == "" {
		endpointName = "default (litellm.json)"
	}
	fmt.Printf("🏔️  Zenith — %s\n", p.Description())
	fmt.Printf("🤖 Endpoint: %s | Model: %s\n", endpointName, resolved.Model)
	fmt.Printf("📎 Conversation ID: %s\n", agent.ConversationID())
	fmt.Printf("📤 Prompt: %s\n\n", prompt)

	resp, err := agent.Chat(ctx, prompt)
	if err != nil {
		log.Fatalf("Chat failed: %v", err)
	}

	fmt.Println(resp.Text)

	fmt.Printf("\n━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━\n")
	fmt.Printf("Steps: %d\n", len(resp.Steps))
	if resp.Usage != nil {
		fmt.Printf("Tokens: prompt=%d, completion=%d, total=%d\n",
			resp.Usage.PromptTokens, resp.Usage.CompletionTokens, resp.Usage.TotalTokens)
	}
}


func printUsage() {
	fmt.Fprintf(os.Stderr, `Zenith — AI-powered codebase improvement agent.

Usage: zenith [flags] <persona> [prompt]
       zenith config show                     Show resolved config and exit

  Note: Flags must come BEFORE the persona name.

Flags:
  --workspace=<path>   Workspace directory (default: cwd)
  --endpoint=<name>    LiteLLM endpoint name (default: from litellm.json)
  --model=<name>       Model name (default: endpoint-specific)
  --base-url=<url>     Base URL override
  --api-key=<key>      API key override
  --config=<path>      Path to YAML config file (overrides auto-discovery)
  --verbose, -v        Enable verbose debug logging
  --help, -h           Show this help

Environment:
  LITELLM_API_KEY      LiteLLM API key (if inline provider config used)
  LOCALHARNESS_BIN     Optional. Path to localharness binary (default: auto-detect).

Config files (YAML):
  .zenith/config.yml          Per-workspace config (highest priority)
  ~/.divmora/agents/zenith/config.yml  Global config

  Example config.yml:
    endpoint: gemini
    model: gemini-3.5-flash

  Priority: CLI flags > env vars > workspace config > global config

Available personas:
`)
	all := personas.All()
	names := make([]string, 0, len(all))
	for n := range all {
		names = append(names, n)
	}
	sort.Strings(names)

	for _, n := range names {
		fmt.Fprintf(os.Stderr, "  %-20s %s\n", n, all[n].Description())
	}

	fmt.Fprintf(os.Stderr, `
Examples:
  zenith bolt                                          # Default endpoint
  zenith --model=gemini-2.5-pro bolt                   # Specific model override
  zenith --endpoint=openai --model=gpt-4o sentinel     # OpenAI endpoint
  zenith --endpoint=ollama --model=llama3 bolt         # Local Ollama endpoint
  zenith --workspace=/path/to/project sentinel
  LOCALHARNESS_BIN=./bin/localharness zenith bolt
`)
}

