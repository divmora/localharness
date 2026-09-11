package tui

import (
	"encoding/base64"
	"fmt"
	"os"

	"github.com/atotto/clipboard"
)

// CopyToClipboard copies text to the system clipboard (pbcopy, xclip, wl-copy, Win32)
// and emits OSC 52 terminal escape sequences for remote SSH/tmux sessions.
func CopyToClipboard(text string) error {
	// 1. System clipboard via atotto/clipboard
	sysErr := clipboard.WriteAll(text)

	// 2. OSC 52 escape sequence writes directly to the user's terminal emulator clipboard.
	// This works over SSH, inside tmux, and across platforms without external tools.
	encoded := base64.StdEncoding.EncodeToString([]byte(text))
	if os.Getenv("TMUX") != "" {
		// Inside tmux, wrap in DCS pass-through escape sequence
		fmt.Fprintf(os.Stdout, "\x1bPtmux;\x1b\x1b]52;c;%s\x07\x1b\\", encoded)
	} else {
		fmt.Fprintf(os.Stdout, "\x1b]52;c;%s\x07", encoded)
	}

	return sysErr
}

// PasteFromClipboard reads text from the system clipboard.
func PasteFromClipboard() (string, error) {
	return clipboard.ReadAll()
}
