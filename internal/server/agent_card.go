package server

import (
	"encoding/json"
	"net/http"

	"github.com/divmora/localharness/internal/config"
)

// AgentCard is the A2A (Agent-to-Agent) discovery document.
// It follows the Google A2A protocol specification and is served at
// /.well-known/agent.json to allow other agents and clients to
// discover capabilities, skills, and interaction requirements.
type AgentCard struct {
	// Name is the display name of the agent.
	Name string `json:"name"`

	// Description summarizes what the agent does.
	Description string `json:"description,omitempty"`

	// Version is the agent software version.
	Version string `json:"version"`

	// URL is the primary service endpoint.
	URL string `json:"url,omitempty"`

	// DocumentationURL links to further documentation.
	DocumentationURL string `json:"documentationUrl,omitempty"`

	// Provider describes the entity providing the agent.
	Provider *AgentCardProvider `json:"provider,omitempty"`

	// Capabilities advertises supported protocol features.
	Capabilities AgentCardCapabilities `json:"capabilities"`

	// DefaultInputModes lists accepted MIME types.
	DefaultInputModes []string `json:"defaultInputModes"`

	// DefaultOutputModes lists produced MIME types.
	DefaultOutputModes []string `json:"defaultOutputModes"`

	// Skills lists specific tasks the agent can perform.
	Skills []AgentCardSkill `json:"skills,omitempty"`

	// Authentication describes required auth schemes.
	Authentication *AgentCardAuth `json:"authentication,omitempty"`
}

// AgentCardProvider describes the entity providing the agent.
type AgentCardProvider struct {
	Name string `json:"name"`
	URL  string `json:"url,omitempty"`
}

// AgentCardCapabilities advertises supported protocol features.
type AgentCardCapabilities struct {
	// Streaming indicates support for SSE/streaming responses.
	Streaming bool `json:"streaming"`

	// PushNotifications indicates support for push notifications.
	PushNotifications bool `json:"pushNotifications"`

	// StateTransitionHistory indicates step history is available.
	StateTransitionHistory bool `json:"stateTransitionHistory"`

	// A2AVersion is the supported A2A protocol version.
	A2AVersion string `json:"a2aVersion,omitempty"`
}

// AgentCardSkill describes a specific task the agent can perform.
type AgentCardSkill struct {
	// ID is a unique identifier for the skill.
	ID string `json:"id"`

	// Name is a human-readable name.
	Name string `json:"name"`

	// Description explains what the skill does.
	Description string `json:"description,omitempty"`

	// Tags are keywords for categorization.
	Tags []string `json:"tags,omitempty"`

	// Examples are sample prompts or inputs.
	Examples []string `json:"examples,omitempty"`

	// InputModes overrides default input MIME types.
	InputModes []string `json:"inputModes,omitempty"`

	// OutputModes overrides default output MIME types.
	OutputModes []string `json:"outputModes,omitempty"`
}

// AgentCardAuth describes authentication requirements.
type AgentCardAuth struct {
	// Schemes lists supported auth schemes.
	Schemes []string `json:"schemes"`
}

// DefaultAgentCard returns the default agent card for the localharness.
// It reflects the current binary's capabilities.
func DefaultAgentCard() *AgentCard {
	return &AgentCard{
		Name:        "LocalHarness Agent",
		Description: "An agentic AI coding assistant powered by LocalHarness. Supports file I/O, shell commands, web search, subagents, and interactive tools over WebSocket + Protobuf.",
		Version:     config.HarnessVersion,
		Provider: &AgentCardProvider{
			Name: "Divmora",
			URL:  "https://github.com/divmora/localharness",
		},
		Capabilities: AgentCardCapabilities{
			Streaming:              true,
			PushNotifications:      false,
			StateTransitionHistory: true,
			A2AVersion:             "0.2.0",
		},
		DefaultInputModes:  []string{"text/plain"},
		DefaultOutputModes: []string{"text/plain", "application/json"},
		Skills: []AgentCardSkill{
			{
				ID:          "code-editing",
				Name:        "Code Editing",
				Description: "View, create, and edit files with targeted search-and-replace.",
				Tags:        []string{"code", "files", "editing"},
				Examples:    []string{"Fix the bug in handler.go", "Create a new test file"},
			},
			{
				ID:          "code-search",
				Name:        "Code Search & Navigation",
				Description: "Search files by content (grep) or name pattern, list directories.",
				Tags:        []string{"search", "navigation", "grep"},
				Examples:    []string{"Find all usages of handleRequest", "List the project structure"},
			},
			{
				ID:          "shell-execution",
				Name:        "Shell Command Execution",
				Description: "Run shell commands with sync, background, and persistent terminal modes.",
				Tags:        []string{"shell", "commands", "terminal"},
				Examples:    []string{"Run the test suite", "Build the project"},
			},
			{
				ID:          "web-research",
				Name:        "Web Research",
				Description: "Search the web and fetch page contents for research.",
				Tags:        []string{"web", "search", "fetch"},
				Examples:    []string{"Search for Go best practices", "Fetch the API docs"},
			},
			{
				ID:          "task-management",
				Name:        "Background Task Management",
				Description: "Run background tasks, manage persistent terminals, schedule timers and cron jobs.",
				Tags:        []string{"tasks", "background", "scheduling"},
				Examples:    []string{"Run the server in the background", "Set a reminder in 5 minutes"},
			},
			{
				ID:          "agent-delegation",
				Name:        "Sub-Agent Delegation",
				Description: "Spawn child agents with isolated context for parallel subtasks.",
				Tags:        []string{"subagent", "delegation", "parallel"},
				Examples:    []string{"Research this topic while I continue coding"},
			},
			{
				ID:          "browser-testing",
				Name:        "Browser Automation & Testing",
				Description: "Navigate websites, interact with elements, take screenshots, and verify UI using Playwright.",
				Tags:        []string{"browser", "testing", "playwright", "automation"},
				Examples:    []string{"Open the app and check if the login page loads", "Take a screenshot of the dashboard"},
			},
		},
		Authentication: &AgentCardAuth{
			Schemes: []string{"apiKey"},
		},
	}
}

// handleAgentCard serves the A2A agent card at /.well-known/agent.json.
func (s *Server) handleAgentCard(w http.ResponseWriter, r *http.Request) {
	card := s.agentCard
	if card == nil {
		card = DefaultAgentCard()
	}

	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Access-Control-Allow-Origin", "*")
	w.WriteHeader(http.StatusOK)

	enc := json.NewEncoder(w)
	enc.SetIndent("", "  ")
	if err := enc.Encode(card); err != nil {
		s.logger.Error("failed to encode agent card", "error", err)
	}
}
