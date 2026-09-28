package tunnel

import (
	"fmt"

	"github.com/skip2/go-qrcode"
)

// GenerateTerminalQRCode returns an ANSI string representing a compact QR code
// suitable for rendering directly in terminal emulators.
func GenerateTerminalQRCode(content string) (string, error) {
	qr, err := qrcode.New(content, qrcode.Medium)
	if err != nil {
		return "", fmt.Errorf("generate qr: %w", err)
	}

	// qr.ToSmallString(false) uses unicode block characters for a compact terminal footprint
	return qr.ToSmallString(false), nil
}
