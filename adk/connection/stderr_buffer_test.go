package connection

import (
	"strings"
	"testing"
)

func TestStderrRingBuffer_BasicCapture(t *testing.T) {
	buf := newStderrRingBuffer(5)

	r := strings.NewReader("line1\nline2\nline3\n")
	buf.Capture(r)

	lines := buf.Lines()
	if len(lines) != 3 {
		t.Fatalf("expected 3 lines, got %d", len(lines))
	}
	if lines[0] != "line1" || lines[1] != "line2" || lines[2] != "line3" {
		t.Errorf("unexpected lines: %v", lines)
	}
}

func TestStderrRingBuffer_Wraparound(t *testing.T) {
	buf := newStderrRingBuffer(3) // Only 3 slots

	// Write 5 lines — should keep last 3
	r := strings.NewReader("A\nB\nC\nD\nE\n")
	buf.Capture(r)

	lines := buf.Lines()
	if len(lines) != 3 {
		t.Fatalf("expected 3 lines, got %d: %v", len(lines), lines)
	}
	if lines[0] != "C" || lines[1] != "D" || lines[2] != "E" {
		t.Errorf("expected [C D E], got %v", lines)
	}
}

func TestStderrRingBuffer_Empty(t *testing.T) {
	buf := newStderrRingBuffer(10)

	if buf.Len() != 0 {
		t.Errorf("expected 0 lines, got %d", buf.Len())
	}
	if buf.String() != "(no stderr output)" {
		t.Errorf("unexpected empty string: %q", buf.String())
	}
	if lines := buf.Lines(); lines != nil {
		t.Errorf("expected nil lines, got %v", lines)
	}
}

func TestStderrRingBuffer_ExactCapacity(t *testing.T) {
	buf := newStderrRingBuffer(3)

	// Write exactly capacity lines
	r := strings.NewReader("X\nY\nZ\n")
	buf.Capture(r)

	lines := buf.Lines()
	if len(lines) != 3 {
		t.Fatalf("expected 3 lines, got %d", len(lines))
	}
	if lines[0] != "X" || lines[1] != "Y" || lines[2] != "Z" {
		t.Errorf("expected [X Y Z], got %v", lines)
	}
}

func TestStderrRingBuffer_String(t *testing.T) {
	buf := newStderrRingBuffer(10)

	r := strings.NewReader("hello\nworld\n")
	buf.Capture(r)

	s := buf.String()
	if s != "hello\nworld" {
		t.Errorf("unexpected string: %q", s)
	}
}
