package server

import (
	"sync"

	pb "github.com/divmora/localharness/gen/go/localharness/v1"
)

// EventRingBuffer is a thread-safe circular buffer for recent ServerMessages.
type EventRingBuffer struct {
	mu       sync.RWMutex
	capacity int
	events   []*pb.ServerMessage
	head     int
	full     bool
}

// NewEventRingBuffer creates a ring buffer with the given capacity.
func NewEventRingBuffer(capacity int) *EventRingBuffer {
	if capacity <= 0 {
		capacity = 200
	}
	return &EventRingBuffer{
		capacity: capacity,
		events:   make([]*pb.ServerMessage, capacity),
	}
}

// Push adds an event to the ring buffer.
func (b *EventRingBuffer) Push(event *pb.ServerMessage) {
	b.mu.Lock()
	defer b.mu.Unlock()

	b.events[b.head] = event
	b.head = (b.head + 1) % b.capacity
	if b.head == 0 {
		b.full = true
	}
}

// All returns a slice of all stored events in chronological order.
func (b *EventRingBuffer) All() []*pb.ServerMessage {
	b.mu.RLock()
	defer b.mu.RUnlock()

	if !b.full {
		out := make([]*pb.ServerMessage, b.head)
		copy(out, b.events[:b.head])
		return out
	}

	out := make([]*pb.ServerMessage, b.capacity)
	n := copy(out, b.events[b.head:])
	copy(out[n:], b.events[:b.head])
	return out
}
