//go:build windows

package desktop

import (
	"context"
	"fmt"
	"os/exec"
	"strings"
)

type windowsDriver struct{}

func newPlatformDriver() Driver {
	return &windowsDriver{}
}

func runPowerShell(ctx context.Context, script string) (string, error) {
	cmd := exec.CommandContext(ctx, "powershell.exe", "-NoProfile", "-NonInteractive", "-ExecutionPolicy", "Bypass", "-Command", script)
	out, err := cmd.CombinedOutput()
	if err != nil {
		return "", fmt.Errorf("powershell failed: %w (output: %s)", err, string(out))
	}
	return strings.TrimSpace(string(out)), nil
}

func (d *windowsDriver) CaptureScreen(ctx context.Context, targetApp string, outputPath string) (string, error) {
	if targetApp != "" {
		_ = d.FocusWindow(ctx, targetApp)
	}

	escapedPath := strings.ReplaceAll(outputPath, `"`, `\"`)
	script := fmt.Sprintf(`
Add-Type -AssemblyName System.Windows.Forms,System.Drawing
$screen = [System.Windows.Forms.Screen]::PrimaryScreen
$bitmap = New-Object System.Drawing.Bitmap $screen.Bounds.Width, $screen.Bounds.Height
$graphics = [System.Drawing.Graphics]::FromImage($bitmap)
$graphics.CopyFromScreen($screen.Bounds.X, $screen.Bounds.Y, 0, 0, $bitmap.Size)
$bitmap.Save("%s", [System.Drawing.Imaging.ImageFormat]::Png)
$graphics.Dispose()
$bitmap.Dispose()
`, escapedPath)

	if _, err := runPowerShell(ctx, script); err != nil {
		return "", fmt.Errorf("windows capture screen failed: %w", err)
	}
	return outputPath, nil
}

func (d *windowsDriver) ListWindows(ctx context.Context) ([]WindowInfo, error) {
	script := `Get-Process | Where-Object { $_.MainWindowTitle -ne '' } | ForEach-Object { $_.Id.ToString() + '|||' + $_.ProcessName + '|||' + $_.MainWindowTitle }`
	out, err := runPowerShell(ctx, script)
	if err != nil {
		return nil, fmt.Errorf("list windows failed: %w", err)
	}

	lines := strings.Split(out, "\n")
	var list []WindowInfo
	for _, line := range lines {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		parts := strings.Split(line, "|||")
		if len(parts) >= 3 {
			list = append(list, WindowInfo{
				ID:    parts[0],
				App:   parts[1],
				Title: parts[2],
			})
		}
	}
	return list, nil
}

func (d *windowsDriver) FocusWindow(ctx context.Context, appOrTitle string) error {
	escaped := strings.ReplaceAll(appOrTitle, `"`, `\"`)
	script := fmt.Sprintf(`
$p = Get-Process | Where-Object { $_.ProcessName -like "*%s*" -or $_.MainWindowTitle -like "*%s*" } | Select-Object -First 1
if ($p) {
    (New-Object -ComObject WScript.Shell).AppActivate($p.Id)
} else {
    throw "Process or window matching '%s' not found"
}
`, escaped, escaped, escaped)

	if _, err := runPowerShell(ctx, script); err != nil {
		return fmt.Errorf("focus window %q failed: %w", appOrTitle, err)
	}
	return nil
}

func (d *windowsDriver) Click(ctx context.Context, x, y int, button string, doubleClick bool) error {
	downFlag := "0x02" // LEFTDOWN
	upFlag := "0x04"   // LEFTUP
	if button == "right" {
		downFlag = "0x08" // RIGHTDOWN
		upFlag = "0x10"   // RIGHTUP
	}

	clicks := 1
	if doubleClick {
		clicks = 2
	}

	script := fmt.Sprintf(`
Add-Type -AssemblyName System.Windows.Forms,System.Drawing
$sig = @'
[DllImport("user32.dll")]
public static extern void mouse_event(uint dwFlags, uint dx, uint dy, uint cButtons, uint dwExtraInfo);
'@
$m = Add-Type -MemberDefinition $sig -Name "Win32Mouse" -Namespace "Win32" -PassThru
[System.Windows.Forms.Cursor]::Position = New-Object System.Drawing.Point(%d, %d)
for ($i = 0; $i -lt %d; $i++) {
    $m::mouse_event(%s, 0, 0, 0, 0)
    $m::mouse_event(%s, 0, 0, 0, 0)
    Start-Sleep -Milliseconds 50
}
`, x, y, clicks, downFlag, upFlag)

	if _, err := runPowerShell(ctx, script); err != nil {
		return fmt.Errorf("click at (%d,%d) failed: %w", x, y, err)
	}
	return nil
}

func (d *windowsDriver) Type(ctx context.Context, text string, enter bool) error {
	escaped := strings.ReplaceAll(text, `"`, `\"`)
	escaped = strings.ReplaceAll(escaped, "{", "{{}")
	escaped = strings.ReplaceAll(escaped, "}", "{}}")
	script := fmt.Sprintf(`
Add-Type -AssemblyName System.Windows.Forms
[System.Windows.Forms.SendKeys]::SendWait("%s")
`, escaped)
	if enter {
		script += `[System.Windows.Forms.SendKeys]::SendWait("{ENTER}")` + "\n"
	}

	if _, err := runPowerShell(ctx, script); err != nil {
		return fmt.Errorf("type failed: %w", err)
	}
	return nil
}

func (d *windowsDriver) KeyShortcut(ctx context.Context, keys ...string) error {
	if len(keys) == 0 {
		return nil
	}

	var combo strings.Builder
	for _, k := range keys {
		lower := strings.ToLower(k)
		switch lower {
		case "ctrl", "control":
			combo.WriteString("^")
		case "alt":
			combo.WriteString("%")
		case "shift":
			combo.WriteString("+")
		case "enter", "return":
			combo.WriteString("{ENTER}")
		case "tab":
			combo.WriteString("{TAB}")
		case "esc", "escape":
			combo.WriteString("{ESC}")
		default:
			combo.WriteString(k)
		}
	}

	script := fmt.Sprintf(`
Add-Type -AssemblyName System.Windows.Forms
[System.Windows.Forms.SendKeys]::SendWait("%s")
`, combo.String())

	if _, err := runPowerShell(ctx, script); err != nil {
		return fmt.Errorf("shortcut %v failed: %w", keys, err)
	}
	return nil
}
