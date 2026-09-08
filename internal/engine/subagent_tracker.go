// Package engine — subagent instance tracker.
//
// SubagentTracker manages the lifecycle of running subagent instances.
// It tracks active children, routes messages between agents, and handles
// kill/kill_all operations. Thread-safe for concurrent access.
package engine

import (
	"context"
	"fmt"
	"sync"
	"time"

	"github.com/divmora/localharness/internal/tools"
)

// SubagentState represents the current state of a subagent instance.
type SubagentState int

const (
	SubagentStateRunning SubagentState = iota
	SubagentStateIdle
	SubagentStateError
	SubagentStateKilled
)

// String returns a human-readable state name.
func (s SubagentState) String() string {
	switch s {
	case SubagentStateRunning:
		return "running"
	case SubagentStateIdle:
		return "idle"
	case SubagentStateError:
		return "error"
	case SubagentStateKilled:
		return "killed"
	default:
		return "unknown"
	}
}

// SubagentInstance represents a running subagent.
type SubagentInstance struct {
	ConversationID string
	TypeName       string
	Role           string
	State          SubagentState
	Engine         *Engine
	Cancel         context.CancelFunc
	Inbox          chan string // Messages from parent/peers → this agent
	StartedAt      time.Time
	Error          error

	mu sync.Mutex // Protects State and Error
}

// SetState atomically updates the instance state.
func (i *SubagentInstance) SetState(state SubagentState, err error) {
	i.mu.Lock()
	defer i.mu.Unlock()
	i.State = state
	if err != nil {
		i.Error = err
	}
}

// GetState returns the current state.
func (i *SubagentInstance) GetState() SubagentState {
	i.mu.Lock()
	defer i.mu.Unlock()
	return i.State
}

// SubagentTracker manages active subagent instances.
type SubagentTracker struct {
	mu        sync.RWMutex
	instances map[string]*SubagentInstance // conversationID → instance
	notifyCh  chan tools.SystemMessage     // Parent's notification channel
}

// NewSubagentTracker creates a new tracker.
// notifyCh is the parent engine's notification channel for receiving
// messages from children (completion notifications, send_message, etc.).
func NewSubagentTracker(notifyCh chan tools.SystemMessage) *SubagentTracker {
	return &SubagentTracker{
		instances: make(map[string]*SubagentInstance),
		notifyCh:  notifyCh,
	}
}

// Register adds a new subagent instance to the tracker.
func (t *SubagentTracker) Register(inst *SubagentInstance) {
	t.mu.Lock()
	defer t.mu.Unlock()
	t.instances[inst.ConversationID] = inst
}

// Get looks up a subagent instance by conversation ID.
func (t *SubagentTracker) Get(id string) (*SubagentInstance, bool) {
	t.mu.RLock()
	defer t.mu.RUnlock()
	inst, ok := t.instances[id]
	return inst, ok
}

// List returns all tracked subagent instances.
func (t *SubagentTracker) List() []*SubagentInstance {
	t.mu.RLock()
	defer t.mu.RUnlock()

	result := make([]*SubagentInstance, 0, len(t.instances))
	for _, inst := range t.instances {
		result = append(result, inst)
	}
	return result
}

// Kill cancels a specific subagent by conversation ID and removes it.
func (t *SubagentTracker) Kill(id string) error {
	t.mu.Lock()
	defer t.mu.Unlock()

	inst, ok := t.instances[id]
	if !ok {
		return fmt.Errorf("subagent %q not found", id)
	}

	if inst.Cancel != nil {
		inst.Cancel()
	}
	inst.SetState(SubagentStateKilled, nil)
	delete(t.instances, id)
	return nil
}

// KillAll cancels all active subagents and clears the tracker.
func (t *SubagentTracker) KillAll() int {
	t.mu.Lock()
	defer t.mu.Unlock()

	count := 0
	for id, inst := range t.instances {
		if inst.Cancel != nil {
			inst.Cancel()
		}
		inst.SetState(SubagentStateKilled, nil)
		delete(t.instances, id)
		count++
	}
	return count
}

// SendMessage delivers a message to a subagent's inbox.
// Returns an error if the recipient is not found or inbox is full.
func (t *SubagentTracker) SendMessage(recipientID, message string) error {
	t.mu.RLock()
	inst, ok := t.instances[recipientID]
	t.mu.RUnlock()

	if !ok {
		return fmt.Errorf("subagent %q not found", recipientID)
	}

	select {
	case inst.Inbox <- message:
		return nil
	default:
		return fmt.Errorf("subagent %q inbox is full", recipientID)
	}
}

// NotifyParent sends a notification to the parent engine.
// Used by subagent goroutines to signal completion or send messages.
func (t *SubagentTracker) NotifyParent(msg tools.SystemMessage) {
	if t.notifyCh == nil {
		return
	}
	select {
	case t.notifyCh <- msg:
	default:
		// Best effort — don't block if parent isn't listening
	}
}

// ActiveCount returns the number of active (non-killed) subagent instances.
func (t *SubagentTracker) ActiveCount() int {
	t.mu.RLock()
	defer t.mu.RUnlock()
	return len(t.instances)
}

// Remove removes a subagent from the tracker without killing it.
// Used when a subagent completes naturally.
func (t *SubagentTracker) Remove(id string) {
	t.mu.Lock()
	defer t.mu.Unlock()
	delete(t.instances, id)
}
