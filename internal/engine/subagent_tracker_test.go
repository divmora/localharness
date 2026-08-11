package engine

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/divmora/localharness/internal/tools"
)

func TestTracker_Register_and_List(t *testing.T) {
	tracker := NewSubagentTracker(nil)

	inst := &SubagentInstance{
		ConversationID: "conv-1",
		TypeName:       "research",
		Role:           "Codebase Researcher",
		State:          SubagentStateRunning,
		Inbox:          make(chan string, 10),
		StartedAt:      time.Now(),
	}
	tracker.Register(inst)

	if tracker.ActiveCount() != 1 {
		t.Errorf("expected 1 active, got %d", tracker.ActiveCount())
	}

	list := tracker.List()
	if len(list) != 1 {
		t.Fatalf("expected 1 instance, got %d", len(list))
	}
	if list[0].ConversationID != "conv-1" {
		t.Errorf("expected conv-1, got %q", list[0].ConversationID)
	}
}

func TestTracker_Get(t *testing.T) {
	tracker := NewSubagentTracker(nil)
	tracker.Register(&SubagentInstance{
		ConversationID: "conv-1",
		TypeName:       "self",
		Inbox:          make(chan string, 10),
	})

	inst, ok := tracker.Get("conv-1")
	if !ok {
		t.Fatal("expected to find conv-1")
	}
	if inst.TypeName != "self" {
		t.Errorf("expected type 'self', got %q", inst.TypeName)
	}

	_, ok = tracker.Get("nonexistent")
	if ok {
		t.Error("should not find nonexistent")
	}
}

func TestTracker_Kill_Single(t *testing.T) {
	tracker := NewSubagentTracker(nil)
	ctx, cancel := context.WithCancel(context.Background())
	_ = ctx

	tracker.Register(&SubagentInstance{
		ConversationID: "conv-1",
		TypeName:       "research",
		Cancel:         cancel,
		Inbox:          make(chan string, 10),
	})
	tracker.Register(&SubagentInstance{
		ConversationID: "conv-2",
		TypeName:       "self",
		Inbox:          make(chan string, 10),
	})

	err := tracker.Kill("conv-1")
	if err != nil {
		t.Fatalf("Kill failed: %v", err)
	}

	if tracker.ActiveCount() != 1 {
		t.Errorf("expected 1 active after kill, got %d", tracker.ActiveCount())
	}

	_, ok := tracker.Get("conv-1")
	if ok {
		t.Error("killed instance should be removed")
	}
}

func TestTracker_Kill_NotFound(t *testing.T) {
	tracker := NewSubagentTracker(nil)
	err := tracker.Kill("nonexistent")
	if err == nil {
		t.Error("expected error for nonexistent kill")
	}
}

func TestTracker_KillAll(t *testing.T) {
	tracker := NewSubagentTracker(nil)
	_, cancel1 := context.WithCancel(context.Background())
	_, cancel2 := context.WithCancel(context.Background())

	tracker.Register(&SubagentInstance{
		ConversationID: "conv-1",
		Cancel:         cancel1,
		Inbox:          make(chan string, 10),
	})
	tracker.Register(&SubagentInstance{
		ConversationID: "conv-2",
		Cancel:         cancel2,
		Inbox:          make(chan string, 10),
	})

	count := tracker.KillAll()
	if count != 2 {
		t.Errorf("expected 2 killed, got %d", count)
	}
	if tracker.ActiveCount() != 0 {
		t.Errorf("expected 0 active after kill_all, got %d", tracker.ActiveCount())
	}
}

func TestTracker_SendMessage(t *testing.T) {
	tracker := NewSubagentTracker(nil)
	inbox := make(chan string, 10)

	tracker.Register(&SubagentInstance{
		ConversationID: "conv-1",
		Inbox:          inbox,
	})

	err := tracker.SendMessage("conv-1", "Hello from parent")
	if err != nil {
		t.Fatalf("SendMessage failed: %v", err)
	}

	select {
	case msg := <-inbox:
		if msg != "Hello from parent" {
			t.Errorf("expected 'Hello from parent', got %q", msg)
		}
	default:
		t.Error("expected message in inbox")
	}
}

func TestTracker_SendMessage_NotFound(t *testing.T) {
	tracker := NewSubagentTracker(nil)
	err := tracker.SendMessage("nonexistent", "hello")
	if err == nil {
		t.Error("expected error for nonexistent recipient")
	}
}

func TestTracker_SendMessage_FullInbox(t *testing.T) {
	tracker := NewSubagentTracker(nil)
	inbox := make(chan string, 1) // capacity 1
	inbox <- "already full"

	tracker.Register(&SubagentInstance{
		ConversationID: "conv-1",
		Inbox:          inbox,
	})

	err := tracker.SendMessage("conv-1", "overflow")
	if err == nil {
		t.Error("expected error for full inbox")
	}
}

func TestTracker_NotifyParent(t *testing.T) {
	notifyCh := make(chan tools.SystemMessage, 10)
	tracker := NewSubagentTracker(notifyCh)

	tracker.NotifyParent(tools.SystemMessage{
		Source:  "subagent_complete",
		TaskID:  "conv-1",
		Content: "Subagent conv-1 completed",
	})

	select {
	case msg := <-notifyCh:
		if msg.Content != "Subagent conv-1 completed" {
			t.Errorf("expected completion message, got %q", msg.Content)
		}
	default:
		t.Error("expected notification in parent channel")
	}
}

func TestTracker_Remove(t *testing.T) {
	tracker := NewSubagentTracker(nil)
	tracker.Register(&SubagentInstance{
		ConversationID: "conv-1",
		Inbox:          make(chan string, 10),
	})

	tracker.Remove("conv-1")
	if tracker.ActiveCount() != 0 {
		t.Errorf("expected 0 after remove, got %d", tracker.ActiveCount())
	}
}

func TestTracker_ConcurrentAccess(t *testing.T) {
	tracker := NewSubagentTracker(nil)

	var wg sync.WaitGroup
	// Concurrent registers
	for i := 0; i < 50; i++ {
		wg.Add(1)
		go func(n int) {
			defer wg.Done()
			tracker.Register(&SubagentInstance{
				ConversationID: "conv-" + string(rune('A'+n%26)) + string(rune('0'+n/26)),
				Inbox:          make(chan string, 10),
			})
		}(i)
	}
	// Concurrent reads
	for i := 0; i < 50; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_ = tracker.List()
			_ = tracker.ActiveCount()
		}()
	}
	wg.Wait()

	if tracker.ActiveCount() < 1 {
		t.Error("expected at least some registered instances")
	}
}

func TestSubagentState_String(t *testing.T) {
	tests := []struct {
		state SubagentState
		want  string
	}{
		{SubagentStateRunning, "running"},
		{SubagentStateIdle, "idle"},
		{SubagentStateError, "error"},
		{SubagentStateKilled, "killed"},
		{SubagentState(99), "unknown"},
	}
	for _, tt := range tests {
		if got := tt.state.String(); got != tt.want {
			t.Errorf("SubagentState(%d).String() = %q, want %q", tt.state, got, tt.want)
		}
	}
}
