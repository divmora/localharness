package client

import (
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"sync"
	"time"

	"github.com/gorilla/websocket"
	"google.golang.org/protobuf/proto"

	pb "github.com/divmora/localharness/gen/go/localharness/v1"
	"github.com/divmora/localharness/internal/daemon"
)


// Client handles WebSocket communication between lhctl CLI/TUI and LocalHarness runtime.
type Client struct {
	conn       *websocket.Conn
	events     chan *pb.ServerMessage
	errors     chan error
	closed     chan struct{}
	closeOnce  sync.Once
	mu         sync.Mutex
	sessionID  string
	apiKey     string
	serverAddr string
	logger     *slog.Logger
}

// Config holds connection options.
type Config struct {
	URL       string // e.g. ws://127.0.0.1:8080 or empty for daemon auto-discovery
	APIKey    string
	SessionID string
	Logger    *slog.Logger
}

// New creates and connects a new Client.
func New(cfg Config) (*Client, error) {
	logger := cfg.Logger
	if logger == nil {
		logger = slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelWarn}))
	}

	c := &Client{
		events:    make(chan *pb.ServerMessage, 100),
		errors:    make(chan error, 10),
		closed:    make(chan struct{}),
		sessionID: cfg.SessionID,
		apiKey:    cfg.APIKey,
		logger:    logger,
	}

	wsURL := cfg.URL
	apiKey := cfg.APIKey

	if wsURL == "" {
		// Auto-discover running daemon
		running, info, err := daemon.IsDaemonRunning()
		if err != nil {
			return nil, fmt.Errorf("check daemon: %w", err)
		}
		if !running || info == nil {
			return nil, fmt.Errorf("localharness daemon is not running. Start with 'lhctl daemon start' or 'localharness daemon start'")
		}
		wsURL = fmt.Sprintf("ws://127.0.0.1:%d", info.Port)
		apiKey = info.APIKey
	}

	c.serverAddr = wsURL
	c.apiKey = apiKey

	headers := http.Header{}
	if apiKey != "" {
		headers.Set("x-localharness-api-key", apiKey)
	}
	if cfg.SessionID != "" {
		headers.Set("x-localharness-session-id", cfg.SessionID)
	}

	dialer := websocket.DefaultDialer
	conn, resp, err := dialer.Dial(wsURL, headers)
	if err != nil {
		if resp != nil {
			return nil, fmt.Errorf("websocket dial failed (status %d): %w", resp.StatusCode, err)
		}
		return nil, fmt.Errorf("websocket dial failed: %w", err)
	}

	c.conn = conn
	go c.readLoop()

	return c, nil
}

// ConnectOrStartDaemon connects to an existing daemon or starts one automatically.
func ConnectOrStartDaemon(logger *slog.Logger) (*Client, error) {
	return ConnectOrStartDaemonWithSession(logger, "")
}

// ConnectOrStartDaemonWithSession connects to an existing daemon with an optional target session ID.
func ConnectOrStartDaemonWithSession(logger *slog.Logger, sessionID string) (*Client, error) {
	running, info, _ := daemon.IsDaemonRunning()
	if !running || info == nil {
		// Attempt to start daemon
		daemonDir, err := daemon.GetDaemonDir()
		if err != nil {
			return nil, fmt.Errorf("get daemon dir: %w", err)
		}

		selfPath, err := os.Executable()
		if err != nil {
			selfPath = "localharness"
		}
		// If selfPath is lhctl, try to find localharness binary or use localharness command
		binDir := filepath.Dir(selfPath)
		harnessBin := filepath.Join(binDir, "localharness")
		if _, err := os.Stat(harnessBin); err != nil {
			harnessBin = "localharness"
		}

		logFile, err := os.OpenFile(filepath.Join(daemonDir, "daemon.log"), os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0644)
		if err == nil {
			defer logFile.Close()
		}

		cmd := exec.Command(harnessBin, "daemon", "run")
		if logFile != nil {
			cmd.Stdout = logFile
			cmd.Stderr = logFile
		}
		if err := cmd.Start(); err != nil {
			return nil, fmt.Errorf("failed to start localharness daemon: %w", err)
		}

		// Wait up to 3s for daemon to become ready
		ready := false
		for i := 0; i < 30; i++ {
			time.Sleep(100 * time.Millisecond)
			if r, inf, _ := daemon.IsDaemonRunning(); r && inf != nil {
				info = inf
				ready = true
				break
			}
		}
		if !ready {
			return nil, fmt.Errorf("timed out waiting for localharness daemon to start")
		}
	}

	return New(Config{
		URL:       fmt.Sprintf("ws://127.0.0.1:%d", info.Port),
		APIKey:    info.APIKey,
		SessionID: sessionID,
		Logger:    logger,
	})
}


func (c *Client) readLoop() {
	defer func() {
		c.Close()
		close(c.events)
		close(c.errors)
	}()

	for {
		select {
		case <-c.closed:
			return
		default:
		}

		msgType, data, err := c.conn.ReadMessage()
		if err != nil {
			select {
			case <-c.closed:
				return
			default:
			}
			c.errors <- fmt.Errorf("websocket read error: %w", err)
			return
		}

		if msgType != websocket.BinaryMessage {
			continue
		}

		var srvMsg pb.ServerMessage
		if err := proto.Unmarshal(data, &srvMsg); err != nil {
			c.logger.Warn("invalid protobuf from server", "error", err)
			continue
		}

		if srvMsg.GetInitResponse() != nil {
			c.sessionID = srvMsg.GetInitResponse().ConversationId
		}

		c.events <- &srvMsg
	}
}

// Events returns the channel of server messages.
func (c *Client) Events() <-chan *pb.ServerMessage {
	return c.events
}

// Errors returns the channel of connection errors.
func (c *Client) Errors() <-chan error {
	return c.errors
}

// SessionID returns the conversation / session ID.
func (c *Client) SessionID() string {
	return c.sessionID
}

func (c *Client) send(msg *pb.ClientMessage) error {
	c.mu.Lock()
	defer c.mu.Unlock()

	select {
	case <-c.closed:
		return fmt.Errorf("client is closed")
	default:
	}

	data, err := proto.Marshal(msg)
	if err != nil {
		return fmt.Errorf("marshal client message: %w", err)
	}

	return c.conn.WriteMessage(websocket.BinaryMessage, data)
}

// Init sends the initial configuration to the server.
func (c *Client) Init(cfg *pb.HarnessConfig) error {
	return c.send(&pb.ClientMessage{
		Payload: &pb.ClientMessage_Init{
			Init: &pb.InitRequest{
				Config: cfg,
			},
		},
	})
}

// SendUserMessage sends a user prompt message to the agent.
func (c *Client) SendUserMessage(content string, context *pb.UserContext, ephemeral []string) error {
	return c.send(&pb.ClientMessage{
		Payload: &pb.ClientMessage_UserMessage{
			UserMessage: &pb.UserMessage{
				Content:           content,
				ConversationId:    c.sessionID,
				Context:           context,
				EphemeralMessages: ephemeral,
			},
		},
	})
}

// SendPermissionResponse responds to a permission approval prompt with optional grant scope.
func (c *Client) SendPermissionResponse(requestID string, approved bool, reason string, scope pb.PermissionResponse_PermissionScope) error {
	return c.SendPermissionResponseWithSubcommands(requestID, approved, reason, scope, nil, nil)
}

// SendPermissionResponseWithSubcommands responds to a permission prompt with granular approved/denied sub-commands.
func (c *Client) SendPermissionResponseWithSubcommands(requestID string, approved bool, reason string, scope pb.PermissionResponse_PermissionScope, approvedSubs, deniedSubs []string) error {
	return c.send(&pb.ClientMessage{
		Payload: &pb.ClientMessage_PermissionResponse{
			PermissionResponse: &pb.PermissionResponse{
				RequestId:           requestID,
				Approved:            approved,
				DenialReason:        reason,
				Scope:               scope,
				ApprovedSubcommands: approvedSubs,
				DeniedSubcommands:   deniedSubs,
			},
		},
	})
}

// SendQuestionResponse responds to an interactive question.
func (c *Client) SendQuestionResponse(requestID string, answers []*pb.QuestionAnswer, skipped bool) error {
	return c.send(&pb.ClientMessage{
		Payload: &pb.ClientMessage_QuestionResponse{
			QuestionResponse: &pb.QuestionResponse{
				RequestId: requestID,
				Answers:   answers,
				Skipped:   skipped,
			},
		},
	})
}

// SendWorkspaceRequest adds, removes, or lists workspaces dynamically.
func (c *Client) SendWorkspaceRequest(action, path, name, corpus string) error {
	return c.send(&pb.ClientMessage{
		Payload: &pb.ClientMessage_WorkspaceRequest{
			WorkspaceRequest: &pb.WorkspaceRequest{
				Action:     action,
				Path:       path,
				Name:       name,
				CorpusName: corpus,
			},
		},
	})
}

// SendSetYoloMode toggles YOLO mode on the server.
func (c *Client) SendSetYoloMode(enabled bool) error {
	return c.send(&pb.ClientMessage{
		Payload: &pb.ClientMessage_SetYoloMode{
			SetYoloMode: &pb.SetYoloModeRequest{
				Enabled: enabled,
			},
		},
	})
}

// SendInterrupt requests graceful pause of the running turn.
func (c *Client) SendInterrupt() error {
	return c.send(&pb.ClientMessage{
		Payload: &pb.ClientMessage_Interrupt{
			Interrupt: &pb.InterruptRequest{},
		},
	})
}

// SendResume requests resuming a paused turn with an optional message.
func (c *Client) SendResume(message string) error {
	return c.send(&pb.ClientMessage{
		Payload: &pb.ClientMessage_Resume{
			Resume: &pb.ResumeRequest{
				Message: message,
			},
		},
	})
}

// SendCancel requests aborting the current turn.
func (c *Client) SendCancel() error {
	return c.send(&pb.ClientMessage{
		Payload: &pb.ClientMessage_Cancel{
			Cancel: &pb.CancelRequest{},
		},
	})
}

// Close closes the WebSocket connection.
func (c *Client) Close() error {
	c.closeOnce.Do(func() {
		close(c.closed)
		if c.conn != nil {
			_ = c.conn.Close()
		}
	})
	return nil
}
