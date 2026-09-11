package engine

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	pb "github.com/divmora/localharness/gen/go/localharness/v1"
	"github.com/divmora/localharness/internal/llm"
	"github.com/divmora/localharness/internal/tools/desktop"
)

var globalDesktopDriver desktop.Driver

func getDesktopDriver() desktop.Driver {
	if globalDesktopDriver == nil {
		globalDesktopDriver = desktop.NewDriver()
	}
	return globalDesktopDriver
}

// desktopToolDeclarations returns the function declarations for desktop tools.
func desktopToolDeclarations() []llm.FunctionDeclaration {
	return []llm.FunctionDeclaration{
		{
			Name:        "desktop_screenshot",
			Description: "Capture a screenshot of the current screen or a specific application window. Saves to the brain artifacts directory.",
			Parameters: map[string]interface{}{
				"type": "object",
				"properties": map[string]interface{}{
					"target_app": map[string]interface{}{
						"type":        "string",
						"description": "Optional name of the application to focus and screenshot (e.g. 'Slack', 'Terminal').",
					},
					"output_path": map[string]interface{}{
						"type":        "string",
						"description": "Optional absolute path to save the screenshot. Defaults to a timestamped PNG in scratch directory.",
					},
				},
			},
		},
		{
			Name:        "desktop_list_windows",
			Description: "List all visible windows and running desktop GUI applications.",
			Parameters: map[string]interface{}{
				"type":       "object",
				"properties": map[string]interface{}{},
			},
		},
		{
			Name:        "desktop_focus_window",
			Description: "Bring a specific application or window to the foreground and focus it.",
			Parameters: map[string]interface{}{
				"type":     "object",
				"required": []string{"app_or_title"},
				"properties": map[string]interface{}{
					"app_or_title": map[string]interface{}{
						"type":        "string",
						"description": "Application name or window title to activate (e.g. 'Google Chrome', 'Slack', 'Terminal').",
					},
				},
			},
		},
		{
			Name:        "desktop_click",
			Description: "Click at specific screen pixel coordinates (x, y).",
			Parameters: map[string]interface{}{
				"type":     "object",
				"required": []string{"x", "y"},
				"properties": map[string]interface{}{
					"x": map[string]interface{}{
						"type":        "integer",
						"description": "Horizontal X coordinate in pixels.",
					},
					"y": map[string]interface{}{
						"type":        "integer",
						"description": "Vertical Y coordinate in pixels.",
					},
					"button": map[string]interface{}{
						"type":        "string",
						"enum":        []string{"left", "right", "middle"},
						"description": "Mouse button to click (default: 'left').",
					},
					"double_click": map[string]interface{}{
						"type":        "boolean",
						"description": "Whether to perform a double click (default: false).",
					},
				},
			},
		},
		{
			Name:        "desktop_type",
			Description: "Type text into the currently active desktop window.",
			Parameters: map[string]interface{}{
				"type":     "object",
				"required": []string{"text"},
				"properties": map[string]interface{}{
					"text": map[string]interface{}{
						"type":        "string",
						"description": "The text string to type.",
					},
					"enter": map[string]interface{}{
						"type":        "boolean",
						"description": "Whether to press Enter after typing the text (default: false).",
					},
				},
			},
		},
		{
			Name:        "desktop_shortcut",
			Description: "Trigger a keyboard shortcut or key combination in the active desktop window.",
			Parameters: map[string]interface{}{
				"type":     "object",
				"required": []string{"keys"},
				"properties": map[string]interface{}{
					"keys": map[string]interface{}{
						"type": "array",
						"items": map[string]interface{}{
							"type": "string",
						},
						"description": "List of keys in sequence/combination, e.g. ['cmd', 'c'], ['ctrl', 'v'], ['alt', 'tab'], ['enter'].",
					},
				},
			},
		},
	}
}

func (e *Engine) executeDesktopScreenshot(ctx context.Context, tc llm.ToolCall, step *pb.StepUpdate) error {
	driver := getDesktopDriver()
	targetApp, _ := tc.Args["target_app"].(string)
	outputPath, _ := tc.Args["output_path"].(string)

	if outputPath == "" {
		outDir := os.TempDir()
		if e.brainDir != "" {
			outDir = filepath.Join(e.brainDir, "scratch")
			_ = os.MkdirAll(outDir, 0755)
		}
		outputPath = filepath.Join(outDir, fmt.Sprintf("desktop_screenshot_%d.png", time.Now().UnixMilli()))
	}

	savedPath, err := driver.CaptureScreen(ctx, targetApp, outputPath)
	if err != nil {
		e.feedToolError(tc, step, fmt.Sprintf("failed to capture desktop screenshot: %v", err))
		return nil
	}

	resultText := fmt.Sprintf("Screenshot saved to: %s\n![Desktop Screenshot](%s)", savedPath, savedPath)
	step.Text = resultText
	step.State = pb.StepUpdate_STATE_DONE
	e.emitStep(step)
	e.history = append(e.history, toolResultMsg(tc, resultText, false))
	return nil
}

func (e *Engine) executeDesktopListWindows(ctx context.Context, tc llm.ToolCall, step *pb.StepUpdate) error {
	driver := getDesktopDriver()
	windows, err := driver.ListWindows(ctx)
	if err != nil {
		e.feedToolError(tc, step, fmt.Sprintf("failed to list desktop windows: %v", err))
		return nil
	}

	b, _ := json.MarshalIndent(windows, "", "  ")
	resultText := string(b)
	if len(windows) == 0 {
		resultText = "No visible desktop windows found."
	}

	step.Text = resultText
	step.State = pb.StepUpdate_STATE_DONE
	e.emitStep(step)
	e.history = append(e.history, toolResultMsg(tc, resultText, false))
	return nil
}

func (e *Engine) executeDesktopFocusWindow(ctx context.Context, tc llm.ToolCall, step *pb.StepUpdate) error {
	driver := getDesktopDriver()
	appOrTitle, _ := tc.Args["app_or_title"].(string)
	if appOrTitle == "" {
		e.feedToolError(tc, step, "app_or_title is required")
		return nil
	}

	if err := driver.FocusWindow(ctx, appOrTitle); err != nil {
		e.feedToolError(tc, step, fmt.Sprintf("failed to focus window %q: %v", appOrTitle, err))
		return nil
	}

	resultText := fmt.Sprintf("Successfully focused window/app: %s", appOrTitle)
	step.Text = resultText
	step.State = pb.StepUpdate_STATE_DONE
	e.emitStep(step)
	e.history = append(e.history, toolResultMsg(tc, resultText, false))
	return nil
}

func (e *Engine) executeDesktopClick(ctx context.Context, tc llm.ToolCall, step *pb.StepUpdate) error {
	driver := getDesktopDriver()
	xVal, okX := tc.Args["x"].(float64)
	yVal, okY := tc.Args["y"].(float64)
	if !okX || !okY {
		e.feedToolError(tc, step, "x and y coordinates are required")
		return nil
	}

	button, _ := tc.Args["button"].(string)
	if button == "" {
		button = "left"
	}
	doubleClick, _ := tc.Args["double_click"].(bool)

	if err := driver.Click(ctx, int(xVal), int(yVal), button, doubleClick); err != nil {
		e.feedToolError(tc, step, fmt.Sprintf("failed to click at (%d,%d): %v", int(xVal), int(yVal), err))
		return nil
	}

	clickType := "click"
	if doubleClick {
		clickType = "double click"
	}
	resultText := fmt.Sprintf("Performed %s (%s button) at (%d, %d)", clickType, button, int(xVal), int(yVal))
	step.Text = resultText
	step.State = pb.StepUpdate_STATE_DONE
	e.emitStep(step)
	e.history = append(e.history, toolResultMsg(tc, resultText, false))
	return nil
}

func (e *Engine) executeDesktopType(ctx context.Context, tc llm.ToolCall, step *pb.StepUpdate) error {
	driver := getDesktopDriver()
	text, _ := tc.Args["text"].(string)
	enter, _ := tc.Args["enter"].(bool)

	if err := driver.Type(ctx, text, enter); err != nil {
		e.feedToolError(tc, step, fmt.Sprintf("failed to type text: %v", err))
		return nil
	}

	resultText := fmt.Sprintf("Typed %d characters (enter=%v)", len(text), enter)
	step.Text = resultText
	step.State = pb.StepUpdate_STATE_DONE
	e.emitStep(step)
	e.history = append(e.history, toolResultMsg(tc, resultText, false))
	return nil
}

func parseShortcutKeys(raw interface{}) []string {
	var keys []string
	if rawKeys, ok := raw.([]interface{}); ok {
		for _, k := range rawKeys {
			if s, ok := k.(string); ok {
				keys = append(keys, s)
			}
		}
	} else if rawStr, ok := raw.(string); ok {
		rawStr = strings.TrimSpace(rawStr)
		if strings.HasPrefix(rawStr, "[") && strings.HasSuffix(rawStr, "]") {
			var parsed []string
			if err := json.Unmarshal([]byte(rawStr), &parsed); err == nil {
				keys = parsed
			}
		}
		if len(keys) == 0 {
			for _, part := range strings.Split(rawStr, "+") {
				part = strings.TrimSpace(part)
				if part != "" {
					keys = append(keys, part)
				}
			}
		}
	}
	return keys
}

func (e *Engine) executeDesktopShortcut(ctx context.Context, tc llm.ToolCall, step *pb.StepUpdate) error {
	driver := getDesktopDriver()
	keys := parseShortcutKeys(tc.Args["keys"])

	if len(keys) == 0 {
		e.feedToolError(tc, step, "keys array is required")
		return nil
	}

	if err := driver.KeyShortcut(ctx, keys...); err != nil {
		e.feedToolError(tc, step, fmt.Sprintf("failed to send shortcut %v: %v", keys, err))
		return nil
	}

	resultText := fmt.Sprintf("Triggered shortcut: %v", keys)
	step.Text = resultText
	step.State = pb.StepUpdate_STATE_DONE
	e.emitStep(step)
	e.history = append(e.history, toolResultMsg(tc, resultText, false))
	return nil
}
