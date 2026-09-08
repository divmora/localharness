package server

import (
	"encoding/json"
	"testing"

	pb "github.com/divmora/localharness/gen/go/localharness/v1"
	"github.com/divmora/localharness/internal/llm"
)

func TestMapProtoMessageToLLM(t *testing.T) {
	// 1. Text only message
	pm1 := &pb.ConversationMessage{
		Role:    "user",
		Content: "hello",
	}
	lm1 := mapProtoMessageToLLM(pm1)
	if lm1.Role != "user" || lm1.Content != "hello" {
		t.Errorf("expected user hello, got %v", lm1)
	}

	// 2. Message with tool calls
	args := map[string]interface{}{"path": "/a/b.txt", "content": "hi"}
	argsJSON, _ := json.Marshal(args)
	pm2 := &pb.ConversationMessage{
		Role: "model",
		ToolCalls: []*pb.ToolCallRecord{
			{
				CallId:   "call-1",
				Name:     "create_file",
				ArgsJson: string(argsJSON),
			},
		},
	}
	lm2 := mapProtoMessageToLLM(pm2)
	if lm2.Role != "model" || len(lm2.ToolCalls) != 1 {
		t.Fatalf("expected 1 tool call, got %v", lm2)
	}
	tc := lm2.ToolCalls[0]
	if tc.ID != "call-1" || tc.Name != "create_file" {
		t.Errorf("unexpected tool call metadata: %v", tc)
	}
	if tc.Args["path"] != "/a/b.txt" || tc.Args["content"] != "hi" {
		t.Errorf("unexpected tool call args: %v", tc.Args)
	}

	// 3. Message with tool result
	pm3 := &pb.ConversationMessage{
		Role: "tool",
		ToolResult: &pb.ToolResultRecord{
			CallId:  "call-1",
			Name:    "create_file",
			Content: `{"created": true}`,
			IsError: false,
		},
	}
	lm3 := mapProtoMessageToLLM(pm3)
	if lm3.Role != "tool" || lm3.ToolResult == nil {
		t.Fatalf("expected tool result, got %v", lm3)
	}
	tr := lm3.ToolResult
	if tr.CallID != "call-1" || tr.Name != "create_file" || tr.Content != `{"created": true}` || tr.IsError {
		t.Errorf("unexpected tool result: %v", tr)
	}
}

func TestMapLLMMessageToProto(t *testing.T) {
	// 1. Text only message
	lm1 := llm.Message{
		Role:    "user",
		Content: "hello",
	}
	pm1 := mapLLMMessageToProto(lm1)
	if pm1.Role != "user" || pm1.Content != "hello" {
		t.Errorf("expected user hello, got %v", pm1)
	}

	// 2. Message with tool calls
	args := map[string]interface{}{"path": "/a/b.txt", "content": "hi"}
	lm2 := llm.Message{
		Role: "model",
		ToolCalls: []llm.ToolCall{
			{
				ID:   "call-1",
				Name: "create_file",
				Args: args,
			},
		},
	}
	pm2 := mapLLMMessageToProto(lm2)
	if pm2.Role != "model" || len(pm2.ToolCalls) != 1 {
		t.Fatalf("expected 1 tool call, got %v", pm2)
	}
	tc := pm2.ToolCalls[0]
	if tc.CallId != "call-1" || tc.Name != "create_file" {
		t.Errorf("unexpected tool call metadata: %v", tc)
	}
	var parsedArgs map[string]interface{}
	json.Unmarshal([]byte(tc.ArgsJson), &parsedArgs)
	if parsedArgs["path"] != "/a/b.txt" || parsedArgs["content"] != "hi" {
		t.Errorf("unexpected tool call args: %v", parsedArgs)
	}

	// 3. Message with tool result
	lm3 := llm.Message{
		Role: "tool",
		ToolResult: &llm.ToolCallResult{
			CallID:  "call-1",
			Name:    "create_file",
			Content: `{"created": true}`,
			IsError: false,
		},
	}
	pm3 := mapLLMMessageToProto(lm3)
	if pm3.Role != "tool" || pm3.ToolResult == nil {
		t.Fatalf("expected tool result, got %v", pm3)
	}
	tr := pm3.ToolResult
	if tr.CallId != "call-1" || tr.Name != "create_file" || tr.Content != `{"created": true}` || tr.IsError {
		t.Errorf("unexpected tool result: %v", tr)
	}
}
