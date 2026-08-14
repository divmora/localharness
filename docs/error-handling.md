# Error Handling Guide for SDK Developers

This guide explains how to handle structured errors from LocalHarness in SDK clients, with practical examples for error recovery and user experience.

## Overview

LocalHarness transmits structured errors over WebSocket with machine-readable error codes and contextual metadata. This enables SDK clients to:

- Implement programmatic error handling based on error categories
- Provide user-friendly error messages
- Implement intelligent error recovery (retry, fallback, user intervention)
- Extract debugging information for logs and monitoring

## Error Reception

SDK clients receive errors via two wire protocol messages:

### 1. ErrorEvent (Session-level errors)

```protobuf
message ErrorEvent {
  string message = 1;
  string code = 2;
  bool fatal = 3;
  map<string, string> metadata = 4;
}
```

Emitted for session-level errors (initialization, connection, configuration).

### 2. ErrorInfo (Step-level errors)

```protobuf
message ErrorInfo {
  string message = 1;
  string code = 2;
  map<string, string> metadata = 3;
}
```

Emitted within `StepUpdate` for tool execution and engine errors.

## Error Code Categories

### Workspace Errors
- `WORKSPACE_VALIDATION` - Invalid workspace path or configuration
- `PATH_TRAVERSAL` - Attempted access outside allowed paths
- `SYMLINK_ATTACK` - Suspicious symlink detected
- `FILE_NOT_FOUND` - File does not exist
- `PERMISSION_DENIED` - Insufficient filesystem permissions

### Tool Execution Errors
- `TOOL_EXECUTION` - General tool execution failure
- `TOOL_TIMEOUT` - Tool execution exceeded timeout
- `TOOL_VALIDATION` - Invalid tool arguments or configuration
- `COMMAND_INJECTION` - Potential command injection detected

### LLM Provider Errors
- `LLM_PROVIDER` - General LLM provider failure
- `LLM_TIMEOUT` - LLM API call timeout
- `LLM_RATE_LIMIT` - API rate limit exceeded
- `LLM_TOKEN_LIMIT` - Token limit exceeded

### Resource Errors
- `RESOURCE_EXHAUSTION` - System resource exhausted
- `MEMORY_LIMIT` - Memory limit exceeded
- `TASK_LIMIT` - Concurrent task limit exceeded

### Configuration Errors
- `CONFIGURATION` - General configuration error
- `INVALID_CONFIG` - Invalid configuration value
- `MISSING_CONFIG` - Required configuration missing

### Network Errors
- `NETWORK` - General network error
- `CONNECTION_FAILED` - Connection establishment failed
- `CONNECTION_TIMEOUT` - Connection timeout

### Authentication Errors
- `AUTHENTICATION` - Authentication failure
- `UNAUTHORIZED` - Unauthorized access
- `INVALID_API_KEY` - Invalid API key

### Engine Errors
- `ENGINE_ERROR` - General engine error
- `MAX_TURNS_EXCEEDED` - Agentic loop iteration limit
- `COMPACTION_FAILED` - Context compaction failure
- `SUBAGENT_DEPTH_LIMIT` - Subagent nesting depth exceeded

## Error Context Fields

Common metadata fields by error type:

### File Operations
- `path` - File path
- `operation` - Operation type (view_file, write_to_file, etc.)
- `workspace` - Workspace directory
- `line_range` - Line range for view operations

### Tool Execution
- `tool` - Tool name
- `args` - Tool arguments
- `cwd` - Current working directory
- `timeout` - Timeout in milliseconds
- `exit_code` - Process exit code

### LLM Calls
- `model` - Model name
- `provider` - LLM provider
- `retry_count` - Number of retries attempted
- `finish_reason` - LLM finish reason
- `status_code` - HTTP status code

### Network
- `url` - Request URL
- `status_code` - HTTP status code
- `timeout_ms` - Timeout in milliseconds
- `attempt` - Retry attempt number

### Engine
- `trajectory_id` - Trajectory identifier
- `conversation_id` - Conversation identifier
- `step_index` - Step index
- `max_turns` - Maximum turns configured

## Error Handling Patterns

### 1. Basic Error Extraction

```go
// Example: Go SDK
func handleStepError(errorInfo *pb.ErrorInfo) {
    code := errorInfo.Code
    message := errorInfo.Message
    metadata := errorInfo.Metadata

    fmt.Printf("Error: [%s] %s\n", code, message)
    for key, value := range metadata {
        fmt.Printf("  %s: %s\n", key, value)
    }
}
```

```python
# Example: Python SDK
def handle_step_error(error_info):
    code = error_info.code
    message = error_info.message
    metadata = error_info.metadata

    print(f"Error: [{code}] {message}")
    for key, value in metadata.items():
        print(f"  {key}: {value}")
```

```typescript
// Example: TypeScript SDK
function handleStepError(errorInfo: ErrorInfo): void {
    const code = errorInfo.code;
    const message = errorInfo.message;
    const metadata = errorInfo.metadata;

    console.log(`Error: [${code}] ${message}`);
    for (const [key, value] of Object.entries(metadata)) {
        console.log(`  ${key}: ${value}`);
    }
}
```

### 2. Error Recovery by Category

```go
// Example: Intelligent retry logic
func handleLLMError(errorInfo *pb.ErrorInfo) error {
    switch errorInfo.Code {
    case "LLM_RATE_LIMIT":
        // Wait and retry with exponential backoff
        retryCount, _ := strconv.Atoi(errorInfo.Metadata["retry_count"])
        waitTime := time.Second * time.Duration(math.Pow(2, float64(retryCount)))
        time.Sleep(waitTime)
        return retryRequest()

    case "LLM_TIMEOUT":
        // Retry with longer timeout
        timeout, _ := strconv.Atoi(errorInfo.Metadata["timeout_ms"])
        return retryWithTimeout(timeout * 2)

    case "LLM_TOKEN_LIMIT":
        // Suggest context compaction or shorter prompt
        return errors.New("reduce context window or prompt length")

    case "INVALID_API_KEY":
        // Fatal error - require user intervention
        return requestNewAPIKey()

    default:
        // Unknown LLM error - fail gracefully
        return fmt.Errorf("LLM error: %s", errorInfo.Message)
    }
}
```

### 3. User-Friendly Error Messages

```go
// Example: Map error codes to user-friendly messages
func getUserFriendlyMessage(errorInfo *pb.ErrorInfo) string {
    switch errorInfo.Code {
    case "FILE_NOT_FOUND":
        path := errorInfo.Metadata["path"]
        return fmt.Sprintf("File not found: %s", path)

    case "PERMISSION_DENIED":
        path := errorInfo.Metadata["path"]
        return fmt.Sprintf("Permission denied: cannot access %s", path)

    case "TOOL_TIMEOUT":
        tool := errorInfo.Metadata["tool"]
        return fmt.Sprintf("Tool '%s' timed out. Try with a longer timeout.", tool)

    case "MAX_TURNS_EXCEEDED":
        return "Agent completed maximum iterations. Request may need refinement."

    case "LLM_RATE_LIMIT":
        return "API rate limit exceeded. Please wait and try again."

    case "INVALID_API_KEY":
        return "Invalid API key. Please check your configuration."

    default:
        return errorInfo.Message
    }
}
```

### 4. Error Logging and Monitoring

```go
// Example: Structured error logging
func logError(errorInfo *pb.ErrorInfo, context map[string]interface{}) {
    logEntry := map[string]interface{}{
        "error_code":    errorInfo.Code,
        "error_message": errorInfo.Message,
        "metadata":      errorInfo.Metadata,
        "context":       context,
        "timestamp":     time.Now().UTC().Format(time.RFC3339),
    }

    // Send to monitoring system
    monitoring.SendError(logEntry)

    // Log locally
    log.WithFields(logrus.Fields{
        "error_code": errorInfo.Code,
        "component":  errorInfo.Metadata["component"],
    }).Error(errorInfo.Message)
}
```

### 5. Error Recovery Strategies

#### Retry with Exponential Backoff

```go
func retryWithBackoff(fn func() error, maxRetries int) error {
    var lastErr error
    for i := 0; i < maxRetries; i++ {
        err := fn()
        if err == nil {
            return nil
        }

        var hErr *errors.HarnessError
        if errors.As(err, &hErr) {
            switch hErr.Code {
            case errors.ErrCodeLLMRateLimit,
                 errors.ErrCodeConnectionTimeout,
                 errors.ErrCodeNetwork:
                // Retryable errors
                waitTime := time.Second * time.Duration(math.Pow(2, float64(i)))
                time.Sleep(waitTime)
                lastErr = err
                continue
            default:
                // Non-retryable errors
                return err
            }
        }
        lastErr = err
    }
    return fmt.Errorf("max retries exceeded: %w", lastErr)
}
```

#### Fallback to Alternative Provider

```go
func handleProviderError(errorInfo *pb.ErrorInfo) error {
    if errorInfo.Code == "LLM_PROVIDER" {
        // Try fallback provider
        if fallbackConfigured() {
            return switchToFallbackProvider()
        }
        return errors.New("primary provider failed and no fallback configured")
    }
    return errors.New(errorInfo.Message)
}
```

#### User Intervention Request

```go
func handleFatalError(errorInfo *pb.ErrorInfo) error {
    if errorInfo.Fatal {
        // Request user intervention
        message := getUserFriendlyMessage(errorInfo)
        return requestUserIntervention(message, errorInfo)
    }
    return errors.New(errorInfo.Message)
}
```

## SDK-Specific Examples

### Go SDK

```go
package main

import (
    "fmt"
    "github.com/divmora/localharness/gen/go/localharness/v1"
)

func handleStepUpdate(step *pb.StepUpdate) {
    if step.State == pb.StepUpdate_STATE_ERROR && step.ErrorInfo != nil {
        errInfo := step.ErrorInfo

        // Extract error information
        code := errInfo.Code
        message := errInfo.Message
        metadata := errInfo.Metadata

        // Log error
        fmt.Printf("Tool error: [%s] %s\n", code, message)

        // Implement error recovery
        switch code {
        case "TOOL_TIMEOUT":
            fmt.Println("Tool timed out - retrying with longer timeout")
            // Implement retry logic
        case "FILE_NOT_FOUND":
            path := metadata["path"]
            fmt.Printf("File not found: %s\n", path)
            // Implement file creation or alternative logic
        default:
            fmt.Println("Non-recoverable error - stopping")
        }
    }
}
```

### Python SDK

```python
from localharness.gen.localharness.v1 import localharness_pb2

def handle_step_update(step):
    if step.state == localharness_pb2.StepUpdate.STATE_ERROR and step.error_info:
        error_info = step.error_info

        # Extract error information
        code = error_info.code
        message = error_info.message
        metadata = dict(error_info.metadata)

        # Log error
        print(f"Tool error: [{code}] {message}")

        # Implement error recovery
        if code == "TOOL_TIMEOUT":
            print("Tool timed out - retrying with longer timeout")
            # Implement retry logic
        elif code == "FILE_NOT_FOUND":
            path = metadata.get("path", "unknown")
            print(f"File not found: {path}")
            # Implement file creation or alternative logic
        else:
            print("Non-recoverable error - stopping")
```

### TypeScript SDK

```typescript
import { StepUpdate, ErrorInfo } from './localharness_pb';

function handleStepUpdate(step: StepUpdate): void {
    if (step.state === StepUpdate.State.STATE_ERROR && step.errorInfo) {
        const errorInfo: ErrorInfo = step.errorInfo;

        // Extract error information
        const code = errorInfo.code;
        const message = errorInfo.message;
        const metadata = errorInfo.metadata;

        // Log error
        console.log(`Tool error: [${code}] ${message}`);

        // Implement error recovery
        switch (code) {
            case "TOOL_TIMEOUT":
                console.log("Tool timed out - retrying with longer timeout");
                // Implement retry logic
                break;
            case "FILE_NOT_FOUND":
                const path = metadata.get("path") || "unknown";
                console.log(`File not found: ${path}`);
                // Implement file creation or alternative logic
                break;
            default:
                console.log("Non-recoverable error - stopping");
        }
    }
}
```

## Best Practices

### 1. Always Check Error Codes
```go
// Good
if errors.IsErrorCode(err, errors.ErrCodeLLMRateLimit) {
    // Implement retry logic
}

// Avoid
if strings.Contains(err.Error(), "rate limit") {
    // Brittle string matching
}
```

### 2. Extract Context for Debugging
```go
// Good
if ctx := errors.GetContext(err); ctx != nil {
    log.Printf("Error context: path=%s, operation=%s", ctx["path"], ctx["operation"])
}

// Include in error logs
```

### 3. Provide User-Friendly Messages
```go
// Good
userMessage := getUserFriendlyMessage(errorInfo)
showToUser(userMessage)

// Avoid
showToUser(errorInfo.Message) // Too technical
```

### 4. Implement Intelligent Recovery
```go
// Good
switch errors.GetCode(err) {
case errors.ErrCodeLLMRateLimit:
    return retryWithBackoff()
case errors.ErrCodeLLMTimeout:
    return retryWithTimeout()
default:
    return err
}

// Avoid
return err // No recovery attempt
```

### 5. Log Structured Error Data
```go
// Good
log.WithFields(logrus.Fields{
    "error_code":   errors.GetCode(err),
    "component":    errors.GetComponent(err),
    "request_id":   errors.GetRequestID(err),
    "context":      errors.GetContext(err),
}).Error(err.Error())

// Avoid
log.Error(err.Error()) // Loss of context
```

## Testing Error Handling

### Unit Tests

```go
func TestErrorRecovery(t *testing.T) {
    // Mock error response
    errorInfo := &pb.ErrorInfo{
        Code:    "LLM_RATE_LIMIT",
        Message: "API rate limit exceeded",
        Metadata: map[string]string{
            "retry_count": "2",
            "provider":    "openai",
        },
    }

    // Test error recovery logic
    err := handleLLMError(errorInfo)
    assert.NoError(t, err)
}
```

### Integration Tests

```go
func TestErrorHandlingIntegration(t *testing.T) {
    // Set up test client
    client := NewTestClient()

    // Trigger error condition
    err := client.SendMessage("invalid request")

    // Verify error handling
    assert.Error(t, err)
    var hErr *errors.HarnessError
    assert.True(t, errors.As(err, &hErr))
    assert.Equal(t, errors.ErrCodeToolValidation, hErr.Code)
}
```

## Troubleshooting

### Common Issues

**Q: Error codes not appearing in client**
- A: Ensure proto schema is updated and regenerated
- A: Check that SDK client is using latest generated code

**Q: Context metadata missing**
- A: Verify that error is created with `WithContext()`
- A: Check that metadata values are serializable to strings

**Q: Backward compatibility issues**
- A: Use `sendStructuredError()` for new errors, `sendError()` for legacy
- A: Test with both old and new SDK clients

## References

- [Architecture - Error Handling](architecture.md#error-handling)
- [Proto Schema](../proto/localharness/v1/localharness.proto)
- [Go Error Package](../internal/errors/errors.go)
