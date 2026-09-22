package engine

import (
	"strings"

	"github.com/divmora/localharness/internal/llm"
)

// ModelTier represents an abstract model performance and cost tier.
type ModelTier string

const (
	// ModelTierInherit indicates the subagent should use the exact model of the parent engine.
	ModelTierInherit ModelTier = "inherit"

	// ModelTierFlashLite indicates a lightweight, low-latency, lowest-cost model.
	// (e.g., gemini-2.0-flash-lite, claude-3-5-haiku, gpt-4o-mini, deepseek-chat)
	ModelTierFlashLite ModelTier = "flash_lite"

	// ModelTierFlash indicates a fast, cost-effective model suitable for research, summarization, and workers.
	// (e.g., gemini-2.5-flash, claude-3-5-haiku, gpt-4o-mini, deepseek-chat)
	ModelTierFlash ModelTier = "flash"

	// ModelTierPro indicates a high-reasoning, flagship model for complex multi-step planning or synthesis.
	// (e.g., gemini-2.5-pro, claude-3-5-sonnet, gpt-4o, deepseek-reasoner)
	ModelTierPro ModelTier = "pro"
)

// ModelTierResolver maps a parent model name and requested tier (or explicit model) to a concrete model name.
type ModelTierResolver func(parentModel string, requestedTierOrModel string) string

// DefaultModelTierResolver maps standard tiers ("inherit", "flash_lite", "flash", "pro") to
// concrete model names based on the parent model's family. If an explicit model name is provided
// (not matching any tier keyword), it is returned verbatim.
func DefaultModelTierResolver(parentModel string, requestedTierOrModel string) string {
	req := strings.TrimSpace(requestedTierOrModel)
	if req == "" || strings.EqualFold(req, string(ModelTierInherit)) {
		return parentModel
	}

	lowerReq := strings.ToLower(req)
	tier := ModelTier(lowerReq)

	// If not one of the standard tiers, treat as an explicit model name
	switch tier {
	case ModelTierFlashLite, ModelTierFlash, ModelTierPro:
		// Known tier — resolve by parent family below
	default:
		return req
	}

	lowerParent := strings.ToLower(parentModel)

	// 1. Google Gemini family
	if strings.Contains(lowerParent, "gemini") {
		switch tier {
		case ModelTierFlashLite:
			return "gemini-2.0-flash-lite"
		case ModelTierFlash:
			return "gemini-2.5-flash"
		case ModelTierPro:
			return "gemini-2.5-pro"
		}
	}

	// 2. Anthropic Claude family
	if strings.Contains(lowerParent, "claude") {
		switch tier {
		case ModelTierFlashLite, ModelTierFlash:
			return "claude-3-5-haiku"
		case ModelTierPro:
			return "claude-3-5-sonnet"
		}
	}

	// 3. DeepSeek family
	if strings.Contains(lowerParent, "deepseek") {
		switch tier {
		case ModelTierFlashLite, ModelTierFlash:
			return "deepseek-chat"
		case ModelTierPro:
			return "deepseek-reasoner"
		}
	}

	// 4. OpenAI / ChatGPT family (gpt-*, o1, o3, etc.)
	if strings.Contains(lowerParent, "gpt") ||
		strings.Contains(lowerParent, "o1") ||
		strings.Contains(lowerParent, "o3") ||
		strings.Contains(lowerParent, "o4") ||
		strings.Contains(lowerParent, "chatgpt") {
		switch tier {
		case ModelTierFlashLite, ModelTierFlash:
			return "gpt-4o-mini"
		case ModelTierPro:
			return "gpt-4o"
		}
	}

	// 5. Generic / fallback if parent model is empty
	if parentModel == "" {
		switch tier {
		case ModelTierFlashLite, ModelTierFlash:
			return "gpt-4o-mini"
		case ModelTierPro:
			return "gpt-4o"
		}
	}

	// For custom local models, keep parentModel to avoid breaking local setups
	return parentModel
}

// ResolveSubagentProvider resolves the appropriate Provider for a subagent based on the requested
// tier/model and the parent provider.
func ResolveSubagentProvider(parent llm.Provider, requestedTierOrModel string, resolver ModelTierResolver) llm.Provider {
	if parent == nil {
		return nil
	}
	if resolver == nil {
		resolver = DefaultModelTierResolver
	}

	targetModel := resolver(parent.ModelName(), requestedTierOrModel)
	if targetModel == "" || targetModel == parent.ModelName() {
		return parent
	}

	if cloner, ok := parent.(llm.ModelCloner); ok {
		return cloner.WithModel(targetModel)
	}

	return parent
}

// ModelContextWindow returns the known maximum context window (in tokens) for a given model.
// Defaults to 128,000 for unknown models.
func ModelContextWindow(modelName string) int {
	lower := strings.ToLower(strings.TrimSpace(modelName))
	switch {
	case strings.Contains(lower, "gemini"):
		// Gemini 1.5, 2.0, 2.5 support 1M to 2M tokens
		return 1048576
	case strings.Contains(lower, "claude"):
		// Claude 3, 3.5, 3.7 support 200k tokens
		return 200000
	case strings.Contains(lower, "o1"), strings.Contains(lower, "o3"), strings.Contains(lower, "o4"):
		// OpenAI reasoning models support 200k tokens
		return 200000
	case strings.Contains(lower, "gpt-4"), strings.Contains(lower, "chatgpt"):
		// GPT-4o / GPT-4 Turbo support 128k tokens
		return 128000
	case strings.Contains(lower, "deepseek"):
		// DeepSeek V3 / R1 support 128k context
		return 128000
	case strings.Contains(lower, "llama-3.1"), strings.Contains(lower, "llama-3.2"), strings.Contains(lower, "llama-3.3"):
		// Llama 3.1+ supports 128k context
		return 128000
	case strings.Contains(lower, "qwen"):
		return 128000
	case strings.Contains(lower, "mistral"), strings.Contains(lower, "codestral"):
		return 128000
	default:
		return 128000
	}
}

// CalculateModelCompactionThreshold calculates context limits and a recommended
// compaction threshold (typically ~75% of context window, capped at 400,000 tokens).
func CalculateModelCompactionThreshold(modelName string) (contextWindow int, compactionThreshold int) {
	window := ModelContextWindow(modelName)
	if window >= 1000000 {
		return window, 400000
	}
	// Target 75% of context window for safe compaction before overflow
	threshold := int(float64(window) * 0.75)
	if threshold < 2000 {
		threshold = 2000
	}
	return window, threshold
}
