package tools

import (
	"fmt"
	"time"
)

// SystemMessage represents a system-generated notification for the agent.
// It is used by TaskManager (task completions) and ScheduleManager (timer/cron fires)
// to notify the engine, which injects them as <SYSTEM_MESSAGE> blocks in the
// enriched user message.
type SystemMessage struct {
	// Source indicates what generated this message.
	// Values: "timer", "cron", "task_complete", "task_failed", "task_killed"
	Source string

	// TaskID is the identifier of the task or schedule that generated this message.
	TaskID string

	// Content is the human-readable message for the agent.
	Content string

	// FiredAt is when this notification was generated.
	FiredAt time.Time
}

// FormatForPrompt renders the SystemMessage as a string suitable for injection
// into the enriched user prompt.
func (m SystemMessage) FormatForPrompt() string {
	return fmt.Sprintf("[%s] Task %s:\n%s", m.Source, m.TaskID, m.Content)
}
