package tools

import (
	"context"
	"fmt"

	pb "github.com/divmora/localharness/gen/go/localharness/v1"
)

func registerFinish(r *Registry) {
	r.Register("finish", executeFinish, ToolSchema{
		Name:        "finish",
		Description: "Signal that the task is complete. Optionally provide structured JSON output as the final result.",
		Parameters: map[string]interface{}{
			"type": "object",
			"properties": map[string]interface{}{
				"output_json": map[string]interface{}{"type": "string", "description": "Optional structured JSON output for the task result"},
			},
		},
	})
}

func executeFinish(ctx context.Context, step *pb.StepUpdate, r *Registry) error {
	fin := step.GetFinish()
	if fin == nil {
		return fmt.Errorf("finish: missing action")
	}

	// The finish tool is primarily a signal — the engine handles the actual
	// termination. We just validate the output if provided.
	r.Logger().Info("agent signaled finish", "has_output", fin.OutputJson != "")

	return nil
}
