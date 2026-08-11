package connection

import (
	"bufio"
	"io"
	"strings"
	"sync"
)

// stderrRingBuffer captures stderr output from the harness process
// in a bounded ring buffer. This provides crash diagnostics when the
// WebSocket connection drops unexpectedly.
//
// The buffer stores at most `cap` lines. When full, new lines overwrite
// the oldest entries (circular buffer behavior).
type stderrRingBuffer struct {
	mu    sync.Mutex
	lines []string
	cap   int
	pos   int
	count int
}

// newStderrRingBuffer creates a ring buffer that stores at most `capacity` lines.
func newStderrRingBuffer(capacity int) *stderrRingBuffer {
	if capacity <= 0 {
		capacity = 100
	}
	return &stderrRingBuffer{
		lines: make([]string, capacity),
		cap:   capacity,
	}
}

// Capture reads from r line-by-line and stores each line in the ring buffer.
// This blocks until r returns an error (typically io.EOF when the process exits).
// Intended to be called as a goroutine: go buf.Capture(stderrPipe)
func (b *stderrRingBuffer) Capture(r io.Reader) {
	scanner := bufio.NewScanner(r)
	// Allow long lines (up to 1MB)
	scanner.Buffer(make([]byte, 0, 64*1024), 1024*1024)

	for scanner.Scan() {
		line := scanner.Text()
		b.mu.Lock()
		b.lines[b.pos] = line
		b.pos = (b.pos + 1) % b.cap
		if b.count < b.cap {
			b.count++
		}
		b.mu.Unlock()
	}
}

// Lines returns a copy of the buffered lines in chronological order.
func (b *stderrRingBuffer) Lines() []string {
	b.mu.Lock()
	defer b.mu.Unlock()

	if b.count == 0 {
		return nil
	}

	result := make([]string, 0, b.count)
	if b.count < b.cap {
		// Buffer hasn't wrapped yet
		result = append(result, b.lines[:b.count]...)
	} else {
		// Buffer has wrapped — start from pos (oldest) and wrap around
		result = append(result, b.lines[b.pos:]...)
		result = append(result, b.lines[:b.pos]...)
	}

	return result
}

// String returns the buffered lines joined with newlines.
// Returns "(no stderr output)" if empty.
func (b *stderrRingBuffer) String() string {
	lines := b.Lines()
	if len(lines) == 0 {
		return "(no stderr output)"
	}
	return strings.Join(lines, "\n")
}

// Len returns the number of lines currently stored.
func (b *stderrRingBuffer) Len() int {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.count
}
