package desktop

import (
	"context"
)

// Rect represents a screen rectangle.
type Rect struct {
	X int `json:"x"`
	Y int `json:"y"`
	W int `json:"w"`
	H int `json:"h"`
}

// WindowInfo contains details about a desktop window / application.
type WindowInfo struct {
	ID       string `json:"id"`
	Title    string `json:"title"`
	App      string `json:"app"`
	Bounds   Rect   `json:"bounds,omitempty"`
	IsActive bool   `json:"is_active"`
}

// Driver defines the cross-platform desktop automation interface.
type Driver interface {
	CaptureScreen(ctx context.Context, targetApp string, outputPath string) (string, error)
	ListWindows(ctx context.Context) ([]WindowInfo, error)
	FocusWindow(ctx context.Context, appOrTitle string) error
	Click(ctx context.Context, x, y int, button string, doubleClick bool) error
	Type(ctx context.Context, text string, enter bool) error
	KeyShortcut(ctx context.Context, keys ...string) error
}

// NewDriver returns the OS-specific desktop automation driver.
func NewDriver() Driver {
	return newPlatformDriver()
}
