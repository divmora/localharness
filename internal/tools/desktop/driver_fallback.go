//go:build !darwin && !linux && !windows

package desktop

import (
	"context"
	"fmt"
	"runtime"
)

type fallbackDriver struct{}

func newPlatformDriver() Driver {
	return &fallbackDriver{}
}

func (d *fallbackDriver) CaptureScreen(ctx context.Context, targetApp string, outputPath string) (string, error) {
	return "", fmt.Errorf("desktop screen capture is not supported on %s", runtime.GOOS)
}

func (d *fallbackDriver) ListWindows(ctx context.Context) ([]WindowInfo, error) {
	return nil, fmt.Errorf("desktop window listing is not supported on %s", runtime.GOOS)
}

func (d *fallbackDriver) FocusWindow(ctx context.Context, appOrTitle string) error {
	return fmt.Errorf("desktop window focus is not supported on %s", runtime.GOOS)
}

func (d *fallbackDriver) Click(ctx context.Context, x, y int, button string, doubleClick bool) error {
	return fmt.Errorf("desktop mouse click is not supported on %s", runtime.GOOS)
}

func (d *fallbackDriver) Type(ctx context.Context, text string, enter bool) error {
	return fmt.Errorf("desktop keyboard typing is not supported on %s", runtime.GOOS)
}

func (d *fallbackDriver) KeyShortcut(ctx context.Context, keys ...string) error {
	return fmt.Errorf("desktop shortcuts are not supported on %s", runtime.GOOS)
}
