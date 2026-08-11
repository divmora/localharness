package tools

import (
	"context"
	"fmt"

	pb "github.com/divmora/localharness/gen/go/localharness/v1"
)

func registerAskQuestion(r *Registry) {
	r.Register("ask_question", executeAskQuestion, ToolSchema{
		Name:        "ask_question",
		Description: "Ask the user one or more multiple-choice questions for clarification, design feedback, or resolving ambiguity. Execution blocks until the user responds or skips.",
		Parameters: map[string]interface{}{
			"type": "object",
			"properties": map[string]interface{}{
				"questions": map[string]interface{}{
					"type":        "array",
					"description": "List of questions to ask the user.",
					"items": map[string]interface{}{
						"type": "object",
						"properties": map[string]interface{}{
							"question": map[string]interface{}{
								"type":        "string",
								"description": "The question text.",
							},
							"options": map[string]interface{}{
								"type":        "array",
								"description": "The selectable options. Must have at least 2.",
								"items":       map[string]interface{}{"type": "string"},
							},
							"is_multi_select": map[string]interface{}{
								"type":        "boolean",
								"description": "If true, user can select multiple options.",
							},
						},
						"required": []string{"question", "options"},
					},
				},
			},
			"required": []string{"questions"},
		},
	})
}

// executeAskQuestion is a special tool — it does NOT actually execute here.
// The engine intercepts it and handles it via the QuestionHandler callback.
// This function is registered to provide the JSON schema but the actual
// handling happens in engine.go's executeAskQuestion method.
func executeAskQuestion(ctx context.Context, step *pb.StepUpdate, r *Registry) error {
	return fmt.Errorf("ask_question should be handled by the engine, not the tool registry")
}
