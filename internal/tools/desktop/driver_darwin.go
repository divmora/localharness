//go:build darwin

package desktop

import (
	"context"
	"fmt"
	"os/exec"
	"strings"
)

type darwinDriver struct{}

func newPlatformDriver() Driver {
	return &darwinDriver{}
}

func (d *darwinDriver) CaptureScreen(ctx context.Context, targetApp string, outputPath string) (string, error) {
	if targetApp != "" {
		_ = d.FocusWindow(ctx, targetApp)
	}
	cmd := exec.CommandContext(ctx, "/usr/sbin/screencapture", "-x", "-C", outputPath)
	out, err := cmd.CombinedOutput()
	if err != nil {
		return "", fmt.Errorf("screencapture failed: %w (output: %s)", err, string(out))
	}
	return outputPath, nil
}

func (d *darwinDriver) ListWindows(ctx context.Context) ([]WindowInfo, error) {
	script := `Application('System Events').applicationProcesses.where({backgroundOnly: false}).name()`
	cmd := exec.CommandContext(ctx, "osascript", "-l", "JavaScript", "-e", script)
	out, err := cmd.Output()
	if err != nil {
		return nil, fmt.Errorf("list windows failed: %w", err)
	}
	raw := strings.TrimSpace(string(out))
	if raw == "" {
		return nil, nil
	}
	names := strings.Split(raw, ", ")
	var list []WindowInfo
	for _, n := range names {
		n = strings.TrimSpace(n)
		if n != "" {
			list = append(list, WindowInfo{
				ID:    n,
				App:   n,
				Title: n,
			})
		}
	}
	return list, nil
}

func (d *darwinDriver) FocusWindow(ctx context.Context, appOrTitle string) error {
	script := fmt.Sprintf(`tell application "%s" to activate`, strings.ReplaceAll(appOrTitle, `"`, `\"`))
	cmd := exec.CommandContext(ctx, "osascript", "-e", script)
	out, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("focus window %q failed: %w (output: %s)", appOrTitle, err, string(out))
	}
	return nil
}

func (d *darwinDriver) Click(ctx context.Context, x, y int, button string, doubleClick bool) error {
	if clPath, lookErr := exec.LookPath("cliclick"); lookErr == nil {
		cmdStr := fmt.Sprintf("c:%d,%d", x, y)
		if doubleClick {
			cmdStr = fmt.Sprintf("dc:%d,%d", x, y)
		}
		if button == "right" {
			cmdStr = fmt.Sprintf("rc:%d,%d", x, y)
		}
		return exec.CommandContext(ctx, clPath, cmdStr).Run()
	}

	clickScript := fmt.Sprintf(`
tell application "System Events"
	click at {%d, %d}
end tell`, x, y)
	cmd := exec.CommandContext(ctx, "osascript", "-e", clickScript)
	if out, err := cmd.CombinedOutput(); err != nil {
		return fmt.Errorf("click at (%d,%d) failed: %w (output: %s)", x, y, err, string(out))
	}
	return nil
}

func (d *darwinDriver) Type(ctx context.Context, text string, enter bool) error {
	escaped := strings.ReplaceAll(text, `\`, `\\`)
	escaped = strings.ReplaceAll(escaped, `"`, `\"`)
	script := fmt.Sprintf(`tell application "System Events" to keystroke "%s"`, escaped)
	if enter {
		script += "\ntell application \"System Events\" to key code 36"
	}
	cmd := exec.CommandContext(ctx, "osascript", "-e", script)
	out, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("type failed: %w (output: %s)", err, string(out))
	}
	return nil
}

func (d *darwinDriver) KeyShortcut(ctx context.Context, keys ...string) error {
	if len(keys) == 0 {
		return nil
	}
	var modifier []string
	key := ""
	for _, k := range keys {
		lower := strings.ToLower(k)
		switch lower {
		case "cmd", "command":
			modifier = append(modifier, "command down")
		case "ctrl", "control":
			modifier = append(modifier, "control down")
		case "alt", "opt", "option":
			modifier = append(modifier, "option down")
		case "shift":
			modifier = append(modifier, "shift down")
		default:
			key = k
		}
	}
	var script string
	if len(modifier) > 0 {
		script = fmt.Sprintf(`tell application "System Events" to keystroke "%s" using {%s}`, key, strings.Join(modifier, ", "))
	} else {
		script = fmt.Sprintf(`tell application "System Events" to keystroke "%s"`, key)
	}
	cmd := exec.CommandContext(ctx, "osascript", "-e", script)
	out, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("shortcut %v failed: %w (output: %s)", keys, err, string(out))
	}
	return nil
}
