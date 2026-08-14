package errors

import (
	"errors"
	"fmt"
	"testing"
)

func TestHarnessError_Error(t *testing.T) {
	tests := []struct {
		name    string
		err     *HarnessError
		want    string
	}{
		{
			name: "error without cause",
			err:  New(ErrCodeToolExecution, "tool failed"),
			want: "[TOOL_EXECUTION] tool failed",
		},
		{
			name: "error with cause",
			err:  Wrap(fmt.Errorf("original error"), ErrCodeToolExecution, "tool failed"),
			want: "[TOOL_EXECUTION] tool failed: original error",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := tt.err.Error(); got != tt.want {
				t.Errorf("HarnessError.Error() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestHarnessError_Unwrap(t *testing.T) {
	original := fmt.Errorf("original error")
	wrapped := Wrap(original, ErrCodeToolExecution, "tool failed")

	if wrapped.Unwrap() != original {
		t.Errorf("HarnessError.Unwrap() = %v, want %v", wrapped.Unwrap(), original)
	}
}

func TestHarnessError_Is(t *testing.T) {
	err1 := New(ErrCodeToolExecution, "tool failed")
	err2 := New(ErrCodeToolExecution, "another tool failed")
	err3 := New(ErrCodeLLMTimeout, "LLM timeout")

	if !err1.Is(err2) {
		t.Error("errors with same code should match via Is()")
	}

	if err1.Is(err3) {
		t.Error("errors with different codes should not match via Is()")
	}
}

func TestHarnessError_WithComponent(t *testing.T) {
	err := New(ErrCodeToolExecution, "tool failed")
	component := "view_file"

	result := err.WithComponent(component)
	if result.Component != component {
		t.Errorf("WithComponent() = %v, want %v", result.Component, component)
	}
}

func TestHarnessError_WithContext(t *testing.T) {
	err := New(ErrCodeToolExecution, "tool failed")
	key := "path"
	value := "/test/file.txt"

	result := err.WithContext(key, value)
	if result.Context[key] != value {
		t.Errorf("WithContext() = %v, want %v", result.Context[key], value)
	}
}

func TestHarnessError_WithRequestID(t *testing.T) {
	err := New(ErrCodeToolExecution, "tool failed")
	requestID := "req-123"

	result := err.WithRequestID(requestID)
	if result.RequestID != requestID {
		t.Errorf("WithRequestID() = %v, want %v", result.RequestID, requestID)
	}
}

func TestHarnessError_Chaining(t *testing.T) {
	original := fmt.Errorf("original error")
	err := Wrap(original, ErrCodeToolExecution, "tool failed").
		WithContext("tool", "view_file").
		WithContext("path", "/test/file.txt").
		WithComponent("view_file").
		WithRequestID("req-123")

	if err.Component != "view_file" {
		t.Errorf("expected component view_file, got %v", err.Component)
	}

	if err.Context["tool"] != "view_file" {
		t.Errorf("expected tool context view_file, got %v", err.Context["tool"])
	}

	if err.RequestID != "req-123" {
		t.Errorf("expected requestID req-123, got %v", err.RequestID)
	}

	if !errors.Is(err, original) {
		t.Error("wrapped error should wrap original via errors.Is")
	}
}

func TestIsErrorCode(t *testing.T) {
	tests := []struct {
		name     string
		err      error
		code     ErrorCode
		expected bool
	}{
		{
			name:     "matching code",
			err:      New(ErrCodeToolExecution, "tool failed"),
			code:     ErrCodeToolExecution,
			expected: true,
		},
		{
			name:     "non-matching code",
			err:      New(ErrCodeToolExecution, "tool failed"),
			code:     ErrCodeLLMTimeout,
			expected: false,
		},
		{
			name:     "wrapped error with matching code",
			err:      Wrap(fmt.Errorf("original"), ErrCodeToolExecution, "tool failed"),
			code:     ErrCodeToolExecution,
			expected: true,
		},
		{
			name:     "non-HarnessError",
			err:      fmt.Errorf("plain error"),
			code:     ErrCodeToolExecution,
			expected: false,
		},
		{
			name:     "nil error",
			err:      nil,
			code:     ErrCodeToolExecution,
			expected: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := IsErrorCode(tt.err, tt.code); got != tt.expected {
				t.Errorf("IsErrorCode() = %v, want %v", got, tt.expected)
			}
		})
	}
}

func TestGetContext(t *testing.T) {
	tests := []struct {
		name     string
		err      error
		expected map[string]interface{}
	}{
		{
			name:     "HarnessError with context",
			err:      New(ErrCodeToolExecution, "tool failed").WithContext("key", "value"),
			expected: map[string]interface{}{"key": "value"},
		},
		{
			name:     "HarnessError without context",
			err:      New(ErrCodeToolExecution, "tool failed"),
			expected: nil,
		},
		{
			name:     "non-HarnessError",
			err:      fmt.Errorf("plain error"),
			expected: nil,
		},
		{
			name:     "nil error",
			err:      nil,
			expected: nil,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := GetContext(tt.err)
			if tt.expected == nil {
				if got != nil {
					t.Errorf("GetContext() = %v, want nil", got)
				}
			} else {
				if got == nil {
					t.Errorf("GetContext() = nil, want %v", tt.expected)
				} else if got["key"] != tt.expected["key"] {
					t.Errorf("GetContext() = %v, want %v", got, tt.expected)
				}
			}
		})
	}
}

func TestGetComponent(t *testing.T) {
	tests := []struct {
		name     string
		err      error
		expected string
	}{
		{
			name:     "HarnessError with component",
			err:      New(ErrCodeToolExecution, "tool failed").WithComponent("view_file"),
			expected: "view_file",
		},
		{
			name:     "HarnessError without component",
			err:      New(ErrCodeToolExecution, "tool failed"),
			expected: "",
		},
		{
			name:     "non-HarnessError",
			err:      fmt.Errorf("plain error"),
			expected: "",
		},
		{
			name:     "nil error",
			err:      nil,
			expected: "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := GetComponent(tt.err); got != tt.expected {
				t.Errorf("GetComponent() = %v, want %v", got, tt.expected)
			}
		})
	}
}

func TestGetRequestID(t *testing.T) {
	tests := []struct {
		name     string
		err      error
		expected string
	}{
		{
			name:     "HarnessError with request ID",
			err:      New(ErrCodeToolExecution, "tool failed").WithRequestID("req-123"),
			expected: "req-123",
		},
		{
			name:     "HarnessError without request ID",
			err:      New(ErrCodeToolExecution, "tool failed"),
			expected: "",
		},
		{
			name:     "non-HarnessError",
			err:      fmt.Errorf("plain error"),
			expected: "",
		},
		{
			name:     "nil error",
			err:      nil,
			expected: "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := GetRequestID(tt.err); got != tt.expected {
				t.Errorf("GetRequestID() = %v, want %v", got, tt.expected)
			}
		})
	}
}

func TestGetCode(t *testing.T) {
	tests := []struct {
		name     string
		err      error
		expected ErrorCode
	}{
		{
			name:     "HarnessError",
			err:      New(ErrCodeToolExecution, "tool failed"),
			expected: ErrCodeToolExecution,
		},
		{
			name:     "wrapped HarnessError",
			err:      Wrap(fmt.Errorf("original"), ErrCodeLLMTimeout, "LLM timeout"),
			expected: ErrCodeLLMTimeout,
		},
		{
			name:     "non-HarnessError",
			err:      fmt.Errorf("plain error"),
			expected: "",
		},
		{
			name:     "nil error",
			err:      nil,
			expected: "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := GetCode(tt.err); got != tt.expected {
				t.Errorf("GetCode() = %v, want %v", got, tt.expected)
			}
		})
	}
}

func TestNew(t *testing.T) {
	err := New(ErrCodeToolExecution, "tool failed")

	if err.Code != ErrCodeToolExecution {
		t.Errorf("New() code = %v, want %v", err.Code, ErrCodeToolExecution)
	}

	if err.Message != "tool failed" {
		t.Errorf("New() message = %v, want %v", err.Message, "tool failed")
	}

	if err.Cause != nil {
		t.Errorf("New() cause should be nil, got %v", err.Cause)
	}
}

func TestWrap(t *testing.T) {
	original := fmt.Errorf("original error")
	err := Wrap(original, ErrCodeToolExecution, "tool failed")

	if err.Code != ErrCodeToolExecution {
		t.Errorf("Wrap() code = %v, want %v", err.Code, ErrCodeToolExecution)
	}

	if err.Message != "tool failed" {
		t.Errorf("Wrap() message = %v, want %v", err.Message, "tool failed")
	}

	if err.Cause != original {
		t.Errorf("Wrap() cause = %v, want %v", err.Cause, original)
	}
}

func TestErrorCodes(t *testing.T) {
	// Test that all error codes are unique and non-empty
	codes := []ErrorCode{
		ErrCodeWorkspaceValidation,
		ErrCodePathTraversal,
		ErrCodeSymlinkAttack,
		ErrCodeFileNotFound,
		ErrCodePermissionDenied,
		ErrCodeToolExecution,
		ErrCodeToolTimeout,
		ErrCodeToolValidation,
		ErrCodeCommandInjection,
		ErrCodeLLMProvider,
		ErrCodeLLMTimeout,
		ErrCodeLLMRateLimit,
		ErrCodeLLMTokenLimit,
		ErrCodeResourceExhaustion,
		ErrCodeMemoryLimit,
		ErrCodeTaskLimit,
		ErrCodeConfiguration,
		ErrCodeInvalidConfig,
		ErrCodeMissingConfig,
		ErrCodeNetwork,
		ErrCodeConnectionFailed,
		ErrCodeConnectionTimeout,
		ErrCodeAuthentication,
		ErrCodeUnauthorized,
		ErrCodeInvalidAPIKey,
		ErrCodeEngineError,
		ErrCodeMaxTurnsExceeded,
		ErrCodeCompactionFailed,
		ErrCodeSubagentDepthLimit,
		ErrCodeConversationNotFound,
		ErrCodeStateCorruption,
		ErrCodePersistenceError,
		ErrCodeMCPConnectionFailed,
		ErrCodeMCPToolConflict,
		ErrCodeMCPExecutionError,
		ErrCodeProtocolError,
		ErrCodeHandshakeError,
		ErrCodeBinaryNotFound,
		ErrCodeConnectionError,
		ErrCodeDownloadFailed,
		ErrCodeInvalidPlatform,
	}

	seen := make(map[ErrorCode]bool)
	for _, code := range codes {
		if code == "" {
			t.Errorf("error code should not be empty")
		}
		if seen[code] {
			t.Errorf("duplicate error code: %v", code)
		}
		seen[code] = true
	}
}

func TestToProto(t *testing.T) {
	// Test basic error conversion
	err := New(ErrCodeFileNotFound, "file not found").
		WithContext("path", "/tmp/test.txt").
		WithContext("operation", "view_file").
		WithComponent("view_file").
		WithRequestID("req-123")

	protoEvent := err.ToProto()

	if protoEvent.Code != string(ErrCodeFileNotFound) {
		t.Errorf("expected code %s, got %s", ErrCodeFileNotFound, protoEvent.Code)
	}

	if protoEvent.Message != "file not found" {
		t.Errorf("expected message 'file not found', got %s", protoEvent.Message)
	}

	if protoEvent.Metadata == nil {
		t.Fatal("expected metadata to be set")
	}

	if protoEvent.Metadata["path"] != "/tmp/test.txt" {
		t.Errorf("expected path '/tmp/test.txt', got %s", protoEvent.Metadata["path"])
	}

	if protoEvent.Metadata["operation"] != "view_file" {
		t.Errorf("expected operation 'view_file', got %s", protoEvent.Metadata["operation"])
	}

	if protoEvent.Metadata["component"] != "view_file" {
		t.Errorf("expected component 'view_file', got %s", protoEvent.Metadata["component"])
	}

	if protoEvent.Metadata["request_id"] != "req-123" {
		t.Errorf("expected request_id 'req-123', got %s", protoEvent.Metadata["request_id"])
	}

	// Test error without context
	err2 := New(ErrCodeToolExecution, "tool failed")
	protoEvent2 := err2.ToProto()

	if protoEvent2.Metadata != nil {
		t.Errorf("expected nil metadata for error without context, got %v", protoEvent2.Metadata)
	}

	// Test error with numeric context
	err3 := New(ErrCodeLLMTimeout, "LLM timeout").
		WithContext("retry_count", 3).
		WithContext("timeout_ms", 30000)

	protoEvent3 := err3.ToProto()

	if protoEvent3.Metadata["retry_count"] != "3" {
		t.Errorf("expected retry_count '3', got %s", protoEvent3.Metadata["retry_count"])
	}

	if protoEvent3.Metadata["timeout_ms"] != "30000" {
		t.Errorf("expected timeout_ms '30000', got %s", protoEvent3.Metadata["timeout_ms"])
	}
}

func TestSerializeValue(t *testing.T) {
	tests := []struct {
		name     string
		value    interface{}
		expected string
	}{
		{"string", "hello", "hello"},
		{"int", 42, "42"},
		{"int32", int32(42), "42"},
		{"int64", int64(42), "42"},
		{"uint", uint(42), "42"},
		{"uint32", uint32(42), "42"},
		{"uint64", uint64(42), "42"},
		{"float32", float32(3.14), "3.14"},
		{"float64", 3.14, "3.14"},
		{"bool true", true, "true"},
		{"bool false", false, "false"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := SerializeValue(tt.value)
			if result != tt.expected {
				t.Errorf("expected %s, got %s", tt.expected, result)
			}
		})
	}
}