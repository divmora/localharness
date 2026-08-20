package tui

import (
	"time"

	pb "github.com/divmora/localharness/gen/go/localharness/v1"
)

// ServerEventMsg wraps an incoming ServerMessage from the daemon/runtime.
type ServerEventMsg struct {
	Msg *pb.ServerMessage
}

// WSErrorMsg wraps a WebSocket error.
type WSErrorMsg struct {
	Err error
}

// SpinnerTickMsg triggers spinner frame update.
type SpinnerTickMsg struct {
	Time time.Time
}

// DurationTickMsg triggers active tool execution duration counter update.
type DurationTickMsg struct {
	Time time.Time
}

// SubagentsUpdatedMsg signals subagent registry change.
type SubagentsUpdatedMsg struct{}

// ApprovalPromptMsg signals an incoming approval request.
type ApprovalPromptMsg struct {
	RequestID   string
	ToolName    string
	Description string
	DiffPreview string
}

// CloseAppMsg signals exiting the application.
type CloseAppMsg struct{}

// DetachAppMsg signals detaching without stopping the agent.
type DetachAppMsg struct{}
