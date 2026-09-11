package tools

// registerDesktopTools registers schema-only entries for desktop computer use tools.
// These tools are engine-intercepted — the engine handles execution directly via
// native OS drivers (screencapture/AppleScript on macOS, maim/xdotool on Linux, PowerShell/Win32 on Windows).
func registerDesktopTools(r *Registry) {
	r.RegisterSchemaOnly("desktop_screenshot", ToolSchema{
		Group:       ToolGroupRead,
		Name:        "desktop_screenshot",
		Description: "Capture a screenshot of the current desktop screen or a specific application window. Saves to the brain artifacts directory.",
		Parameters: map[string]interface{}{
			"type": "object",
			"properties": map[string]interface{}{
				"target_app": map[string]interface{}{
					"type":        "string",
					"description": "Optional name of the application to focus and screenshot (e.g. 'Slack', 'Terminal', 'VS Code').",
				},
				"output_path": map[string]interface{}{
					"type":        "string",
					"description": "Optional absolute path to save the screenshot. Defaults to a timestamped PNG in scratch directory.",
				},
			},
		},
	})

	r.RegisterSchemaOnly("desktop_list_windows", ToolSchema{
		Group:       ToolGroupRead,
		Name:        "desktop_list_windows",
		Description: "List all visible windows and running desktop GUI applications.",
		Parameters: map[string]interface{}{
			"type":       "object",
			"properties": map[string]interface{}{},
		},
	})

	r.RegisterSchemaOnly("desktop_focus_window", ToolSchema{
		Group:       ToolGroupWrite,
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
	})

	r.RegisterSchemaOnly("desktop_click", ToolSchema{
		Group:       ToolGroupWrite,
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
					"enum":        []interface{}{"left", "right", "middle"},
					"description": "Mouse button to click (default: 'left').",
				},
				"double_click": map[string]interface{}{
					"type":        "boolean",
					"description": "Whether to perform a double click (default: false).",
				},
			},
		},
	})

	r.RegisterSchemaOnly("desktop_type", ToolSchema{
		Group:       ToolGroupWrite,
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
	})

	r.RegisterSchemaOnly("desktop_shortcut", ToolSchema{
		Group:       ToolGroupWrite,
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
	})
}
