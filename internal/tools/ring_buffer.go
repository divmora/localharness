package tools

import (
	"bytes"
	"sync"
)

// RingBuffer is a thread-safe, fixed-size circular buffer that implements io.Writer.
// When full, oldest data is overwritten. Used to capture process output without
// unbounded memory growth.
type RingBuffer struct {
	mu        sync.Mutex
	buf       []byte
	size      int
	pos       int  // next write position
	full      bool // whether the buffer has wrapped around
	listeners []chan struct{}
}

// NewRingBuffer creates a new ring buffer with the given capacity in bytes.
func NewRingBuffer(size int) *RingBuffer {
	if size <= 0 {
		size = 102400 // 100KB default
	}
	return &RingBuffer{
		buf:  make([]byte, size),
		size: size,
	}
}

// Subscribe registers a listener channel that is notified whenever new data is written.
// The channel is buffered (capacity 1) and notifications are non-blocking.
func (rb *RingBuffer) Subscribe() chan struct{} {
	rb.mu.Lock()
	defer rb.mu.Unlock()

	ch := make(chan struct{}, 1)
	rb.listeners = append(rb.listeners, ch)
	return ch
}

// Unsubscribe removes a previously registered listener channel.
func (rb *RingBuffer) Unsubscribe(ch chan struct{}) {
	rb.mu.Lock()
	defer rb.mu.Unlock()

	for i, l := range rb.listeners {
		if l == ch {
			rb.listeners = append(rb.listeners[:i], rb.listeners[i+1:]...)
			break
		}
	}
}

// Write appends data to the ring buffer, overwriting oldest data if full.
// Implements io.Writer. Notifies any active subscribers.
func (rb *RingBuffer) Write(p []byte) (n int, err error) {
	rb.mu.Lock()
	defer rb.mu.Unlock()

	n = len(p)
	if n == 0 {
		return 0, nil
	}

	// If the incoming data is larger than the buffer, only keep the last `size` bytes.
	if n >= rb.size {
		copy(rb.buf, p[n-rb.size:])
		rb.pos = 0
		rb.full = true
	} else {
		// How much space is available from pos to end of buffer?
		remaining := rb.size - rb.pos
		if n <= remaining {
			copy(rb.buf[rb.pos:], p)
			rb.pos += n
			if rb.pos == rb.size {
				rb.pos = 0
				rb.full = true
			}
		} else {
			// Split: fill to end, then wrap around
			copy(rb.buf[rb.pos:], p[:remaining])
			copy(rb.buf, p[remaining:])
			rb.pos = n - remaining
			rb.full = true
		}
	}

	// Notify all active subscribers non-blockingly
	for _, l := range rb.listeners {
		select {
		case l <- struct{}{}:
		default:
		}
	}

	return n, nil
}

// Contains checks whether the circular buffer contains the byte sequence p in chronological order.
func (rb *RingBuffer) Contains(p []byte) bool {
	rb.mu.Lock()
	defer rb.mu.Unlock()

	n := len(p)
	if n == 0 {
		return true
	}
	if n > rb.size {
		return false
	}

	if !rb.full {
		if n > rb.pos {
			return false
		}
		return bytes.Contains(rb.buf[:rb.pos], p)
	}

	// When full, chronological order is: rb.buf[rb.pos:] followed by rb.buf[:rb.pos]
	if bytes.Contains(rb.buf[rb.pos:], p) {
		return true
	}
	if bytes.Contains(rb.buf[:rb.pos], p) {
		return true
	}

	// Check wrap-around boundary between rb.buf[rb.size-1] and rb.buf[0]
	m := n - 1
	start := rb.size - m
	if start < rb.pos {
		start = rb.pos
	}
	end := m
	if end > rb.pos {
		end = rb.pos
	}
	boundary := make([]byte, 0, (rb.size-start)+end)
	boundary = append(boundary, rb.buf[start:]...)
	boundary = append(boundary, rb.buf[:end]...)
	return bytes.Contains(boundary, p)
}

// Bytes returns the buffered contents in chronological order.
func (rb *RingBuffer) Bytes() []byte {
	rb.mu.Lock()
	defer rb.mu.Unlock()

	if !rb.full {
		out := make([]byte, rb.pos)
		copy(out, rb.buf[:rb.pos])
		return out
	}

	// Buffer has wrapped: [pos..end] + [0..pos)
	out := make([]byte, rb.size)
	copy(out, rb.buf[rb.pos:])
	copy(out[rb.size-rb.pos:], rb.buf[:rb.pos])
	return out
}

// String returns the buffered contents as a string.
func (rb *RingBuffer) String() string {
	return string(rb.Bytes())
}

// Last returns the last n bytes of the buffer contents as a string.
// If the buffer contains fewer than n bytes, all contents are returned.
func (rb *RingBuffer) Last(n int) string {
	data := rb.Bytes()
	if n >= len(data) {
		return string(data)
	}
	return string(data[len(data)-n:])
}

// Len returns the number of bytes currently stored.
func (rb *RingBuffer) Len() int {
	rb.mu.Lock()
	defer rb.mu.Unlock()

	if rb.full {
		return rb.size
	}
	return rb.pos
}

// Reset clears the buffer.
func (rb *RingBuffer) Reset() {
	rb.mu.Lock()
	defer rb.mu.Unlock()

	rb.pos = 0
	rb.full = false
}
