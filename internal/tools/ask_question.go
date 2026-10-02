package tools

import (
	"context"
	"fmt"

	pb "github.com/divmora/localharness/gen/go/localharness/v1"
)

func registerAskQuestion(r *Registry) {
	r.Register("ask_question", executeAskQuestion, ToolSchema{
		Name: "ask_question",
		Description: "Use this tool to ask the user one or more multiple-choice questions, with the goal of:\n" +
			"- Clarifying underspecified requirements\n" +
			"- Soliciting design feedback or user preferences\n" +
			"- Addressing ambiguous user intent\n" +
			"- Picking a solution from a list of options\n\n" +
			"When called, this tool renders an interactive modal containing the question, selectable options, a default write-in option, and Submit/Skip buttons. Execution is blocked until the user responds.\n\n" +
			"Guidance:\n" +
			"- When specifying files in the question, use github markdown links (e.g. [filename](file:///path/to/file)).\n" +
			"- Don't use this tool to ask trivial questions that can be answered with a single word (e.g. yes/no); output regular text to ask these questions.\n" +
			"- Don't include an 'other' option for write-in responses; one is always provided in the UI by default.\n" +
			"- Don't enumerate the options; they are enumerated by default.\n" +
			"- Don't include \"Select all options that apply\", or similar, in the question; the UI already includes this.\n" +
			"- If you recommend any options, list it first and prefix the option text with \"(Recommended)\".\n" +
			"- Format options as the user's direct response instead of describing your own actions.\n" +
			"- Set 'IsMultiSelect' to true to allow the user to select multiple options with checkboxes.",
		Parameters: map[string]interface{}{
			"type": "object",
			"properties": map[string]interface{}{
				"Questions": map[string]interface{}{
					"type":        "array",
					"description": "The list of questions to ask.",
					"items": map[string]interface{}{
						"type": "object",
						"properties": map[string]interface{}{
							"Question": map[string]interface{}{
								"type":        "string",
								"description": "The question to ask the user. Do NOT add 'select all that apply' or similar text to the question title.",
							},
							"Options": map[string]interface{}{
								"type":        "array",
								"description": "The text for each option, formatted as the user's response. Must have at least 2 options. Do NOT add an 'Other' option to questions.",
								"items":       map[string]interface{}{"type": "string"},
							},
							"IsMultiSelect": map[string]interface{}{
								"type":        "boolean",
								"description": "If true, the user can select multiple options.",
							},
						},
						"required": []string{"Question", "Options"},
					},
				},
				"ToolAction": map[string]interface{}{
					"type":        "string",
					"description": "Brief 2-5 word phrase in -ing form describing the specific action. Capitalize like a sentence. Some examples: 'Analyzing directory', 'Searching the web', 'Checking git status', 'Running tests', 'Searching code'.",
				},
				"ToolSummary": map[string]interface{}{
					"type":        "string",
					"description": "Brief 2-5 word noun phrase describing the specific task. Capitalize like a sentence. Some examples: 'Directory analysis', 'Web search', 'Git status check', 'Test execution', 'Code search'.",
				},
			},
			"required": []string{
				"ToolSummary",
				"ToolAction",
			},
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
