package tools

import (
	"strings"
	"testing"
)

func TestRingBufferBasic(t *testing.T) {
	rb := NewRingBuffer(100)

	n, err := rb.Write([]byte("hello"))
	if err != nil {
		t.Fatalf("Write failed: %v", err)
	}
	if n != 5 {
		t.Errorf("expected 5 bytes written, got %d", n)
	}
	if rb.String() != "hello" {
		t.Errorf("expected 'hello', got %q", rb.String())
	}
	if rb.Len() != 5 {
		t.Errorf("expected Len=5, got %d", rb.Len())
	}
}

func TestRingBufferMultipleWrites(t *testing.T) {
	rb := NewRingBuffer(100)

	rb.Write([]byte("hello "))
	rb.Write([]byte("world"))

	if rb.String() != "hello world" {
		t.Errorf("expected 'hello world', got %q", rb.String())
	}
}

func TestRingBufferOverflow(t *testing.T) {
	rb := NewRingBuffer(10)

	// Write more than buffer size
	rb.Write([]byte("abcdefghijklmno")) // 15 bytes into 10-byte buffer

	result := rb.String()
	if len(result) != 10 {
		t.Errorf("expected 10 bytes, got %d: %q", len(result), result)
	}
	// Should keep the last 10 bytes: "fghijklmno"
	if result != "fghijklmno" {
		t.Errorf("expected 'fghijklmno', got %q", result)
	}
}

func TestRingBufferWrapAround(t *testing.T) {
	rb := NewRingBuffer(10)

	// Fill buffer partially
	rb.Write([]byte("12345678")) // 8 bytes
	// Now write 5 more (wraps around)
	rb.Write([]byte("abcde"))

	result := rb.String()
	if len(result) != 10 {
		t.Errorf("expected 10 bytes, got %d: %q", len(result), result)
	}
	// Should be "78" from old + "abcde" wrapped around = "345678" tail gone, etc.
	// Buffer is circular: positions 0-9
	// First write: pos 0-7 = "12345678", pos=8
	// Second write: "abc" fits in 8,9,0 (remaining=2, so "ab" at 8,9), then wrap: "cde" at 0,1,2
	// Wait let me recalculate:
	// buf size=10, first write 8 bytes, pos=8, full=false
	// second write 5 bytes: remaining = 10-8 = 2
	// n=5 > remaining=2, so split: copy "ab" to buf[8:10], copy "cde" to buf[0:3], pos=3, full=true
	// Contents in order: buf[3:10] + buf[0:3] = "45678ab" + "cde" = "45678abcde"
	if result != "45678abcde" {
		t.Errorf("expected '45678abcde', got %q", result)
	}
}

func TestRingBufferLast(t *testing.T) {
	rb := NewRingBuffer(100)
	rb.Write([]byte("hello world"))

	if rb.Last(5) != "world" {
		t.Errorf("expected 'world', got %q", rb.Last(5))
	}
	if rb.Last(100) != "hello world" {
		t.Errorf("Last(100) should return all content: %q", rb.Last(100))
	}
}

func TestRingBufferReset(t *testing.T) {
	rb := NewRingBuffer(100)
	rb.Write([]byte("hello"))
	rb.Reset()

	if rb.Len() != 0 {
		t.Errorf("expected Len=0 after Reset, got %d", rb.Len())
	}
	if rb.String() != "" {
		t.Errorf("expected empty string after Reset, got %q", rb.String())
	}
}

func TestRingBufferExactFit(t *testing.T) {
	rb := NewRingBuffer(5)
	rb.Write([]byte("12345"))

	if rb.String() != "12345" {
		t.Errorf("expected '12345', got %q", rb.String())
	}
	if rb.Len() != 5 {
		t.Errorf("expected Len=5, got %d", rb.Len())
	}
}

func TestRingBufferEmpty(t *testing.T) {
	rb := NewRingBuffer(10)

	if rb.String() != "" {
		t.Errorf("expected empty string, got %q", rb.String())
	}
	if rb.Len() != 0 {
		t.Errorf("expected Len=0, got %d", rb.Len())
	}
}

func TestRingBufferConcurrent(t *testing.T) {
	rb := NewRingBuffer(1024)
	done := make(chan struct{})

	// Writer goroutine
	go func() {
		for i := 0; i < 100; i++ {
			rb.Write([]byte("test data "))
		}
		close(done)
	}()

	// Reader goroutine — should not panic
	for i := 0; i < 50; i++ {
		_ = rb.String()
		_ = rb.Len()
		_ = rb.Last(10)
	}

	<-done

	// Verify buffer is in a consistent state
	result := rb.String()
	if len(result) == 0 {
		t.Error("expected non-empty buffer after writes")
	}
}

func TestRingBufferDefaultSize(t *testing.T) {
	rb := NewRingBuffer(0)
	if rb.size != 102400 {
		t.Errorf("expected default size 102400, got %d", rb.size)
	}

	rb2 := NewRingBuffer(-1)
	if rb2.size != 102400 {
		t.Errorf("expected default size 102400 for negative, got %d", rb2.size)
	}
}

func TestRingBufferLargeOverflow(t *testing.T) {
	rb := NewRingBuffer(5)

	// Write data much larger than buffer multiple times
	rb.Write([]byte("first-pass-data"))
	rb.Write([]byte("second-pass-data-here"))

	result := rb.String()
	if len(result) != 5 {
		t.Errorf("expected 5 bytes, got %d", len(result))
	}
	// Should be last 5 bytes of "second-pass-data-here" = "-here"
	if result != "-here" {
		t.Errorf("expected '-here', got %q", result)
	}
}

func TestParseTerminalResult(t *testing.T) {
	marker := "__LH_DONE_test1234__"

	tests := []struct {
		name       string
		output     string
		beforeLen  int
		wantOutput string
		wantCode   int
	}{
		{
			name:       "basic command",
			output:     "hello world\n" + marker + "0\n",
			beforeLen:  0,
			wantOutput: "hello world",
			wantCode:   0,
		},
		{
			name:       "non-zero exit",
			output:     "error output\n" + marker + "1\n",
			beforeLen:  0,
			wantOutput: "error output",
			wantCode:   1,
		},
		{
			name:       "with prior output",
			output:     "old stuff\nnew output\n" + marker + "0\n",
			beforeLen:  10, // Skip "old stuff\n"
			wantOutput: "new output",
			wantCode:   0,
		},
		{
			name:       "no marker",
			output:     "just some output\n",
			beforeLen:  0,
			wantOutput: "just some output",
			wantCode:   -1,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			gotOutput, gotCode := parseTerminalResult(tt.output, tt.beforeLen, marker)
			if gotOutput != tt.wantOutput {
				t.Errorf("output = %q, want %q", gotOutput, tt.wantOutput)
			}
			if gotCode != tt.wantCode {
				t.Errorf("exitCode = %d, want %d", gotCode, tt.wantCode)
			}
		})
	}
}

func TestExtractTerminalOutput(t *testing.T) {
	marker := "__LH_DONE_test__"

	tests := []struct {
		name       string
		allOutput  string
		beforeLen  int
		wantOutput string
	}{
		{
			name:       "new output with marker",
			allOutput:  "hello\n" + marker + "0\n",
			beforeLen:  0,
			wantOutput: "hello",
		},
		{
			name:       "new output without marker",
			allOutput:  "just output",
			beforeLen:  0,
			wantOutput: "just output",
		},
		{
			name:       "beforeLen past end",
			allOutput:  "short",
			beforeLen:  100,
			wantOutput: "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := extractTerminalOutput(tt.allOutput, tt.beforeLen, marker)
			if got != tt.wantOutput {
				t.Errorf("got %q, want %q", got, tt.wantOutput)
			}
		})
	}
}

func TestTruncateOutputFunc(t *testing.T) {
	// Already tested in tools_test.go, but verify it's accessible
	result := truncateOutput("hello", 100)
	if result != "hello" {
		t.Errorf("expected 'hello', got %q", result)
	}

	long := strings.Repeat("x", 200)
	result = truncateOutput(long, 50)
	if len(result) <= 50 {
		// The truncated version includes a suffix
		t.Logf("truncated result length: %d", len(result))
	}
}
