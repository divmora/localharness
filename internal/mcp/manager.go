// Package mcp implements the MCP (Model Context Protocol) bridge.
// It connects to external MCP servers via stdio or HTTP transport,
// discovers their tools, and makes them available to the engine.
// MCP tools are treated identically to built-in tools.
package mcp

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"os"
	"os/exec"
	"strings"
	"sync"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	pb "github.com/divmora/localharness/gen/go/localharness/v1"
	"github.com/divmora/localharness/internal/llm"
)

// Manager orchestrates connections to multiple MCP servers.
// It discovers tools from each server and makes them callable.
type Manager struct {
	sessions map[string]*serverSession  // name → live session
	tools    map[string]*toolEntry      // tool name → owning server
	logger   *slog.Logger
	mu       sync.RWMutex
}

// serverSession tracks a single MCP server connection.
type serverSession struct {
	name         string
	session      *mcp.ClientSession
	cancel       context.CancelFunc
	enabledTools map[string]bool // whitelist (nil = all)
}

// toolEntry maps a discovered tool name to its server and schema.
type toolEntry struct {
	serverName  string
	declaration llm.FunctionDeclaration
}

// NewManager creates a new MCP Manager.
func NewManager(logger *slog.Logger) *Manager {
	return &Manager{
		sessions: make(map[string]*serverSession),
		tools:    make(map[string]*toolEntry),
		logger:   logger,
	}
}

// Connect launches all configured MCP servers, performs the MCP handshake,
// and discovers their tools. Tool name conflicts with already-registered
// names cause an error.
func (m *Manager) Connect(ctx context.Context, servers []*pb.McpServerConfig) error {
	for _, cfg := range servers {
		if err := m.connectServer(ctx, cfg); err != nil {
			// Clean up any already-connected servers
			m.Close()
			return fmt.Errorf("mcp: failed to connect server %q: %w", cfg.Name, err)
		}
	}
	return nil
}

// connectServer connects to a single MCP server and discovers its tools.
func (m *Manager) connectServer(ctx context.Context, cfg *pb.McpServerConfig) error {
	m.logger.Info("connecting to MCP server", "name", cfg.Name)

	// Create MCP client
	client := mcp.NewClient(&mcp.Implementation{
		Name:    "localharness",
		Version: "1.0.0",
	}, nil)

	// Build transport
	var transport mcp.Transport
	switch t := cfg.Transport.(type) {
	case *pb.McpServerConfig_Stdio:
		cmd := exec.CommandContext(ctx, t.Stdio.Command, t.Stdio.Args...)
		// Set environment variables
		if len(cfg.Env) > 0 {
			cmd.Env = append(os.Environ(), mapToEnvSlice(cfg.Env)...)
		}
		cmd.Stderr = os.Stderr // Forward server stderr for debugging
		transport = &mcp.CommandTransport{Command: cmd}

	case *pb.McpServerConfig_Http:
		transport = &mcp.StreamableClientTransport{Endpoint: t.Http.Url}

	default:
		return fmt.Errorf("no transport configured")
	}

	// Connect
	serverCtx, cancel := context.WithCancel(ctx)
	session, err := client.Connect(serverCtx, transport, nil)
	if err != nil {
		cancel()
		return fmt.Errorf("connect: %w", err)
	}

	m.logger.Info("MCP server connected", "name", cfg.Name)

	// Build enabled tools whitelist
	var enabledTools map[string]bool
	if len(cfg.EnabledTools) > 0 {
		enabledTools = make(map[string]bool, len(cfg.EnabledTools))
		for _, t := range cfg.EnabledTools {
			enabledTools[t] = true
		}
	}

	// Store session
	m.mu.Lock()
	m.sessions[cfg.Name] = &serverSession{
		name:         cfg.Name,
		session:      session,
		cancel:       cancel,
		enabledTools: enabledTools,
	}
	m.mu.Unlock()

	// Discover tools
	if err := m.discoverTools(ctx, cfg.Name, session, enabledTools); err != nil {
		m.logger.Error("failed to discover tools", "server", cfg.Name, "error", err)
		return fmt.Errorf("discover tools: %w", err)
	}

	return nil
}

// discoverTools queries the MCP server for its tools and registers them.
func (m *Manager) discoverTools(ctx context.Context, serverName string, session *mcp.ClientSession, enabled map[string]bool) error {
	result, err := session.ListTools(ctx, nil)
	if err != nil {
		return fmt.Errorf("list tools: %w", err)
	}

	m.mu.Lock()
	defer m.mu.Unlock()

	for _, tool := range result.Tools {
		// Apply whitelist filter
		if enabled != nil && !enabled[tool.Name] {
			m.logger.Debug("skipping non-whitelisted MCP tool", "server", serverName, "tool", tool.Name)
			continue
		}

		// Check for conflicts with already-registered tools
		if existing, ok := m.tools[tool.Name]; ok {
			return fmt.Errorf("tool name conflict: %q is already registered by server %q", tool.Name, existing.serverName)
		}

		// Convert MCP tool schema to our FunctionDeclaration
		params := convertInputSchema(tool.InputSchema)

		m.tools[tool.Name] = &toolEntry{
			serverName: serverName,
			declaration: llm.FunctionDeclaration{
				Name:        tool.Name,
				Description: tool.Description,
				Parameters:  params,
			},
		}

		m.logger.Info("registered MCP tool", "server", serverName, "tool", tool.Name)
	}

	return nil
}

// ToolDeclarations returns all discovered MCP tool schemas for LLM function calling.
func (m *Manager) ToolDeclarations() []llm.FunctionDeclaration {
	m.mu.RLock()
	defer m.mu.RUnlock()

	decls := make([]llm.FunctionDeclaration, 0, len(m.tools))
	for _, entry := range m.tools {
		decls = append(decls, entry.declaration)
	}
	return decls
}

// IsMCPTool checks if a tool name belongs to an MCP server.
func (m *Manager) IsMCPTool(name string) bool {
	m.mu.RLock()
	defer m.mu.RUnlock()
	_, ok := m.tools[name]
	return ok
}

// ServerName returns the MCP server that owns the given tool.
func (m *Manager) ServerName(toolName string) string {
	m.mu.RLock()
	defer m.mu.RUnlock()
	if entry, ok := m.tools[toolName]; ok {
		return entry.serverName
	}
	return ""
}

// CallTool dispatches a tool call to the MCP server that owns it.
// Returns the JSON result, whether it's an error, and any transport-level error.
func (m *Manager) CallTool(ctx context.Context, name string, args map[string]any) (resultJSON string, isError bool, err error) {
	m.mu.RLock()
	entry, ok := m.tools[name]
	if !ok {
		m.mu.RUnlock()
		return "", false, fmt.Errorf("unknown MCP tool: %s", name)
	}

	sess, ok := m.sessions[entry.serverName]
	if !ok {
		m.mu.RUnlock()
		return "", false, fmt.Errorf("MCP server %q not connected", entry.serverName)
	}
	m.mu.RUnlock()

	m.logger.Info("calling MCP tool", "server", entry.serverName, "tool", name)

	result, err := sess.session.CallTool(ctx, &mcp.CallToolParams{
		Name:      name,
		Arguments: args,
	})
	if err != nil {
		return "", false, fmt.Errorf("MCP CallTool %q: %w", name, err)
	}

	// Extract text content from result
	resultText := extractCallToolResult(result)

	return resultText, result.IsError, nil
}

// ToolCount returns the number of registered MCP tools.
func (m *Manager) ToolCount() int {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return len(m.tools)
}

// ServerCount returns the number of connected MCP servers.
func (m *Manager) ServerCount() int {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return len(m.sessions)
}

// Close shuts down all MCP server connections gracefully.
func (m *Manager) Close() {
	m.mu.Lock()
	defer m.mu.Unlock()

	for name, sess := range m.sessions {
		m.logger.Info("closing MCP server", "name", name)
		if err := sess.session.Close(); err != nil {
			m.logger.Warn("error closing MCP session", "name", name, "error", err)
		}
		sess.cancel()
	}

	m.sessions = make(map[string]*serverSession)
	m.tools = make(map[string]*toolEntry)
}

// --- Helpers ---

// convertInputSchema converts an MCP tool's InputSchema to our JSON Schema map.
func convertInputSchema(schema any) map[string]interface{} {
	if schema == nil {
		return map[string]interface{}{
			"type":       "object",
			"properties": map[string]interface{}{},
		}
	}

	// The MCP SDK returns InputSchema as a map[string]any from JSON unmarshaling
	if m, ok := schema.(map[string]any); ok {
		return m
	}

	// Try JSON round-trip for other types
	data, err := json.Marshal(schema)
	if err != nil {
		return map[string]interface{}{
			"type":       "object",
			"properties": map[string]interface{}{},
		}
	}

	var result map[string]interface{}
	if err := json.Unmarshal(data, &result); err != nil {
		return map[string]interface{}{
			"type":       "object",
			"properties": map[string]interface{}{},
		}
	}

	return result
}

// extractCallToolResult extracts text content from an MCP CallToolResult.
func extractCallToolResult(result *mcp.CallToolResult) string {
	if result == nil {
		return "{}"
	}

	var parts []string
	for _, content := range result.Content {
		switch c := content.(type) {
		case *mcp.TextContent:
			parts = append(parts, c.Text)
		default:
			// For non-text content, marshal to JSON
			data, err := json.Marshal(content)
			if err == nil {
				parts = append(parts, string(data))
			}
		}
	}

	if len(parts) == 0 {
		return "{}"
	}
	return strings.Join(parts, "\n")
}

// mapToEnvSlice converts a map to KEY=VALUE environment variable format.
func mapToEnvSlice(m map[string]string) []string {
	vars := make([]string, 0, len(m))
	for k, v := range m {
		vars = append(vars, k+"="+v)
	}
	return vars
}
