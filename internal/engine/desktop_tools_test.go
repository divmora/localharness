package engine

import (
	"context"
	"log/slog"
	"sync"
	"testing"

	pb "github.com/divmora/localharness/gen/go/localharness/v1"
	"github.com/divmora/localharness/internal/llm"
	"github.com/divmora/localharness/internal/tools"
	"github.com/divmora/localharness/internal/tools/desktop"
)

type mockDesktopDriver struct {
	desktop.Driver
	capturedApp    string
	capturedOutput string
	focusedApp     string
	clickedX       int
	clickedY       int
	clickedButton  string
	clickedDouble  bool
	typedText      string
	typedEnter     bool
	shortcutKeys   []string
}

func (m *mockDesktopDriver) CaptureScreen(ctx context.Context, targetApp string, outputPath string) (string, error) {
	m.capturedApp = targetApp
	m.capturedOutput = outputPath
	return outputPath, nil
}

func (m *mockDesktopDriver) ListWindows(ctx context.Context) ([]desktop.WindowInfo, error) {
	return []desktop.WindowInfo{
		{ID: "win-1", Title: "Code Editor", App: "VSCode", IsActive: true},
		{ID: "win-2", Title: "Terminal", App: "Ghostty", IsActive: false},
	}, nil
}

func (m *mockDesktopDriver) FocusWindow(ctx context.Context, appOrTitle string) error {
	m.focusedApp = appOrTitle
	return nil
}

func (m *mockDesktopDriver) Click(ctx context.Context, x, y int, button string, doubleClick bool) error {
	m.clickedX = x
	m.clickedY = y
	m.clickedButton = button
	m.clickedDouble = doubleClick
	return nil
}

func (m *mockDesktopDriver) Type(ctx context.Context, text string, enter bool) error {
	m.typedText = text
	m.typedEnter = enter
	return nil
}

func (m *mockDesktopDriver) KeyShortcut(ctx context.Context, keys ...string) error {
	m.shortcutKeys = keys
	return nil
}

func TestGetDesktopDriver_Concurrent(t *testing.T) {
	// Reset driver to nil state so we test concurrent lazy initialization under race detector
	resetDesktopDriverForTest()
	defer resetDesktopDriverForTest()

	const concurrency = 64
	drivers := make([]desktop.Driver, concurrency)
	var wg sync.WaitGroup
	wg.Add(concurrency)

	for i := 0; i < concurrency; i++ {
		go func(idx int) {
			defer wg.Done()
			drivers[idx] = getDesktopDriver()
		}(i)
	}
	wg.Wait()

	first := drivers[0]
	if first == nil {
		t.Fatal("expected non-nil desktop driver")
	}
	for i := 1; i < concurrency; i++ {
		if drivers[i] != first {
			t.Errorf("expected all goroutines to receive identical driver instance, got drivers[%d] != drivers[0]", i)
		}
	}
}

func TestSetDesktopDriverForTest(t *testing.T) {
	mock := &mockDesktopDriver{}
	cleanup := setDesktopDriverForTest(mock)
	defer cleanup()

	driver := getDesktopDriver()
	if driver != mock {
		t.Fatalf("expected mocked driver, got %v", driver)
	}

	wins, err := driver.ListWindows(context.Background())
	if err != nil || len(wins) != 2 || wins[0].App != "VSCode" {
		t.Errorf("unexpected windows: %v, err: %v", wins, err)
	}
}

func TestExecuteDesktopTools(t *testing.T) {
	mock := &mockDesktopDriver{}
	cleanup := setDesktopDriverForTest(mock)
	defer cleanup()

	logger := slog.Default()
	toolRegistry := tools.NewRegistry(nil, logger)
	eng := NewEngine(Config{
		ToolRegistry:     toolRegistry,
		Logger:           logger,
		HasDesktopConfig: true,
	})
	ctx := context.Background()

	// 1. desktop_screenshot
	{
		step := &pb.StepUpdate{}
		tc := llm.ToolCall{
			ID:   "call_ss",
			Name: "desktop_screenshot",
			Args: map[string]interface{}{
				"target_app":  "Slack",
				"output_path": "/tmp/test_slack.png",
			},
		}
		err := eng.executeDesktopScreenshot(ctx, tc, step)
		if err != nil {
			t.Fatalf("screenshot failed: %v", err)
		}
		if mock.capturedApp != "Slack" || mock.capturedOutput != "/tmp/test_slack.png" {
			t.Errorf("unexpected capture: app=%q, out=%q", mock.capturedApp, mock.capturedOutput)
		}
		if step.State != pb.StepUpdate_STATE_DONE {
			t.Errorf("expected state DONE, got %v", step.State)
		}
	}

	// 2. desktop_list_windows
	{
		step := &pb.StepUpdate{}
		tc := llm.ToolCall{
			ID:   "call_list",
			Name: "desktop_list_windows",
			Args: map[string]interface{}{},
		}
		err := eng.executeDesktopListWindows(ctx, tc, step)
		if err != nil {
			t.Fatalf("list windows failed: %v", err)
		}
		if step.State != pb.StepUpdate_STATE_DONE {
			t.Errorf("expected state DONE, got %v", step.State)
		}
	}

	// 3. desktop_focus_window
	{
		step := &pb.StepUpdate{}
		tc := llm.ToolCall{
			ID:   "call_focus",
			Name: "desktop_focus_window",
			Args: map[string]interface{}{
				"app_or_title": "VSCode",
			},
		}
		err := eng.executeDesktopFocusWindow(ctx, tc, step)
		if err != nil {
			t.Fatalf("focus window failed: %v", err)
		}
		if mock.focusedApp != "VSCode" {
			t.Errorf("expected focusedApp VSCode, got %q", mock.focusedApp)
		}
	}

	// 4. desktop_click
	{
		step := &pb.StepUpdate{}
		tc := llm.ToolCall{
			ID:   "call_click",
			Name: "desktop_click",
			Args: map[string]interface{}{
				"x":            float64(150),
				"y":            float64(300),
				"button":       "right",
				"double_click": true,
			},
		}
		err := eng.executeDesktopClick(ctx, tc, step)
		if err != nil {
			t.Fatalf("click failed: %v", err)
		}
		if mock.clickedX != 150 || mock.clickedY != 300 || mock.clickedButton != "right" || !mock.clickedDouble {
			t.Errorf("unexpected click args: x=%d, y=%d, button=%s, double=%v",
				mock.clickedX, mock.clickedY, mock.clickedButton, mock.clickedDouble)
		}
	}

	// 5. desktop_type
	{
		step := &pb.StepUpdate{}
		tc := llm.ToolCall{
			ID:   "call_type",
			Name: "desktop_type",
			Args: map[string]interface{}{
				"text":  "hello world",
				"enter": true,
			},
		}
		err := eng.executeDesktopType(ctx, tc, step)
		if err != nil {
			t.Fatalf("type failed: %v", err)
		}
		if mock.typedText != "hello world" || !mock.typedEnter {
			t.Errorf("unexpected type args: text=%q, enter=%v", mock.typedText, mock.typedEnter)
		}
	}

	// 6. desktop_shortcut
	{
		step := &pb.StepUpdate{}
		tc := llm.ToolCall{
			ID:   "call_shortcut",
			Name: "desktop_shortcut",
			Args: map[string]interface{}{
				"keys": []interface{}{"cmd", "shift", "p"},
			},
		}
		err := eng.executeDesktopShortcut(ctx, tc, step)
		if err != nil {
			t.Fatalf("shortcut failed: %v", err)
		}
		if len(mock.shortcutKeys) != 3 || mock.shortcutKeys[0] != "cmd" || mock.shortcutKeys[1] != "shift" || mock.shortcutKeys[2] != "p" {
			t.Errorf("unexpected shortcut keys: %v", mock.shortcutKeys)
		}
	}
}
