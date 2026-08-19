// Package engine — subagent type definitions.
//
// SubagentTypeDef describes a named subagent type that can be invoked
// by the agent. Types come from three sources, merged in priority order:
//
//  1. Built-in (LocalHarness core): "research", "self" — always available
//     unless excluded by SDK config.
//  2. SDK-registered (host app): custom types passed via HarnessConfig.
//     Override built-in types with the same name.
//  3. Agent-defined (runtime): created via the define_subagent tool during
//     a conversation. Override everything with the same name.
package engine

// SubagentTypeDef defines a named subagent type.
type SubagentTypeDef struct {
	Name                string // Unique identifier (e.g. "research", "self")
	Description         string // Human-readable description of what this subagent does
	SystemPrompt        string // System prompt for child engines of this type
	EnableWriteTools    bool   // create_file, edit_file, run_command, etc.
	EnableMCPTools      bool   // MCP server tools
	EnableSubagentTools bool   // define_subagent, invoke_subagent (recursive)
	IsBuiltin           bool   // true for LH-provided types (not serialized)
}

// builtinSubagentTypes are the default subagent types provided by LocalHarness.
// These are always available unless explicitly excluded by the SDK.
var builtinSubagentTypes = []SubagentTypeDef{
	{
		Name: "research",
		Description: "Research subagent with read-only tools for exploring the codebase, " +
			"searching the web, and reading files. Delegate to this agent when you need " +
			"to run a research task in the background while continuing other work (e.g., " +
			"coding, building, testing), when a research task requires many search and " +
			"file-reading steps that would clutter your context, or when you need a broad " +
			"survey of the codebase or documentation. Prefer doing research yourself for " +
			"quick, targeted lookups.",
		SystemPrompt: `You are a focused research assistant. Your job is to thoroughly investigate the codebase, documentation, and web resources to answer questions and gather information.

You have read-only access to the codebase. You cannot create, edit, or delete files, and you cannot run commands. Focus on:
1. Reading and understanding code structure
2. Finding relevant files and patterns
3. Searching the web for documentation and references
4. Providing clear, comprehensive summaries of your findings

Be thorough but efficient. Your response will be sent back to the parent agent as context.`,
		EnableWriteTools:    false,
		EnableMCPTools:      false,
		EnableSubagentTools: false,
		IsBuiltin:           true,
	},
	{
		Name: "self",
		Description: "Subagent that inherits the parent agent's full configuration " +
			"including tools, system prompt, and model. Use this when you need to run " +
			"a task in a separate conversation context but with the same capabilities " +
			"as the current agent.",
		SystemPrompt: "", // Empty = inherit parent's system prompt
		EnableWriteTools:    true,
		EnableMCPTools:      true,
		EnableSubagentTools: true,
		IsBuiltin:           true,
	},
	{
		Name: "manager",
		Description: "Manager subagent. This agent is designed to break down complex tasks into smaller sub-tasks, hire specialized subagents (using invoke_subagent), and compile their results. Delegate to this agent when you have a large project that requires coordination of multiple agents.",
		SystemPrompt: `You are the Manager subagent. Your role is to break down complex projects into discrete tasks and hire specialized subagents to complete them.

Guidelines:
1. First, analyze the prompt and break it down into 2-5 clear, distinct sub-tasks.
2. For each sub-task, determine the best agent type to handle it (e.g., 'research', 'self').
3. Use the invoke_subagent tool to hire subagents.
4. Wait for all subagents to finish their work. You will receive a summary of their results.
5. Compile the results from all subagents into a final, cohesive response to the original prompt.
6. Do not perform the work yourself unless absolutely necessary. Your primary job is delegation and coordination.`,
		EnableWriteTools:    false,
		EnableMCPTools:      false,
		EnableSubagentTools: true,
		IsBuiltin:           true,
	},
}

// BuiltinSubagentTypes returns a copy of the built-in subagent types.
func BuiltinSubagentTypes() []SubagentTypeDef {
	result := make([]SubagentTypeDef, len(builtinSubagentTypes))
	copy(result, builtinSubagentTypes)
	return result
}
