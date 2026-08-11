package engine

import (
	"context"
	"fmt"

	pb "github.com/divmora/localharness/gen/go/localharness/v1"
	"github.com/divmora/localharness/internal/llm"
)

// executePublish handles the publish tool call.
// It sends a BusMessage to the AgentBus so peer agents receive a notification
// about the published artifact. The message includes the sender's role, conv ID,
// a summary, optional artifact path, and tags.
func (e *Engine) executePublish(_ context.Context, tc llm.ToolCall, step *pb.StepUpdate) error {
	if e.agentBus == nil {
		// No bus — not fatal, just tell the LLM
		result := "publish: no agent bus available — you are running as a standalone agent without peers."
		e.history = append(e.history, toolResultMsg(tc, result, true))
		step.Text = result
		step.State = pb.StepUpdate_STATE_ERROR
		step.ErrorInfo = &pb.ErrorInfo{
			Message: result,
			Code:    "NO_AGENT_BUS",
		}
		e.emitStep(step)
		return nil
	}

	// Extract args
	summary, _ := tc.Args["summary"].(string)
	if summary == "" {
		result := "Error: summary is required"
		e.history = append(e.history, toolResultMsg(tc, result, true))
		step.Text = result
		step.State = pb.StepUpdate_STATE_ERROR
		e.emitStep(step)
		return nil
	}

	artifactPath, _ := tc.Args["artifact_path"].(string)

	var tags []string
	if tagsRaw, ok := tc.Args["tags"]; ok {
		if tagsSlice, ok := tagsRaw.([]interface{}); ok {
			for _, t := range tagsSlice {
				if s, ok := t.(string); ok {
					tags = append(tags, s)
				}
			}
		}
	}

	// Build and publish message.
	// Use agent role from conversation state, fallback to "agent".
	fromRole := "agent"
	if e.conv != nil && e.conv.State != nil && e.conv.State.AgentRole != "" {
		fromRole = e.conv.State.AgentRole
	}

	msg := BusMessage{
		From:    fromRole,
		FromID:  e.convID,
		Summary: summary,
		Path:    artifactPath,
		Tags:    tags,
	}

	e.agentBus.Publish(msg)

	// Build result text
	result := fmt.Sprintf("Published to agent bus: %q", summary)
	if artifactPath != "" {
		result += fmt.Sprintf("\nArtifact: %s", artifactPath)
	}
	if len(tags) > 0 {
		result += fmt.Sprintf("\nTags: %v", tags)
	}
	result += fmt.Sprintf("\nListeners: %d", e.agentBus.ListenerCount())

	e.logger.Info("published to agent bus",
		"summary", summary,
		"artifact_path", artifactPath,
		"tags", tags,
		"from", fromRole,
		"listeners", e.agentBus.ListenerCount(),
	)

	// Feed result to LLM history + emit step
	e.history = append(e.history, toolResultMsg(tc, result, false))
	step.Text = result
	step.State = pb.StepUpdate_STATE_DONE
	e.emitStep(step)

	return nil
}
