# Proto Schema Changes for SDK Maintainers

This document describes changes to the LocalHarness protobuf schema that SDK maintainers need to be aware of, with migration guidance and compatibility information.

## Version: Error Metadata Addition

### Change Date
2026-08-14

### Impact Level
**MINOR** - Backward compatible addition

### Changed Messages

#### 1. ErrorEvent

**Location:** `proto/localharness/v1/localharness.proto`

**Before:**
```protobuf
message ErrorEvent {
  string message = 1;
  string code = 2;
  bool fatal = 3;
}
```

**After:**
```protobuf
message ErrorEvent {
  string message = 1;
  string code = 2;
  bool fatal = 3;
  map<string, string> metadata = 4;  // NEW FIELD
}
```

**Purpose:** Enable transmission of structured error context (path, operation, component, request_id, etc.) over WebSocket.

**Compatibility:** Fully backward compatible. Existing clients that ignore the new field will continue to work.

#### 2. ErrorInfo

**Location:** `proto/localharness/v1/localharness.proto`

**Before:**
```protobuf
message ErrorInfo {
  string message = 1;
  string code = 2;
}
```

**After:**
```protobuf
message ErrorInfo {
  string message = 1;
  string code = 2;
  map<string, string> metadata = 3;  // NEW FIELD
}
```

**Purpose:** Enable structured error context in step-level errors (tool execution, engine errors).

**Compatibility:** Fully backward compatible. The field was already present in the schema but now utilized.

## Migration Guide for SDK Maintainers

### 1. Regenerate Proto Code

SDK maintainers need to regenerate protobuf code for their target language:

#### Go SDK
```bash
# Already handled by the Go build process
make proto
```

#### Python SDK
```bash
# Using protoc
protoc --python_out=. --proto_path=proto proto/localharness/v1/localharness.proto

# Or using grpc_tools
python -m grpc_tools.protoc \
    --python_out=. \
    --grpc_python_out=. \
    --proto_path=proto \
    proto/localharness/v1/localharness.proto
```

#### TypeScript/JavaScript SDK
```bash
# Using protoc-gen-ts
protoc \
    --plugin=protoc-gen-ts=./node_modules/.bin/protoc-gen-ts \
    --ts_out=. \
    --proto_path=proto \
    proto/localharness/v1/localharness.proto
```

### 2. Update Error Handling Code

SDK error handling should be updated to leverage the new metadata field:

#### Before (Legacy Error Handling)
```python
# Python SDK - Before
def handle_error(error_event):
    code = error_event.code
    message = error_event.message
    print(f"Error: [{code}] {message}")
```

#### After (Structured Error Handling)
```python
# Python SDK - After
def handle_error(error_event):
    code = error_event.code
    message = error_event.message
    metadata = error_event.metadata  # NEW: Access structured context

    print(f"Error: [{code}] {message}")
    if metadata:
        for key, value in metadata.items():
            print(f"  {key}: {value}")
```

### 3. Implement Error Code-Based Logic

SDKs should implement programmatic error handling based on error codes:

```python
# Python SDK - Error Recovery Example
def handle_llm_error(error_info):
    code = error_info.code
    metadata = error_info.metadata

    if code == "LLM_RATE_LIMIT":
        retry_count = int(metadata.get("retry_count", "0"))
        wait_time = 2 ** retry_count  # Exponential backoff
        time.sleep(wait_time)
        return retry_request()
    elif code == "LLM_TIMEOUT":
        timeout = int(metadata.get("timeout_ms", "30000"))
        return retry_with_timeout(timeout * 2)
    else:
        return error_info.message
```

### 4. Update User-Facing Error Messages

SDKs should map error codes to user-friendly messages:

```python
# Python SDK - User-Friendly Messages
ERROR_MESSAGES = {
    "FILE_NOT_FOUND": lambda m: f"File not found: {m.get('path', 'unknown')}",
    "PERMISSION_DENIED": lambda m: f"Permission denied: {m.get('path', 'unknown')}",
    "TOOL_TIMEOUT": lambda m: f"Tool '{m.get('tool', 'unknown')}' timed out",
    "MAX_TURNS_EXCEEDED": lambda m: "Agent completed maximum iterations",
    "LLM_RATE_LIMIT": lambda m: "API rate limit exceeded. Please wait.",
}

def get_user_message(error_info):
    code = error_info.code
    metadata = error_info.metadata
    message_func = ERROR_MESSAGES.get(code)
    return message_func(metadata) if message_func else error_info.message
```

## Backward Compatibility

### Guaranteed Compatibility

The proto changes are **fully backward compatible**:

1. **Field Addition:** Adding new fields to protobuf messages is safe
2. **Optional Field:** The `metadata` field is optional (not `required`)
3. **Default Behavior:** Old clients ignore unknown fields by default
4. **Wire Protocol:** No changes to message structure or encoding

### Testing Compatibility

SDK maintainers should test with:

1. **Old SDK + New Server:** Old SDK should work with new server
2. **New SDK + Old Server:** New SDK should handle missing metadata gracefully
3. **New SDK + New Server:** Full structured error functionality

```python
# Python SDK - Backward Compatibility Check
def handle_error_safe(error_event):
    code = error_event.code
    message = error_event.message

    # Safely access metadata (may not exist in old protocol)
    metadata = getattr(error_event, 'metadata', None) or {}

    print(f"Error: [{code}] {message}")
    for key, value in metadata.items():
        print(f"  {key}: {value}")
```

## Error Code Reference

SDK maintainers should implement handling for these error codes:

### High-Priority Error Codes (Implement First)

| Error Code | Category | User Action Required |
|:---|:---|:---|
| `INVALID_API_KEY` | Authentication | Update API key |
| `PERMISSION_DENIED` | Workspace | Check file permissions |
| `FILE_NOT_FOUND` | Workspace | Verify file path |
| `MAX_TURNS_EXCEEDED` | Engine | Refine user request |
| `LLM_RATE_LIMIT` | LLM Provider | Wait and retry |

### Medium-Priority Error Codes

| Error Code | Category | Recovery Strategy |
|:---|:---|:---|
| `TOOL_TIMEOUT` | Tool Execution | Retry with longer timeout |
| `LLM_TIMEOUT` | LLM Provider | Retry with extended timeout |
| `NETWORK` | Network | Retry with backoff |
| `CONNECTION_FAILED` | Network | Reconnect |

### Low-Priority Error Codes

| Error Code | Category | Action |
|:---|:---|:---|
| `TOOL_VALIDATION` | Tool Execution | Fix tool arguments |
| `CONFIGURATION` | Configuration | Update config |
| `PATH_TRAVERSAL` | Workspace | Security concern |

## Metadata Field Reference

Common metadata fields that SDKs should expect:

### File Operations
- `path` (string) - File path
- `operation` (string) - Operation type
- `workspace` (string) - Workspace directory
- `line_range` (string) - Line range for view operations

### Tool Execution
- `tool` (string) - Tool name
- `args` (string) - Tool arguments
- `cwd` (string) - Current working directory
- `timeout` (string) - Timeout in milliseconds
- `exit_code` (string) - Process exit code

### LLM Calls
- `model` (string) - Model name
- `provider` (string) - LLM provider
- `retry_count` (string) - Number of retries
- `finish_reason` (string) - LLM finish reason
- `status_code` (string) - HTTP status code

### Engine
- `trajectory_id` (string) - Trajectory identifier
- `conversation_id` (string) - Conversation identifier
- `step_index` (string) - Step index
- `max_turns` (string) - Maximum turns configured

## Testing Checklist

SDK maintainers should verify:

- [ ] Proto code regenerated successfully
- [ ] Error handling code compiles with new schema
- [ ] Error metadata is accessible in error handlers
- [ ] Error code-based logic works correctly
- [ ] User-friendly error messages display properly
- [ ] Backward compatibility with old server works
- [ ] Forward compatibility with new server works
- [ ] Error recovery logic functions as expected
- [ ] All error codes are handled appropriately
- [ ] Metadata fields are parsed correctly

## Rollback Plan

If issues arise with the new schema:

1. **Revert Proto Changes:** Remove `metadata` field from proto definition
2. **Regenerate Proto Code:** Rebuild SDK with old schema
3. **Deploy Old Server:** Use previous LocalHarness binary version
4. **Communicate:** Notify users of temporary rollback

## Support Resources

- **Error Handling Guide:** [error-handling.md](error-handling.md)
- **Architecture:** [architecture.md](architecture.md#error-handling)
- **Go Error Package:** [internal/errors/errors.go](../internal/errors/errors.go)
- **Proto Schema:** [proto/localharness/v1/localharness.proto](../proto/localharness/v1/localharness.proto)

## Future Considerations

Potential future proto changes that SDK maintainers should be aware of:

1. **Additional Error Codes:** New error categories may be added
2. **Extended Metadata:** New context fields may be added
3. **Error Severity:** Potential `severity` field for error prioritization
4. **Error Recovery Hints:** Potential `recovery_strategy` field

SDK maintainers should design error handling to be extensible for future changes.

## Contact

For questions about proto schema changes or migration issues, contact:
- GitHub Issues: [github.com/divmora/localharness/issues](https://github.com/divmora/localharness/issues)
- Documentation: [docs/](../docs/)
