package adk

import (
	"context"
	"fmt"
	"strings"
	"testing"

	"github.com/divmora/localharness/adk/connection"
	"github.com/divmora/localharness/adk/policy"
)

func TestAgent_HostToolCall(t *testing.T) {
	var calledWith map[string]any

	cfg := &LocalAgentConfig{
		Policies:     []policy.Policy{},
		HostTools: []HostToolDef{
			{
				Name:        "get_weather",
				Description: "Get weather for a city",
				Parameters: map[string]any{
					"type": "object",
					"properties": map[string]any{
						"city": map[string]any{"type": "string"},
					},
					"required": []string{"city"},
				},
				Handler: func(ctx context.Context, args map[string]any) (any, error) {
					calledWith = args
					return map[string]string{
						"city":        args["city"].(string),
						"temperature": "22°C",
						"condition":   "Sunny",
					}, nil
				},
			},
		},
	}

	agent, err := NewAgent(cfg)
	if err != nil {
		t.Fatalf("NewAgent failed: %v", err)
	}

	mock := &mockConnectionWithToolResult{
		mockConnection: mockConnection{
			conversationID: "conv-host-tool",
			steps: []connection.Step{
				{
					Index:          1,
					ToolName:       "get_weather",
					ToolArgsJSON:   `{"city":"Tokyo"}`,
					State:          connection.StateWaiting,
					Source:         connection.SourceModel,
					IsHostToolCall: true,
				},
				{
					Index:   2,
					Text:    "The weather in Tokyo is 22°C and Sunny.",
					State:   connection.StateDone,
					Source:   connection.SourceModel,
					IsFinal: true,
				},
			},
		},
	}
	agent.conn = mock
	agent.started = true

	resp, err := agent.Chat(context.Background(), "What's the weather in Tokyo?")
	if err != nil {
		t.Fatalf("Chat failed: %v", err)
	}

	// Verify handler was called with correct args
	if calledWith == nil {
		t.Fatal("expected handler to be called")
	}
	if city, _ := calledWith["city"].(string); city != "Tokyo" {
		t.Errorf("expected city 'Tokyo', got %q", city)
	}

	// Verify result was sent back
	if !mock.toolResultSent {
		t.Fatal("expected tool result to be sent")
	}
	if mock.sentToolName != "get_weather" {
		t.Errorf("expected tool name 'get_weather', got %q", mock.sentToolName)
	}
	if mock.sentIsError {
		t.Error("expected tool result to not be an error")
	}
	if !strings.Contains(mock.sentResultJSON, "22°C") {
		t.Errorf("expected result to contain '22°C', got %q", mock.sentResultJSON)
	}

	if resp.Text != "The weather in Tokyo is 22°C and Sunny." {
		t.Errorf("unexpected response: %q", resp.Text)
	}
}

func TestAgent_HostToolCallError(t *testing.T) {
	cfg := &LocalAgentConfig{
		Policies:     []policy.Policy{},
		HostTools: []HostToolDef{
			{
				Name:        "failing_tool",
				Description: "A tool that always fails",
				Handler: func(ctx context.Context, args map[string]any) (any, error) {
					return nil, fmt.Errorf("database connection timeout")
				},
			},
		},
	}

	agent, err := NewAgent(cfg)
	if err != nil {
		t.Fatalf("NewAgent failed: %v", err)
	}

	mock := &mockConnectionWithToolResult{
		mockConnection: mockConnection{
			conversationID: "conv-host-tool-err",
			steps: []connection.Step{
				{
					Index:          1,
					ToolName:       "failing_tool",
					ToolArgsJSON:   `{}`,
					State:          connection.StateWaiting,
					Source:         connection.SourceModel,
					IsHostToolCall: true,
				},
				{
					Index:   2,
					Text:    "The tool failed.",
					State:   connection.StateDone,
					Source:   connection.SourceModel,
					IsFinal: true,
				},
			},
		},
	}
	agent.conn = mock
	agent.started = true

	_, err = agent.Chat(context.Background(), "run failing tool")
	if err != nil {
		t.Fatalf("Chat failed: %v", err)
	}

	if !mock.toolResultSent {
		t.Fatal("expected tool result to be sent")
	}
	if !mock.sentIsError {
		t.Error("expected tool result to be an error")
	}
	if !strings.Contains(mock.sentResultJSON, "database connection timeout") {
		t.Errorf("expected error message in result, got %q", mock.sentResultJSON)
	}
}

func TestAgent_HostToolCallNoHandler(t *testing.T) {
	cfg := &LocalAgentConfig{
		Policies:     []policy.Policy{},
	}

	agent, err := NewAgent(cfg)
	if err != nil {
		t.Fatalf("NewAgent failed: %v", err)
	}

	mock := &mockConnectionWithToolResult{
		mockConnection: mockConnection{
			conversationID: "conv-host-tool-nohandler",
			steps: []connection.Step{
				{
					Index:          1,
					ToolName:       "unknown_tool",
					ToolArgsJSON:   `{}`,
					State:          connection.StateWaiting,
					Source:         connection.SourceModel,
					IsHostToolCall: true,
				},
				{
					Index:   2,
					Text:    "Tool not found.",
					State:   connection.StateDone,
					Source:   connection.SourceModel,
					IsFinal: true,
				},
			},
		},
	}
	agent.conn = mock
	agent.started = true

	_, err = agent.Chat(context.Background(), "use unknown tool")
	if err != nil {
		t.Fatalf("Chat failed: %v", err)
	}

	if !mock.toolResultSent {
		t.Fatal("expected error tool result to be sent for unknown tool")
	}
	if !mock.sentIsError {
		t.Error("expected tool result to be an error")
	}
	if !strings.Contains(mock.sentResultJSON, "no handler registered") {
		t.Errorf("expected 'no handler registered' in result, got %q", mock.sentResultJSON)
	}
}

// mockConnectionWithToolResult extends mockConnection to capture SendToolResult calls.
type mockConnectionWithToolResult struct {
	mockConnection
	toolResultSent bool
	sentStepID     string
	sentToolName   string
	sentResultJSON string
	sentIsError    bool
}

func (m *mockConnectionWithToolResult) SendToolResult(ctx context.Context, stepID, toolName, resultJSON string, isError bool) error {
	m.toolResultSent = true
	m.sentStepID = stepID
	m.sentToolName = toolName
	m.sentResultJSON = resultJSON
	m.sentIsError = isError
	return nil
}
