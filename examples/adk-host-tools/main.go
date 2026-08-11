// Example adk-host-tools demonstrates registering custom SDK-side tools
// that the LLM can call. The harness forwards tool calls to your handlers
// and feeds the results back to the LLM.
//
// Usage:
//
//	make build
//	go run ./examples/adk-host-tools
package main

import (
	"context"
	"fmt"
	"log"
	"strings"

	"github.com/divmora/localharness/adk"
	"github.com/divmora/localharness/adk/policy"
)

func main() {

	cfg := adk.NewLocalAgentConfig()
	cfg.Policies = []policy.Policy{policy.AllowAll()}
	cfg.SystemInstructions = "You are a helpful assistant with access to weather and unit conversion tools. Use them when asked."

	// Register custom host tools
	cfg.HostTools = []adk.HostToolDef{
		{
			Name:        "get_weather",
			Description: "Get the current weather for a city. Returns temperature in Celsius and condition.",
			Parameters: map[string]any{
				"type": "object",
				"properties": map[string]any{
					"city": map[string]any{
						"type":        "string",
						"description": "City name, e.g. 'Tokyo'",
					},
				},
				"required": []string{"city"},
			},
			Handler: func(ctx context.Context, args map[string]any) (any, error) {
				city, _ := args["city"].(string)
				fmt.Printf("  🌤️  get_weather called: city=%s\n", city)

				// Simulate weather lookup
				weather := map[string]any{
					"city":        city,
					"temperature": 22,
					"unit":        "celsius",
					"condition":   "Partly Cloudy",
					"humidity":    65,
				}
				return weather, nil
			},
		},
		{
			Name:        "convert_units",
			Description: "Convert a value from one unit to another. Supports temperature (celsius/fahrenheit) and distance (km/miles).",
			Parameters: map[string]any{
				"type": "object",
				"properties": map[string]any{
					"value": map[string]any{
						"type":        "number",
						"description": "The numeric value to convert",
					},
					"from_unit": map[string]any{
						"type":        "string",
						"description": "Source unit (celsius, fahrenheit, km, miles)",
					},
					"to_unit": map[string]any{
						"type":        "string",
						"description": "Target unit (celsius, fahrenheit, km, miles)",
					},
				},
				"required": []string{"value", "from_unit", "to_unit"},
			},
			Handler: func(ctx context.Context, args map[string]any) (any, error) {
				value, _ := args["value"].(float64)
				from, _ := args["from_unit"].(string)
				to, _ := args["to_unit"].(string)
				fmt.Printf("  🔄  convert_units called: %.1f %s → %s\n", value, from, to)

				var result float64
				switch {
				case strings.EqualFold(from, "celsius") && strings.EqualFold(to, "fahrenheit"):
					result = value*9/5 + 32
				case strings.EqualFold(from, "fahrenheit") && strings.EqualFold(to, "celsius"):
					result = (value - 32) * 5 / 9
				case strings.EqualFold(from, "km") && strings.EqualFold(to, "miles"):
					result = value * 0.621371
				case strings.EqualFold(from, "miles") && strings.EqualFold(to, "km"):
					result = value / 0.621371
				default:
					return nil, fmt.Errorf("unsupported conversion: %s → %s", from, to)
				}

				return map[string]any{
					"value":     result,
					"from_unit": from,
					"to_unit":   to,
					"original":  value,
				}, nil
			},
		},
	}

	agent, err := adk.NewAgent(cfg)
	if err != nil {
		log.Fatalf("NewAgent: %v", err)
	}
	defer agent.Close()

	ctx := context.Background()
	if err := agent.Start(ctx); err != nil {
		log.Fatalf("Start: %v", err)
	}

	// Ask something that triggers both tools
	prompt := "What's the weather in Tokyo? Also convert the temperature to Fahrenheit."
	fmt.Printf("📝 Prompt: %s\n\n", prompt)

	events, err := agent.ChatStream(ctx, prompt)
	if err != nil {
		log.Fatalf("ChatStream: %v", err)
	}

	for event := range events {
		switch event.Type {
		case adk.EventTextDelta:
			fmt.Print(event.TextDelta)
		case adk.EventToolCallStart:
			fmt.Printf("\n🔧 Calling: %s\n", event.Step.ToolName)
		case adk.EventToolCallDone:
			fmt.Printf("   ✅ Done\n")
		case adk.EventError:
			fmt.Printf("   ❌ %s\n", event.Step.ErrorMessage)
		case adk.EventTurnComplete:
			fmt.Printf("\n\n✅ Turn complete — %d steps\n", len(event.Response.Steps))
		}
	}
}
