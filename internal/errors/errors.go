// Package errors provides structured error types for LocalHarness.
package errors

import (
	"fmt"
)

// ErrorCode represents a categorizable error code for programmatic handling.
type ErrorCode string

const (
	// Workspace errors
	ErrCodeWorkspaceValidation ErrorCode = "WORKSPACE_VALIDATION"
	ErrCodePathTraversal      ErrorCode = "PATH_TRAVERSAL"
	ErrCodeSymlinkAttack      ErrorCode = "SYMLINK_ATTACK"
	ErrCodeFileNotFound       ErrorCode = "FILE_NOT_FOUND"
	ErrCodePermissionDenied   ErrorCode = "PERMISSION_DENIED"

	// Tool execution errors
	ErrCodeToolExecution    ErrorCode = "TOOL_EXECUTION"
	ErrCodeToolTimeout      ErrorCode = "TOOL_TIMEOUT"
	ErrCodeToolValidation   ErrorCode = "TOOL_VALIDATION"
	ErrCodeCommandInjection ErrorCode = "COMMAND_INJECTION"

	// LLM provider errors
	ErrCodeLLMProvider   ErrorCode = "LLM_PROVIDER"
	ErrCodeLLMTimeout    ErrorCode = "LLM_TIMEOUT"
	ErrCodeLLMRateLimit  ErrorCode = "LLM_RATE_LIMIT"
	ErrCodeLLMTokenLimit ErrorCode = "LLM_TOKEN_LIMIT"

	// Resource errors
	ErrCodeResourceExhaustion ErrorCode = "RESOURCE_EXHAUSTION"
	ErrCodeMemoryLimit        ErrorCode = "MEMORY_LIMIT"
	ErrCodeTaskLimit         ErrorCode = "TASK_LIMIT"

	// Configuration errors
	ErrCodeConfiguration  ErrorCode = "CONFIGURATION"
	ErrCodeInvalidConfig ErrorCode = "INVALID_CONFIG"
	ErrCodeMissingConfig ErrorCode = "MISSING_CONFIG"

	// Network errors
	ErrCodeNetwork           ErrorCode = "NETWORK"
	ErrCodeConnectionFailed  ErrorCode = "CONNECTION_FAILED"
	ErrCodeConnectionTimeout ErrorCode = "CONNECTION_TIMEOUT"

	// Authentication errors
	ErrCodeAuthentication ErrorCode = "AUTHENTICATION"
	ErrCodeUnauthorized   ErrorCode = "UNAUTHORIZED"
	ErrCodeInvalidAPIKey  ErrorCode = "INVALID_API_KEY"

	// Engine errors
	ErrCodeEngineError        ErrorCode = "ENGINE_ERROR"
	ErrCodeMaxTurnsExceeded   ErrorCode = "MAX_TURNS_EXCEEDED"
	ErrCodeCompactionFailed   ErrorCode = "COMPACTION_FAILED"
	ErrCodeSubagentDepthLimit ErrorCode = "SUBAGENT_DEPTH_LIMIT"
)

// HarnessError is the structured error type for LocalHarness.
type HarnessError struct {
	Code      ErrorCode              // Categorizable error code
	Message   string                 // Human-readable message
	Cause     error                  // Underlying error (for unwrapping)
	Context   map[string]interface{} // Structured context
	Component string                 // Component that generated the error
	RequestID string                 // Correlation ID for request tracking
}

// Error returns the error message with code and cause.
func (e *HarnessError) Error() string {
	if e.Cause != nil {
		return fmt.Sprintf("[%s] %s: %v", e.Code, e.Message, e.Cause)
	}
	return fmt.Sprintf("[%s] %s", e.Code, e.Message)
}

// Unwrap returns the underlying error for errors.Is/As.
func (e *HarnessError) Unwrap() error {
	return e.Cause
}

// Is checks if this error matches a target error type.
// For HarnessError, it compares error codes.
func (e *HarnessError) Is(target error) bool {
	if t, ok := target.(*HarnessError); ok {
		return e.Code == t.Code
	}
	return false
}

// WithContext adds structured context to the error.
func (e *HarnessError) WithContext(key string, value interface{}) *HarnessError {
	if e.Context == nil {
		e.Context = make(map[string]interface{})
	}
	e.Context[key] = value
	return e
}

// WithComponent sets the component that generated the error.
func (e *HarnessError) WithComponent(component string) *HarnessError {
	e.Component = component
	return e
}

// WithRequestID sets the correlation ID for request tracking.
func (e *HarnessError) WithRequestID(requestID string) *HarnessError {
	e.RequestID = requestID
	return e
}

// New creates a new HarnessError with the given code and message.
func New(code ErrorCode, message string) *HarnessError {
	return &HarnessError{
		Code:    code,
		Message: message,
	}
}

// Wrap wraps an existing error with additional context.
func Wrap(err error, code ErrorCode, message string) *HarnessError {
	return &HarnessError{
		Code:    code,
		Message: message,
		Cause:   err,
	}
}

// IsErrorCode checks if an error is a HarnessError with a specific code.
func IsErrorCode(err error, code ErrorCode) bool {
	var hErr *HarnessError
	if As(err, &hErr) {
		return hErr.Code == code
	}
	return false
}

// As finds the first error in err's chain that matches *HarnessError.
func As(err error, target **HarnessError) bool {
	// Walk the error chain looking for a HarnessError
	for err != nil {
		if hErr, ok := err.(*HarnessError); ok {
			*target = hErr
			return true
		}
		
		// Try to unwrap
		if unwrapper, ok := err.(interface{ Unwrap() error }); ok {
			err = unwrapper.Unwrap()
		} else {
			return false
		}
	}
	return false
}

// GetContext extracts structured context from an error.
func GetContext(err error) map[string]interface{} {
	var hErr *HarnessError
	if As(err, &hErr) {
		return hErr.Context
	}
	return nil
}

// GetComponent extracts the component name from an error.
func GetComponent(err error) string {
	var hErr *HarnessError
	if As(err, &hErr) {
		return hErr.Component
	}
	return ""
}

// GetRequestID extracts the request ID from an error.
func GetRequestID(err error) string {
	var hErr *HarnessError
	if As(err, &hErr) {
		return hErr.RequestID
	}
	return ""
}

// GetCode extracts the error code from an error.
func GetCode(err error) ErrorCode {
	var hErr *HarnessError
	if As(err, &hErr) {
		return hErr.Code
	}
	return ""
}