package server

import (
	"context"
	"encoding/json"
	"log/slog"
	"testing"
	"time"

	pb "github.com/divmora/localharness/gen/go/localharness/v1"
	"github.com/divmora/localharness/internal/engine"
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
	if err := json.Unmarshal([]byte(tc.ArgsJson), &parsedArgs); err != nil {
		t.Fatalf("unmarshal tool call args: %v", err)
	}
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

func TestHandleCancel_NoActiveTurn(t *testing.T) {
	s := &Session{
		logger: slog.Default(),
	}
	// Calling handleCancel with no active turn should not panic and should be a safe no-op
	s.handleCancel()
}

func TestHandleCancel_IsolatesTurnCancellation(t *testing.T) {
	sessionCtx, sessionCancel := context.WithCancel(context.Background())
	defer sessionCancel()

	s := &Session{
		logger: slog.Default(),
		cancel: sessionCancel,
	}

	turnCtx, turnCancel := context.WithCancel(sessionCtx)
	s.turnCancelMu.Lock()
	s.currentTurnCancel = turnCancel
	s.turnCancelMu.Unlock()

	// Verify both contexts are active
	if turnCtx.Err() != nil {
		t.Fatalf("turn context should not be cancelled initially")
	}
	if sessionCtx.Err() != nil {
		t.Fatalf("session context should not be cancelled initially")
	}

	// Cancel current turn
	s.handleCancel()

	// Turn context must be cancelled
	if turnCtx.Err() != context.Canceled {
		t.Errorf("expected turn context to be cancelled, got %v", turnCtx.Err())
	}

	// Session context MUST remain alive
	if sessionCtx.Err() != nil {
		t.Errorf("session context must NOT be cancelled by handleCancel, got %v", sessionCtx.Err())
	}
}

func TestCleanup_CancelsActiveTurn(t *testing.T) {
	sessionCtx, sessionCancel := context.WithCancel(context.Background())
	defer sessionCancel()

	s := &Session{
		logger: slog.Default(),
		cancel: sessionCancel,
	}

	turnCtx, turnCancel := context.WithCancel(sessionCtx)
	s.turnCancelMu.Lock()
	s.currentTurnCancel = turnCancel
	s.turnCancelMu.Unlock()

	// Run cleanup
	s.cleanup()

	// Active turn must be cancelled by cleanup
	if turnCtx.Err() != context.Canceled {
		t.Errorf("expected turn context to be cancelled by cleanup, got %v", turnCtx.Err())
	}
}

func TestCleanup_UnblocksPendingQuestions(t *testing.T) {
	_, sessionCancel := context.WithCancel(context.Background())
	defer sessionCancel()

	s := &Session{
		logger:           slog.Default(),
		cancel:           sessionCancel,
		pendingQuestions: make(map[string]chan *pb.QuestionResponse),
	}

	ch := make(chan *pb.QuestionResponse, 1)
	s.pendingQuestions["q-123"] = ch

	doneCh := make(chan struct{})
	go func() {
		defer close(doneCh)
		s.cleanup()
	}()

	select {
	case resp := <-ch:
		if !resp.Skipped {
			t.Errorf("expected question response to be skipped, got: %+v", resp)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("cleanup timed out waiting to unblock pending question")
	}

	<-doneCh
}

func TestCleanup_UnblocksPendingPermissions(t *testing.T) {
	_, sessionCancel := context.WithCancel(context.Background())
	defer sessionCancel()

	s := &Session{
		logger:             slog.Default(),
		cancel:             sessionCancel,
		pendingPermissions: make(map[string]chan *pb.PermissionResponse),
	}

	ch := make(chan *pb.PermissionResponse, 1)
	s.pendingPermissions["perm-456"] = ch

	doneCh := make(chan struct{})
	go func() {
		defer close(doneCh)
		s.cleanup()
	}()

	select {
	case resp := <-ch:
		if resp.Approved {
			t.Errorf("expected permission response to be denied, got: %+v", resp)
		}
		if resp.DenialReason == "" {
			t.Errorf("expected denial reason to be set, got empty")
		}
	case <-time.After(2 * time.Second):
		t.Fatal("cleanup timed out waiting to unblock pending permission")
	}

	<-doneCh
}

func TestCleanup_UnblocksPendingToolResults(t *testing.T) {
	_, sessionCancel := context.WithCancel(context.Background())
	defer sessionCancel()

	s := &Session{
		logger:             slog.Default(),
		cancel:             sessionCancel,
		pendingToolResults: make(map[string]chan *pb.ToolResult),
	}

	ch := make(chan *pb.ToolResult, 1)
	s.pendingToolResults["step-789"] = ch

	doneCh := make(chan struct{})
	go func() {
		defer close(doneCh)
		s.cleanup()
	}()

	select {
	case res := <-ch:
		if !res.IsError {
			t.Errorf("expected tool result to be an error, got: %+v", res)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("cleanup timed out waiting to unblock pending tool result")
	}

	<-doneCh
}

func TestHandleSwitchModel(t *testing.T) {
	// 1. Session not initialized
	sUninit := &Session{
		logger: slog.Default(),
	}
	// Use a mock/channel to capture sent message
	// Since sUninit.conn is nil, sendServerMessage doesn't panic
	sUninit.handleSwitchModel(&pb.SwitchModelRequest{Model: "gpt-4o"})

	// 2. Initialized session with OpenAI provider
	p, err := llm.NewOpenAIProvider(llm.OpenAIConfig{
		BaseURL:   "http://127.0.0.1:4000/v1",
		APIKey:    "test-key",
		ModelName: "gpt-4o",
	}, nil)
	if err != nil {
		t.Fatalf("failed to create provider: %v", err)
	}

	eng := engine.NewEngine(engine.Config{
		Provider: p,
	})

	s := &Session{
		logger: slog.Default(),
		engine: eng,
		initCfg: &pb.HarnessConfig{
			LitellmBaseUrl: "http://127.0.0.1:4000/v1",
			LitellmApiKey:  "test-key",
			LitellmModel:   "gpt-4o",
		},
	}

	// Switch to claude-3-7-sonnet
	s.handleSwitchModel(&pb.SwitchModelRequest{
		Model: "claude-3-7-sonnet",
	})

	if s.engine.Provider().ModelName() != "claude-3-7-sonnet" {
		t.Errorf("expected engine model to be claude-3-7-sonnet, got %s", s.engine.Provider().ModelName())
	}
	if s.engine.CompactionThreshold() != 150000 {
		t.Errorf("expected compaction threshold 150000, got %d", s.engine.CompactionThreshold())
	}

	// Switch using tier alias "flash" (resolves to claude-3-5-haiku since parent is claude)
	s.handleSwitchModel(&pb.SwitchModelRequest{
		Model: "flash",
	})

	if s.engine.Provider().ModelName() != "claude-3-5-haiku" {
		t.Errorf("expected engine model to be claude-3-5-haiku, got %s", s.engine.Provider().ModelName())
	}
}
