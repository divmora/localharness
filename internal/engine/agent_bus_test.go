package engine

import (
	"testing"
	"time"
)

func TestAgentBus_PublishBroadcast(t *testing.T) {
	bus := NewAgentBus()

	chA, _ := bus.Subscribe("agent-a")
	chB, _ := bus.Subscribe("agent-b")

	// Publish from agent-a
	bus.Publish(BusMessage{
		From:    "developer",
		FromID:  "agent-a",
		Summary: "Plan ready",
		Path:    "/brain/agent-a/plan.md",
		Tags:    []string{"plan"},
	})

	// Agent B should receive
	select {
	case msg := <-chB:
		if msg.Summary != "Plan ready" {
			t.Errorf("got summary %q, want %q", msg.Summary, "Plan ready")
		}
		if msg.From != "developer" {
			t.Errorf("got from %q, want %q", msg.From, "developer")
		}
	case <-time.After(100 * time.Millisecond):
		t.Error("agent B did not receive message")
	}

	// Agent A should NOT receive (sender is excluded)
	select {
	case msg := <-chA:
		t.Errorf("agent A should not receive own message, got: %+v", msg)
	case <-time.After(50 * time.Millisecond):
		// expected — sender doesn't get its own message
	}
}

func TestAgentBus_LateJoinerGetsHistory(t *testing.T) {
	bus := NewAgentBus()

	// Subscribe first agent and publish a message
	bus.Subscribe("agent-a")
	bus.Publish(BusMessage{
		From:    "developer",
		FromID:  "agent-a",
		Summary: "First message",
	})
	bus.Publish(BusMessage{
		From:    "developer",
		FromID:  "agent-a",
		Summary: "Second message",
	})

	// Late joiner subscribes after messages were published
	_, history := bus.Subscribe("agent-b")

	if len(history) != 2 {
		t.Errorf("late joiner got %d history messages, want 2", len(history))
	}
	if history[0].Summary != "First message" {
		t.Errorf("history[0] summary = %q, want %q", history[0].Summary, "First message")
	}
	if history[1].Summary != "Second message" {
		t.Errorf("history[1] summary = %q, want %q", history[1].Summary, "Second message")
	}
}

func TestAgentBus_Unsubscribe(t *testing.T) {
	bus := NewAgentBus()
	bus.Subscribe("agent-a")
	bus.Subscribe("agent-b")

	if bus.ListenerCount() != 2 {
		t.Errorf("listener count = %d, want 2", bus.ListenerCount())
	}

	bus.Unsubscribe("agent-a")

	if bus.ListenerCount() != 1 {
		t.Errorf("listener count = %d after unsubscribe, want 1", bus.ListenerCount())
	}

	// Unsubscribe again should be safe
	bus.Unsubscribe("agent-a")

	if bus.ListenerCount() != 1 {
		t.Errorf("listener count = %d after double unsubscribe, want 1", bus.ListenerCount())
	}
}

func TestAgentBus_PublishAfterUnsubscribe(t *testing.T) {
	bus := NewAgentBus()
	bus.Subscribe("agent-a")
	chB, _ := bus.Subscribe("agent-b")

	bus.Unsubscribe("agent-a")

	// Publishing should not panic even though agent-a is unsubscribed
	bus.Publish(BusMessage{
		From:    "reviewer",
		FromID:  "agent-b",
		Summary: "Review done",
	})

	// No one should receive (agent-b is the sender, agent-a unsubscribed)
	select {
	case msg := <-chB:
		t.Errorf("agent B should not receive own message, got: %+v", msg)
	case <-time.After(50 * time.Millisecond):
		// expected
	}
}

func TestAgentBus_History(t *testing.T) {
	bus := NewAgentBus()

	bus.Publish(BusMessage{Summary: "msg1", FromID: "x"})
	bus.Publish(BusMessage{Summary: "msg2", FromID: "y"})
	bus.Publish(BusMessage{Summary: "msg3", FromID: "z"})

	history := bus.History()
	if len(history) != 3 {
		t.Fatalf("history length = %d, want 3", len(history))
	}

	// Verify timestamps are set
	for i, msg := range history {
		if msg.Timestamp.IsZero() {
			t.Errorf("history[%d] has zero timestamp", i)
		}
	}
}
