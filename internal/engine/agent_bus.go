package engine

import (
	"sync"
	"time"
)

// BusMessage is a lightweight notification shared between agents via the AgentBus.
// It contains metadata about an artifact or event — not the artifact content itself.
// Receiving agents use view_file to read the actual content if needed.
type BusMessage struct {
	From      string    `json:"from"`      // Agent role (e.g., "developer")
	FromID    string    `json:"from_id"`   // Conversation ID of sender
	Summary   string    `json:"summary"`   // Human-readable description
	Path      string    `json:"path"`      // Artifact path (optional)
	Tags      []string  `json:"tags"`      // For filtering/routing
	Timestamp time.Time `json:"timestamp"`
}

// AgentBus is an in-memory broadcast pub/sub bus shared across all engines in a family.
// All subagents within a single conversation tree share the same bus instance.
// Thread-safe for concurrent access from multiple goroutines.
type AgentBus struct {
	mu        sync.RWMutex
	messages  []BusMessage               // Ordered log (late joiners catch up via History)
	listeners map[string]chan BusMessage  // Keyed by conversation ID
}

// NewAgentBus creates a new broadcast bus.
func NewAgentBus() *AgentBus {
	return &AgentBus{
		listeners: make(map[string]chan BusMessage),
	}
}

// Subscribe registers an agent as a listener on the bus.
// Returns a channel for incoming messages and a copy of all past messages (for late joiners).
func (b *AgentBus) Subscribe(convID string) (<-chan BusMessage, []BusMessage) {
	b.mu.Lock()
	defer b.mu.Unlock()

	ch := make(chan BusMessage, 64)
	b.listeners[convID] = ch

	// Return copy of message history for catch-up
	history := make([]BusMessage, len(b.messages))
	copy(history, b.messages)

	return ch, history
}

// Unsubscribe removes an agent's listener and closes its channel.
// Safe to call multiple times.
func (b *AgentBus) Unsubscribe(convID string) {
	b.mu.Lock()
	defer b.mu.Unlock()

	if ch, ok := b.listeners[convID]; ok {
		close(ch)
		delete(b.listeners, convID)
	}
}

// Publish sends a message to all listeners except the sender.
// The message is appended to the history log for late joiners.
func (b *AgentBus) Publish(msg BusMessage) {
	b.mu.Lock()
	msg.Timestamp = time.Now().UTC()
	b.messages = append(b.messages, msg)

	// Snapshot listeners while holding the lock
	targets := make(map[string]chan BusMessage, len(b.listeners))
	for k, v := range b.listeners {
		targets[k] = v
	}
	b.mu.Unlock()

	// Send to all listeners except the sender (non-blocking)
	for convID, ch := range targets {
		if convID == msg.FromID {
			continue // Don't echo back to sender
		}
		select {
		case ch <- msg:
		default:
			// Listener buffer full — drop message.
			// Agent can catch up via History() on next turn.
		}
	}
}

// History returns a copy of all messages published so far.
func (b *AgentBus) History() []BusMessage {
	b.mu.RLock()
	defer b.mu.RUnlock()
	result := make([]BusMessage, len(b.messages))
	copy(result, b.messages)
	return result
}

// ListenerCount returns the number of active listeners.
func (b *AgentBus) ListenerCount() int {
	b.mu.RLock()
	defer b.mu.RUnlock()
	return len(b.listeners)
}
