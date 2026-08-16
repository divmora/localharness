package connection

import (
	"context"
	"encoding/binary"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"os/exec"
	"sync"
	"syscall"
	"time"

	"github.com/gorilla/websocket"
	"google.golang.org/protobuf/proto"

	pb "github.com/divmora/localharness/gen/go/localharness/v1"
)

// LocalConfig holds the configuration parameters needed to initialize
// a LocalConnection.
type LocalConfig struct {
	BinaryPath string
	Workspaces []string
	Config     *pb.HarnessConfig
	Debug      bool
}

// LocalConnection implements connection.Connection to manage a local
// localharness process and communicate with it over WebSocket.
//
// The connection is established via a pipe-based handshake:
//  1. SDK spawns the binary, capturing stdin/stdout/stderr pipes
//  2. SDK writes InputConfig to stdin (4-byte LE length + protobuf)
//  3. Binary binds localhost:0, generates API key, writes OutputConfig to stdout
//  4. SDK connects via WebSocket with x-localharness-api-key header
type LocalConnection struct {
	cmd            *exec.Cmd
	conn           *websocket.Conn
	conversationID string
	harnessVersion string
	apiKey         string
	baseURL        string         // HTTP base URL (http://localhost:<port>)
	stepsCh        chan Step
	mu             sync.Mutex // Protects lifecycle state and write access to conn
	stepsMu        sync.Mutex // Protects stepsCh
	logger         *slog.Logger
	stdinPipe      io.WriteCloser
	stderrBuf      *stderrRingBuffer
	done           chan struct{} // Closed when the read loop exits
	running        bool
}

// NewLocalConnection starts the localharness process, performs the pipe-based
// handshake, connects via WebSocket with API key auth, and sends the InitRequest.
func NewLocalConnection(ctx context.Context, cfg LocalConfig, logger *slog.Logger) (*LocalConnection, error) {
	if logger == nil {
		logger = slog.Default()
	}

	// 1. Resolve Binary Path (universal resolution chain)
	resolver := &BinaryResolver{
		Logger: logger,
	}
	binaryPath, err := resolver.Resolve(cfg.BinaryPath)
	if err != nil {
		logger.Error("cannot find localharness binary", "error", err)
		return nil, fmt.Errorf("cannot find localharness binary: %w", err)
	}

	// 2. Build command args (only non-port/host flags remain)
	var args []string
	if len(cfg.Workspaces) > 0 {
		args = append(args, "--workspace", cfg.Workspaces[0])
	}
	if cfg.Debug {
		args = append(args, "--debug")
	}

	// 3. Start process with captured pipes
	cmd := exec.Command(binaryPath, args...)

	stdinPipe, err := cmd.StdinPipe()
	if err != nil {
		return nil, fmt.Errorf("failed to create stdin pipe: %w", err)
	}

	stdoutPipe, err := cmd.StdoutPipe()
	if err != nil {
		return nil, fmt.Errorf("failed to create stdout pipe: %w", err)
	}

	stderrPipe, err := cmd.StderrPipe()
	if err != nil {
		return nil, fmt.Errorf("failed to create stderr pipe: %w", err)
	}

	logger.Debug("Starting localharness process", "path", binaryPath, "args", args)
	if err := cmd.Start(); err != nil {
		return nil, fmt.Errorf("failed to start localharness server process: %w", err)
	}

	// 4. Start stderr capture goroutine
	stderrBuf := newStderrRingBuffer(100)
	go stderrBuf.Capture(stderrPipe)

	// 5. Write InputConfig to stdin (pipe handshake step 1)
	inputCfg := &pb.InputConfig{
		Debug: cfg.Debug,
	}
	if len(cfg.Workspaces) > 0 {
		inputCfg.Workspace = cfg.Workspaces[0]
	}

	if err := writeInputConfig(stdinPipe, inputCfg); err != nil {
		cmd.Process.Kill()
		cmd.Wait()
		logger.Error("failed to write InputConfig to stdin", "error", err)
		return nil, fmt.Errorf("failed to write InputConfig to stdin: %w", err)
	}

	// 6. Read OutputConfig from stdout (pipe handshake step 2)
	outputCfg, err := readOutputConfig(stdoutPipe)
	if err != nil {
		stderrTail := stderrBuf.String()
		cmd.Process.Kill()
		cmd.Wait()
		logger.Error("failed to read OutputConfig from stdout", "error", err, "harness_stderr", stderrTail)
		return nil, fmt.Errorf("failed to read OutputConfig from stdout: %w\nHarness stderr:\n%s", err, stderrTail)
	}

	port := int(outputCfg.Port)
	apiKey := outputCfg.ApiKey
	harnessVersion := outputCfg.HarnessVersion

	logger.Debug("Pipe handshake complete",
		"port", port,
		"api_key_len", len(apiKey),
		"harness_version", harnessVersion,
	)

	// 7. Connect via WebSocket with API key auth header
	baseURL := fmt.Sprintf("http://localhost:%d", port)
	url := fmt.Sprintf("ws://localhost:%d/", port)
	headers := http.Header{}
	headers.Set("x-localharness-api-key", apiKey)

	var wsConn *websocket.Conn
	var dialErr error

	logger.Debug("Dialing localharness WebSocket server", "url", url)
	for i := 0; i < 40; i++ {
		dialCtx, dialCancel := context.WithTimeout(ctx, 200*time.Millisecond)
		wsConn, _, dialErr = websocket.DefaultDialer.DialContext(dialCtx, url, headers)
		dialCancel()
		if dialErr == nil {
			break
		}
		select {
		case <-ctx.Done():
			stdinPipe.Close()
			cmd.Process.Kill()
			cmd.Wait()
			return nil, ctx.Err()
		case <-time.After(100 * time.Millisecond):
		}
	}

	if wsConn == nil {
		stderrTail := stderrBuf.String()
		stdinPipe.Close()
		cmd.Process.Kill()
		cmd.Wait()
		logger.Error("failed to connect to localharness WebSocket", "url", url, "error", dialErr, "harness_stderr", stderrTail)
		return nil, fmt.Errorf("failed to connect to localharness at %s after retries: %w\nHarness stderr:\n%s", url, dialErr, stderrTail)
	}

	c := &LocalConnection{
		cmd:       cmd,
		conn:      wsConn,
		apiKey:    apiKey,
		baseURL:   baseURL,
		logger:    logger,
		stdinPipe: stdinPipe,
		stderrBuf: stderrBuf,
		done:      make(chan struct{}),
		running:   true,
	}

	// 8. Send InitRequest
	initMsg := &pb.ClientMessage{
		Payload: &pb.ClientMessage_Init{
			Init: &pb.InitRequest{
				Config: cfg.Config,
			},
		},
	}
	initData, err := proto.Marshal(initMsg)
	if err != nil {
		c.Close()
		return nil, fmt.Errorf("failed to marshal InitRequest: %w", err)
	}

	if err := c.conn.WriteMessage(websocket.BinaryMessage, initData); err != nil {
		c.Close()
		return nil, fmt.Errorf("failed to send InitRequest over WebSocket: %w", err)
	}

	// 9. Read InitResponse
	_, data, err := c.conn.ReadMessage()
	if err != nil {
		c.Close()
		return nil, fmt.Errorf("failed to read InitResponse from WebSocket: %w", err)
	}

	var initResp pb.ServerMessage
	if err := proto.Unmarshal(data, &initResp); err != nil {
		c.Close()
		return nil, fmt.Errorf("failed to unmarshal InitResponse: %w", err)
	}

	switch p := initResp.Payload.(type) {
	case *pb.ServerMessage_InitResponse:
		c.conversationID = p.InitResponse.ConversationId
		c.harnessVersion = p.InitResponse.HarnessVersion
		logger.Debug("LocalHarness connection initialized successfully", "conversation_id", c.conversationID, "version", c.harnessVersion)
	case *pb.ServerMessage_Error:
		c.Close()
		// Extract structured error information from protobuf
		errorCode := p.Error.Code
		errorMessage := p.Error.Message
		errorMetadata := p.Error.Metadata

		logger.Error("harness initialization failed",
			"error_code", errorCode,
			"error_message", errorMessage,
			"error_metadata", errorMetadata)

		// Return error with code and context
		return nil, fmt.Errorf("harness initialization failed: [%s] %s", errorCode, errorMessage)
	default:
		c.Close()
		return nil, fmt.Errorf("unexpected message type received during initialization: %T", initResp.Payload)
	}

	// 10. Configure WebSocket keepalive.
	// The server sends periodic pings; gorilla/websocket auto-responds with pongs.
	// Set a read deadline that resets on each ping to detect server death.
	// 45s pong timeout matches the server's wsPongTimeout constant.
	const wsPongTimeout = 45 * time.Second
	wsConn.SetReadDeadline(time.Now().Add(wsPongTimeout))
	wsConn.SetPingHandler(func(appData string) error {
		// Reset read deadline on each ping from server
		wsConn.SetReadDeadline(time.Now().Add(wsPongTimeout))
		// Send pong response (gorilla requires explicit handler to reset deadline)
		return wsConn.WriteControl(websocket.PongMessage, []byte(appData), time.Now().Add(5*time.Second))
	})

	// 11. Launch background read loop
	go c.readLoop(ctx)

	return c, nil
}

// writeInputConfig writes a length-prefixed protobuf InputConfig to w.
// Wire format: 4-byte little-endian length + protobuf payload.
func writeInputConfig(w io.Writer, cfg *pb.InputConfig) error {
	data, err := proto.Marshal(cfg)
	if err != nil {
		return fmt.Errorf("marshal InputConfig: %w", err)
	}

	length := uint32(len(data))
	if err := binary.Write(w, binary.LittleEndian, length); err != nil {
		return fmt.Errorf("write length prefix: %w", err)
	}

	if _, err := w.Write(data); err != nil {
		return fmt.Errorf("write payload: %w", err)
	}

	return nil
}

// readOutputConfig reads a length-prefixed protobuf OutputConfig from r.
// Wire format: 4-byte little-endian length + protobuf payload.
func readOutputConfig(r io.Reader) (*pb.OutputConfig, error) {
	var length uint32
	if err := binary.Read(r, binary.LittleEndian, &length); err != nil {
		return nil, fmt.Errorf("read length prefix: %w", err)
	}

	if length > 1024*1024 { // 1MB sanity limit
		return nil, fmt.Errorf("OutputConfig too large: %d bytes", length)
	}

	buf := make([]byte, length)
	if _, err := io.ReadFull(r, buf); err != nil {
		return nil, fmt.Errorf("read payload: %w", err)
	}

	var cfg pb.OutputConfig
	if err := proto.Unmarshal(buf, &cfg); err != nil {
		return nil, fmt.Errorf("unmarshal OutputConfig: %w", err)
	}

	return &cfg, nil
}

// readLoop reads messages from the WebSocket connection in a background loop.
func (c *LocalConnection) readLoop(ctx context.Context) {
	defer close(c.done)
	for {
		_, data, err := c.conn.ReadMessage()
		if err != nil {
			c.mu.Lock()
			running := c.running
			c.mu.Unlock()
			if !running {
				return
			}
			stderrTail := c.stderrBuf.String()
			c.logger.Debug("WebSocket read loop exited",
				"error", err,
				"harness_stderr", stderrTail,
			)
			c.closeStepsCh()
			return
		}

		var msg pb.ServerMessage
		if err := proto.Unmarshal(data, &msg); err != nil {
			c.logger.Error("failed to unmarshal ServerMessage in read loop", "error", err)
			continue
		}

		switch p := msg.Payload.(type) {
		case *pb.ServerMessage_StepUpdate:
			c.handleStepUpdate(p.StepUpdate)
		case *pb.ServerMessage_TrajectoryState:
			c.handleTrajectoryState(p.TrajectoryState)
		case *pb.ServerMessage_Error:
			c.handleErrorEvent(p.Error)
		}
	}
}

func (c *LocalConnection) handleStepUpdate(su *pb.StepUpdate) {
	step := Step{
		Index:         su.StepIndex,
		Text:          su.Text,
		TextDelta:     su.TextDelta,
		Thinking:      su.Thinking,
		ThinkingDelta: su.ThinkingDelta,
		Source:        StepSource(su.Source),
		State:         StepState(su.State),
	}

	if su.ErrorInfo != nil {
		step.ErrorMessage = su.ErrorInfo.Message
		step.ErrorCode = su.ErrorInfo.Code
		step.ErrorMetadata = su.ErrorInfo.Metadata
	}

	if su.Usage != nil {
		step.Usage = &UsageMetadata{
			PromptTokens:     int(su.Usage.PromptTokens),
			CompletionTokens: int(su.Usage.CompletionTokens),
			ThinkingTokens:   int(su.Usage.ThinkingTokens),
			TotalTokens:      int(su.Usage.TotalTokens),
			CachedTokens:     int(su.Usage.CachedTokens),
		}
	}

	if su.Action != nil {
		switch a := su.Action.(type) {
		case *pb.StepUpdate_ViewFile:
			step.ToolName = "view_file"
			b, _ := json.Marshal(a.ViewFile)
			step.ToolArgsJSON = string(b)
		case *pb.StepUpdate_WriteToFile:
			step.ToolName = "write_to_file"
			b, _ := json.Marshal(a.WriteToFile)
			step.ToolArgsJSON = string(b)
		case *pb.StepUpdate_ReplaceFileContent:
			step.ToolName = "replace_file_content"
			b, _ := json.Marshal(a.ReplaceFileContent)
			step.ToolArgsJSON = string(b)
		case *pb.StepUpdate_ListDir:
			step.ToolName = "list_dir"
			b, _ := json.Marshal(a.ListDir)
			step.ToolArgsJSON = string(b)
		case *pb.StepUpdate_GrepSearch:
			step.ToolName = "grep_search"
			b, _ := json.Marshal(a.GrepSearch)
			step.ToolArgsJSON = string(b)
		case *pb.StepUpdate_FindFile:
			step.ToolName = "find_file"
			b, _ := json.Marshal(a.FindFile)
			step.ToolArgsJSON = string(b)
		case *pb.StepUpdate_RunCommand:
			step.ToolName = "run_command"
			b, _ := json.Marshal(a.RunCommand)
			step.ToolArgsJSON = string(b)
		case *pb.StepUpdate_ManageTask:
			step.ToolName = "manage_task"
			b, _ := json.Marshal(a.ManageTask)
			step.ToolArgsJSON = string(b)
		case *pb.StepUpdate_Finish:
			step.ToolName = "finish"
			b, _ := json.Marshal(a.Finish)
			step.ToolArgsJSON = string(b)
		case *pb.StepUpdate_InvokeSubagent:
			step.ToolName = "invoke_subagent"
			b, _ := json.Marshal(a.InvokeSubagent)
			step.ToolArgsJSON = string(b)
		case *pb.StepUpdate_SearchWeb:
			step.ToolName = "search_web"
			b, _ := json.Marshal(a.SearchWeb)
			step.ToolArgsJSON = string(b)
		case *pb.StepUpdate_ReadUrlContent:
			step.ToolName = "read_url_content"
			b, _ := json.Marshal(a.ReadUrlContent)
			step.ToolArgsJSON = string(b)
		case *pb.StepUpdate_HostToolCall:
			step.ToolName = a.HostToolCall.ToolName
			step.ToolArgsJSON = a.HostToolCall.ArgsJson
			step.IsHostToolCall = true
		case *pb.StepUpdate_PermissionRequest:
			step.PermissionRequestID = a.PermissionRequest.RequestId
			step.PermissionToolName = a.PermissionRequest.ToolName
			step.PermissionArgsSummary = a.PermissionRequest.ArgsSummary
			step.ToolArgsJSON = a.PermissionRequest.ArgsJson
		case *pb.StepUpdate_UserQuestion:
			step.QuestionRequestID = a.UserQuestion.RequestId
			step.ToolName = "ask_question"
			for _, q := range a.UserQuestion.Questions {
				step.Questions = append(step.Questions, UserQuestion{
					Question:      q.Question,
					Options:       q.Options,
					IsMultiSelect: q.IsMultiSelect,
				})
			}
		case *pb.StepUpdate_Compaction:
			step.CompactionOriginalTokens = int(a.Compaction.OriginalTokens)
			step.CompactionCompactedTokens = int(a.Compaction.CompactedTokens)
			step.CompactionMessagesRemoved = int(a.Compaction.MessagesRemoved)
			step.CompactionSummary = a.Compaction.Summary
		}
	}

	// Extract tool result content from completed tool steps.
	// The engine puts the tool output in the step Text field when state=DONE.
	if step.ToolName != "" && step.State == StateDone && step.Text != "" {
		step.ToolResultContent = step.Text
	}
	// Tool error steps have STATE_ERROR with error info
	if step.ToolName != "" && step.State == StateError {
		step.ToolResultIsError = true
		step.ToolResultContent = step.ErrorMessage
	}

	// Any model text response (not calling tools) in a step is considered final
	if step.Source == SourceModel && step.State == StateDone && step.ToolName == "" && step.Text != "" {
		step.IsFinal = true
	}

	c.stepsMu.Lock()
	ch := c.stepsCh
	c.stepsMu.Unlock()

	if ch != nil {
		select {
		case ch <- step:
		default:
			c.logger.Warn("steps channel buffer full, blocking on write")
			ch <- step
		}
	}
}

func (c *LocalConnection) handleTrajectoryState(ts *pb.TrajectoryState) {
	c.logger.Debug("received trajectory state",
		"state", ts.State.String(),
		"parent", ts.ParentTrajectoryId,
		"trajectory_id", ts.TrajectoryId,
	)
	// Root trajectory state change to idle/done/error signals the end of the Chat turn.
	if ts.ParentTrajectoryId == "" {
		if ts.State == pb.TrajectoryState_TRAJ_IDLE ||
			ts.State == pb.TrajectoryState_TRAJ_COMPLETED ||
			ts.State == pb.TrajectoryState_TRAJ_ERROR {
			if ts.State == pb.TrajectoryState_TRAJ_ERROR {
				c.logger.Warn("trajectory ended with ERROR state",
					"trajectory_id", ts.TrajectoryId,
				)
			}
			c.closeStepsCh()
		}
	}
}

func (c *LocalConnection) handleErrorEvent(ee *pb.ErrorEvent) {
	step := Step{
		State:        StateError,
		ErrorMessage: ee.Message,
		ErrorCode:    ee.Code,
		ErrorMetadata: ee.Metadata,
	}

	c.logger.Error("harness error event",
		"error_code", ee.Code,
		"error_message", ee.Message,
		"error_fatal", ee.Fatal,
		"error_metadata", ee.Metadata)

	c.stepsMu.Lock()
	ch := c.stepsCh
	c.stepsMu.Unlock()

	if ch != nil {
		select {
		case ch <- step:
		default:
			ch <- step
		}
	}

	if ee.Fatal {
		c.closeStepsCh()
	}
}

func (c *LocalConnection) closeStepsCh() {
	c.stepsMu.Lock()
	defer c.stepsMu.Unlock()
	if c.stepsCh != nil {
		close(c.stepsCh)
		c.stepsCh = nil
	}
}

// Send sends a user prompt (UserMessage) to the localharness server.
func (c *LocalConnection) Send(ctx context.Context, prompt string) error {
	return c.SendWithContext(ctx, prompt, nil, nil)
}

// SendWithContext sends a user prompt with optional per-message host context and ephemeral messages.
func (c *LocalConnection) SendWithContext(ctx context.Context, prompt string, userCtx *pb.UserContext, ephemeralMsgs []string) error {
	c.stepsMu.Lock()
	if c.stepsCh != nil {
		close(c.stepsCh)
	}
	c.stepsCh = make(chan Step, 1000)
	c.stepsMu.Unlock()

	msg := &pb.ClientMessage{
		Payload: &pb.ClientMessage_UserMessage{
			UserMessage: &pb.UserMessage{
				Content:           prompt,
				Context:           userCtx,
				EphemeralMessages: ephemeralMsgs,
			},
		},
	}

	data, err := proto.Marshal(msg)
	if err != nil {
		return fmt.Errorf("failed to marshal UserMessage: %w", err)
	}

	c.mu.Lock()
	defer c.mu.Unlock()
	if !c.running {
		return fmt.Errorf("connection is not running")
	}

	if err := c.conn.WriteMessage(websocket.BinaryMessage, data); err != nil {
		return fmt.Errorf("failed to write UserMessage to WebSocket: %w", err)
	}
	return nil
}

// ReceiveSteps returns the channel streaming connection.Step events for the turn.
func (c *LocalConnection) ReceiveSteps(ctx context.Context) (<-chan Step, error) {
	c.stepsMu.Lock()
	defer c.stepsMu.Unlock()
	if c.stepsCh == nil {
		c.stepsCh = make(chan Step, 1000)
	}
	return c.stepsCh, nil
}

// SendPermissionResponse sends approval/denial decision back to the localharness server.
func (c *LocalConnection) SendPermissionResponse(ctx context.Context, requestID string, approved bool, reason string) error {
	msg := &pb.ClientMessage{
		Payload: &pb.ClientMessage_PermissionResponse{
			PermissionResponse: &pb.PermissionResponse{
				RequestId:    requestID,
				Approved:     approved,
				DenialReason: reason,
			},
		},
	}

	data, err := proto.Marshal(msg)
	if err != nil {
		return fmt.Errorf("failed to marshal PermissionResponse: %w", err)
	}

	c.mu.Lock()
	defer c.mu.Unlock()
	if !c.running {
		return fmt.Errorf("connection is not running")
	}

	if err := c.conn.WriteMessage(websocket.BinaryMessage, data); err != nil {
		return fmt.Errorf("failed to write PermissionResponse to WebSocket: %w", err)
	}
	return nil
}

// SendQuestionResponse sends the user's answers back to the localharness server.
func (c *LocalConnection) SendQuestionResponse(ctx context.Context, requestID string, answers []*QuestionAnswer, skipped bool) error {
	// Convert SDK QuestionAnswer to proto QuestionAnswer
	pbAnswers := make([]*pb.QuestionAnswer, len(answers))
	for i, a := range answers {
		pbAnswers[i] = &pb.QuestionAnswer{
			SelectedIndices: a.SelectedIndices,
			SelectedOptions: a.SelectedOptions,
			Text:            a.Text,
		}
	}

	msg := &pb.ClientMessage{
		Payload: &pb.ClientMessage_QuestionResponse{
			QuestionResponse: &pb.QuestionResponse{
				RequestId: requestID,
				Answers:   pbAnswers,
				Skipped:   skipped,
			},
		},
	}

	data, err := proto.Marshal(msg)
	if err != nil {
		return fmt.Errorf("failed to marshal QuestionResponse: %w", err)
	}

	c.mu.Lock()
	defer c.mu.Unlock()
	if !c.running {
		return fmt.Errorf("connection is not running")
	}

	if err := c.conn.WriteMessage(websocket.BinaryMessage, data); err != nil {
		return fmt.Errorf("failed to write QuestionResponse to WebSocket: %w", err)
	}
	return nil
}

// SendToolResult sends a custom tool execution result back to the localharness server.
func (c *LocalConnection) SendToolResult(ctx context.Context, stepID, toolName, resultJSON string, isError bool) error {
	msg := &pb.ClientMessage{
		Payload: &pb.ClientMessage_HostToolResult{
			HostToolResult: &pb.ToolResult{
				StepId:     stepID,
				ToolName:   toolName,
				ResultJson: resultJSON,
				IsError:    isError,
			},
		},
	}

	data, err := proto.Marshal(msg)
	if err != nil {
		return fmt.Errorf("failed to marshal HostToolResult: %w", err)
	}

	c.mu.Lock()
	defer c.mu.Unlock()
	if !c.running {
		return fmt.Errorf("connection is not running")
	}

	if err := c.conn.WriteMessage(websocket.BinaryMessage, data); err != nil {
		return fmt.Errorf("failed to write HostToolResult to WebSocket: %w", err)
	}
	return nil
}

// Close shuts down the connection with graceful multi-phase teardown.
//
// Phase 1: Close WebSocket cleanly (sends close frame)
// Phase 2: Wait for read loop to exit (confirms server received close)
// Phase 3: Close stdin (signals Go binary to initiate graceful shutdown)
// Phase 4: Wait up to 5s for graceful exit → SIGTERM → SIGKILL
func (c *LocalConnection) Close() error {
	c.mu.Lock()
	if !c.running {
		c.mu.Unlock()
		return nil
	}
	c.running = false
	c.mu.Unlock()

	c.closeStepsCh()

	// Phase 1: Clean WebSocket close handshake
	var wsErr error
	if c.conn != nil {
		closeMsg := websocket.FormatCloseMessage(websocket.CloseNormalClosure, "closing session")
		c.conn.WriteMessage(websocket.CloseMessage, closeMsg)
		wsErr = c.conn.Close()
	}

	// Phase 2: Wait for read loop to exit — this ensures the server-side
	// session has processed the WebSocket close and its cleanup() has a
	// chance to wait for in-flight turns before we kill the process.
	select {
	case <-c.done:
	case <-time.After(3 * time.Second):
		c.logger.Debug("read loop did not exit within 3s, proceeding to stdin close")
	}

	// Phase 3: Close stdin (triggers binary's watchStdinEOF → context cancel → graceful shutdown)
	if c.stdinPipe != nil {
		c.stdinPipe.Close()
	}

	// Phase 4: Wait up to 5s for the process to exit gracefully
	if c.cmd != nil && c.cmd.Process != nil {
		exited := make(chan struct{})
		go func() {
			c.cmd.Wait()
			close(exited)
		}()

		select {
		case <-exited:
			// Clean exit — binary handled shutdown gracefully
			if wsErr != nil {
				return fmt.Errorf("WebSocket close error: %w", wsErr)
			}
			return nil
		case <-time.After(5 * time.Second):
		}

		// Escalate — SIGTERM → wait 1s → SIGKILL
		c.logger.Debug("process did not exit within 5s, sending SIGTERM")
		c.cmd.Process.Signal(syscall.SIGTERM)

		select {
		case <-exited:
			if wsErr != nil {
				return fmt.Errorf("WebSocket close error: %w", wsErr)
			}
			return nil
		case <-time.After(1 * time.Second):
		}

		c.logger.Debug("process did not exit within 1s of SIGTERM, sending SIGKILL")
		c.cmd.Process.Kill()
		<-exited
	}

	if wsErr != nil {
		return fmt.Errorf("WebSocket close error: %w", wsErr)
	}
	return nil
}

// ConversationID returns the active conversation identifier.
func (c *LocalConnection) ConversationID() string {
	return c.conversationID
}

// StderrOutput returns the captured stderr output from the harness process.
// Useful for diagnostics when the connection fails.
func (c *LocalConnection) StderrOutput() string {
	if c.stderrBuf != nil {
		return c.stderrBuf.String()
	}
	return "(no stderr output)"
}

// FetchAgentCard fetches the A2A agent card from the harness HTTP endpoint.
func (c *LocalConnection) FetchAgentCard(ctx context.Context) (*AgentCard, error) {
	if c.baseURL == "" {
		return nil, fmt.Errorf("no base URL available for agent card fetch")
	}

	url := c.baseURL + "/.well-known/agent.json"
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, fmt.Errorf("failed to create agent card request: %w", err)
	}

	if c.apiKey != "" {
		req.Header.Set("x-localharness-api-key", c.apiKey)
	}

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("failed to fetch agent card: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("agent card request failed with status %d", resp.StatusCode)
	}

	var card AgentCard
	if err := json.NewDecoder(resp.Body).Decode(&card); err != nil {
		return nil, fmt.Errorf("failed to decode agent card: %w", err)
	}

	return &card, nil
}
