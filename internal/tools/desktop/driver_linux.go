//go:build linux

package desktop

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"strconv"
	"strings"
)

type linuxDriver struct{}

func newPlatformDriver() Driver {
	return &linuxDriver{}
}

func isWayland() bool {
	return os.Getenv("WAYLAND_DISPLAY") != "" || strings.Contains(os.Getenv("XDG_SESSION_TYPE"), "wayland")
}

func (d *linuxDriver) CaptureScreen(ctx context.Context, targetApp string, outputPath string) (string, error) {
	if targetApp != "" {
		_ = d.FocusWindow(ctx, targetApp)
	}

	if isWayland() {
		if grimPath, err := exec.LookPath("grim"); err == nil {
			cmd := exec.CommandContext(ctx, grimPath, outputPath)
			out, err := cmd.CombinedOutput()
			if err != nil {
				return "", fmt.Errorf("grim failed: %w (output: %s)", err, string(out))
			}
			return outputPath, nil
		}
	}

	// Try maim
	if maimPath, err := exec.LookPath("maim"); err == nil {
		cmd := exec.CommandContext(ctx, maimPath, outputPath)
		out, err := cmd.CombinedOutput()
		if err != nil {
			return "", fmt.Errorf("maim failed: %w (output: %s)", err, string(out))
		}
		return outputPath, nil
	}

	// Try scrot
	if scrotPath, err := exec.LookPath("scrot"); err == nil {
		cmd := exec.CommandContext(ctx, scrotPath, outputPath)
		out, err := cmd.CombinedOutput()
		if err != nil {
			return "", fmt.Errorf("scrot failed: %w (output: %s)", err, string(out))
		}
		return outputPath, nil
	}

	// Try ImageMagick import
	if importPath, err := exec.LookPath("import"); err == nil {
		cmd := exec.CommandContext(ctx, importPath, "-window", "root", outputPath)
		out, err := cmd.CombinedOutput()
		if err != nil {
			return "", fmt.Errorf("import failed: %w (output: %s)", err, string(out))
		}
		return outputPath, nil
	}

	return "", fmt.Errorf("no screenshot utility found; please install 'grim' (Wayland) or 'maim'/'scrot' (X11)")
}

func (d *linuxDriver) ListWindows(ctx context.Context) ([]WindowInfo, error) {
	if wmctrlPath, err := exec.LookPath("wmctrl"); err == nil {
		cmd := exec.CommandContext(ctx, wmctrlPath, "-l")
		out, err := cmd.Output()
		if err != nil {
			return nil, fmt.Errorf("wmctrl -l failed: %w", err)
		}
		lines := strings.Split(strings.TrimSpace(string(out)), "\n")
		var list []WindowInfo
		for _, line := range lines {
			parts := strings.Fields(line)
			if len(parts) >= 4 {
				title := strings.Join(parts[3:], " ")
				list = append(list, WindowInfo{
					ID:    parts[0],
					App:   parts[2],
					Title: title,
				})
			}
		}
		return list, nil
	}

	if xdotoolPath, err := exec.LookPath("xdotool"); err == nil {
		cmd := exec.CommandContext(ctx, xdotoolPath, "search", "--onlyvisible", "--name", "")
		out, err := cmd.Output()
		if err == nil {
			ids := strings.Fields(string(out))
			var list []WindowInfo
			for _, id := range ids {
				nameCmd := exec.CommandContext(ctx, xdotoolPath, "getwindowname", id)
				nameOut, _ := nameCmd.Output()
				title := strings.TrimSpace(string(nameOut))
				if title != "" {
					list = append(list, WindowInfo{
						ID:    id,
						Title: title,
						App:   title,
					})
				}
			}
			return list, nil
		}
	}

	return nil, fmt.Errorf("no window listing utility found; please install 'wmctrl' or 'xdotool'")
}

func (d *linuxDriver) FocusWindow(ctx context.Context, appOrTitle string) error {
	if wmctrlPath, err := exec.LookPath("wmctrl"); err == nil {
		cmd := exec.CommandContext(ctx, wmctrlPath, "-a", appOrTitle)
		if out, err := cmd.CombinedOutput(); err == nil {
			return nil
		} else {
			return fmt.Errorf("wmctrl -a %q failed: %w (output: %s)", appOrTitle, err, string(out))
		}
	}

	if xdotoolPath, err := exec.LookPath("xdotool"); err == nil {
		cmd := exec.CommandContext(ctx, xdotoolPath, "search", "--name", appOrTitle, "windowactivate")
		if out, err := cmd.CombinedOutput(); err == nil {
			return nil
		} else {
			return fmt.Errorf("xdotool windowactivate %q failed: %w (output: %s)", appOrTitle, err, string(out))
		}
	}

	return fmt.Errorf("no window management utility found; please install 'wmctrl' or 'xdotool'")
}

func (d *linuxDriver) Click(ctx context.Context, x, y int, button string, doubleClick bool) error {
	btnNum := "1"
	if button == "right" {
		btnNum = "3"
	} else if button == "middle" {
		btnNum = "2"
	}

	if xdotoolPath, err := exec.LookPath("xdotool"); err == nil {
		args := []string{"mousemove", strconv.Itoa(x), strconv.Itoa(y), "click"}
		if doubleClick {
			args = append(args, "--repeat", "2", "--delay", "100")
		}
		args = append(args, btnNum)
		cmd := exec.CommandContext(ctx, xdotoolPath, args...)
		if out, err := cmd.CombinedOutput(); err != nil {
			return fmt.Errorf("xdotool click failed: %w (output: %s)", err, string(out))
		}
		return nil
	}

	if ydotoolPath, err := exec.LookPath("ydotool"); err == nil {
		moveCmd := exec.CommandContext(ctx, ydotoolPath, "mousemove", "-a", strconv.Itoa(x), strconv.Itoa(y))
		_ = moveCmd.Run()
		clickBtn := "0xC0" // left click
		if button == "right" {
			clickBtn = "0xC1"
		}
		clickCmd := exec.CommandContext(ctx, ydotoolPath, "click", clickBtn)
		if doubleClick {
			_ = clickCmd.Run()
			clickCmd = exec.CommandContext(ctx, ydotoolPath, "click", clickBtn)
		}
		if out, err := clickCmd.CombinedOutput(); err != nil {
			return fmt.Errorf("ydotool click failed: %w (output: %s)", err, string(out))
		}
		return nil
	}

	return fmt.Errorf("no mouse control utility found; please install 'xdotool' (X11) or 'ydotool' (Wayland)")
}

func (d *linuxDriver) Type(ctx context.Context, text string, enter bool) error {
	if xdotoolPath, err := exec.LookPath("xdotool"); err == nil {
		cmd := exec.CommandContext(ctx, xdotoolPath, "type", "--delay", "12", "--", text)
		if out, err := cmd.CombinedOutput(); err != nil {
			return fmt.Errorf("xdotool type failed: %w (output: %s)", err, string(out))
		}
		if enter {
			_ = exec.CommandContext(ctx, xdotoolPath, "key", "Return").Run()
		}
		return nil
	}

	if ydotoolPath, err := exec.LookPath("ydotool"); err == nil {
		cmd := exec.CommandContext(ctx, ydotoolPath, "type", "--", text)
		if out, err := cmd.CombinedOutput(); err != nil {
			return fmt.Errorf("ydotool type failed: %w (output: %s)", err, string(out))
		}
		if enter {
			_ = exec.CommandContext(ctx, ydotoolPath, "key", "28:1", "28:0").Run() // 28 is KEY_ENTER
		}
		return nil
	}

	return fmt.Errorf("no keyboard utility found; please install 'xdotool' or 'ydotool'")
}

func (d *linuxDriver) KeyShortcut(ctx context.Context, keys ...string) error {
	if len(keys) == 0 {
		return nil
	}

	if xdotoolPath, err := exec.LookPath("xdotool"); err == nil {
		combo := strings.Join(keys, "+")
		cmd := exec.CommandContext(ctx, xdotoolPath, "key", combo)
		if out, err := cmd.CombinedOutput(); err != nil {
			return fmt.Errorf("xdotool shortcut %q failed: %w (output: %s)", combo, err, string(out))
		}
		return nil
	}

	if ydotoolPath, err := exec.LookPath("ydotool"); err == nil {
		combo := strings.Join(keys, "+")
		cmd := exec.CommandContext(ctx, ydotoolPath, "key", combo)
		if out, err := cmd.CombinedOutput(); err != nil {
			return fmt.Errorf("ydotool shortcut %q failed: %w (output: %s)", combo, err, string(out))
		}
		return nil
	}

	return fmt.Errorf("no shortcut utility found; please install 'xdotool' or 'ydotool'")
}
