# Configuring LLM Providers

LocalHarness natively routes all LLM requests through **LiteLLM**. We no longer maintain custom provider integrations (like Anthropic, Gemini, Vertex AI, etc.) in the core Go codebase. Instead, you configure your providers either globally via `litellm.json` or programmatically via the ADK.

## 1. Global Configuration (Recommended)

The easiest way to add or manage providers is through the global LiteLLM configuration file located at:
`~/.divmora/config/litellm.json`

This file allows you to define multiple named endpoints. For example:

```json
{
  "defaultEndpoint": "cloudflare-llama",
  "endpoints": {
    "cloudflare-llama": {
      "baseUrl": "https://gateway.ai.cloudflare.com/v1/ACCOUNT_ID/GATEWAY_ID/openai",
      "apiKey": "YOUR_UPSTREAM_KEY",
      "defaultModel": "@cf/meta/llama-3-8b-instruct"
    },
    "local-ollama": {
      "baseUrl": "http://localhost:11434/v1",
      "apiKey": "ollama",
      "defaultModel": "llama3"
    },
    "openai-gpt4": {
      "baseUrl": "https://api.openai.com/v1",
      "apiKey": "sk-...",
      "defaultModel": "gpt-4o"
    }
  }
}
```

When an agent initializes, it will use the `defaultEndpoint` unless the ADK overrides it.

## 2. Using an Endpoint in the ADK

You can instruct the local-harness agent to use a specific endpoint defined in `litellm.json`:

```go
cfg := adk.NewLocalAgentConfig()
cfg.LitellmEndpoint = "local-ollama" // References the named endpoint in litellm.json
```

## 3. Inline Programmatic Configuration

If you do not want to rely on the global `litellm.json` file, or if you need to dynamically inject credentials at runtime, you can pass the configuration directly via the ADK. 

These fields will override any settings found in `litellm.json`.

```go
cfg := adk.NewLocalAgentConfig()
cfg.LitellmBaseURL = "https://api.anthropic.com/v1"
cfg.LitellmAPIKey  = os.Getenv("ANTHROPIC_API_KEY")
cfg.LitellmModel   = "claude-3-5-sonnet-20240620"

agent, err := adk.NewAgent(cfg)
```

## 4. Testclient Usage

When debugging with `lhctl` or `testclient`, you can omit provider-specific flags and instead ensure your `litellm.json` is properly configured. If you need to test a specific model or endpoint quickly:

```bash
# Uses the default endpoint in litellm.json
go run ./cmd/testclient --prompt "What is 2+2?"
```

*(Note: Legacy provider flags like `--provider=anthropic` have been deprecated.)*

