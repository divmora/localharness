// Package sdk provides the Go ADK for the LocalHarness agent runtime.
//
// The SDK manages the lifecycle of a localharness binary process, communicates
// over WebSocket + Protobuf, and provides a high-level Agent API for
// sending prompts and receiving responses.
//
// Usage:
//
//	cfg := adk.NewLocalAgentConfig()
//	cfg.LitellmAPIKey = os.Getenv("LITELLM_API_KEY")
//	cfg.Workspaces = []adk.WorkspaceDef{{Directory: "/path/to/project"}}
//	cfg.Policies = policy.ConfirmRunCommand()
//
//	agent, err := adk.NewAgent(cfg)
//	// ... use agent.Chat(ctx, "prompt") ...
package adk
