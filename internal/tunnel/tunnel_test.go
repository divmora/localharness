package tunnel

import (
	"os"
	"testing"
	"time"
)

func TestTunnelURLRegex(t *testing.T) {
	testCases := []struct {
		input    string
		expected string
	}{
		{
			input:    "2026-09-28T14:00:00Z INF |  https://quiet-river-lake.trycloudflare.com",
			expected: "https://quiet-river-lake.trycloudflare.com",
		},
		{
			input:    "Visit your quick Tunnel at: https://random-123-abc-xyz.trycloudflare.com/",
			expected: "https://random-123-abc-xyz.trycloudflare.com",
		},
		{
			input:    "No url here",
			expected: "",
		},
	}

	for _, tc := range testCases {
		match := tunnelURLRegex.FindString(tc.input)
		if match != tc.expected {
			t.Errorf("FindString(%q) = %q; want %q", tc.input, match, tc.expected)
		}
	}
}

func TestTunnelInfoSaveAndLoad(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "tunnel-test-*")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(tmpDir)

	origHome := os.Getenv("HOME")
	defer os.Setenv("HOME", origHome)
	os.Setenv("HOME", tmpDir)

	info := &Info{
		URL:              "https://cool-river.trycloudflare.com",
		ControlURL:       "https://cool-river.trycloudflare.com/?key=12345#session-abc",
		PID:              os.Getpid(),
		StartedAt:        time.Now().Truncate(time.Second),
		LocalPort:        8080,
		InitialSessionID: "session-abc",
	}

	if err := SaveTunnelInfo(info); err != nil {
		t.Fatalf("SaveTunnelInfo failed: %v", err)
	}

	loaded, err := LoadTunnelInfo()
	if err != nil {
		t.Fatalf("LoadTunnelInfo failed: %v", err)
	}

	if loaded.URL != info.URL {
		t.Errorf("URL = %q; want %q", loaded.URL, info.URL)
	}
	if loaded.ControlURL != info.ControlURL {
		t.Errorf("ControlURL = %q; want %q", loaded.ControlURL, info.ControlURL)
	}
	if loaded.PID != info.PID {
		t.Errorf("PID = %d; want %d", loaded.PID, info.PID)
	}
	if loaded.LocalPort != info.LocalPort {
		t.Errorf("LocalPort = %d; want %d", loaded.LocalPort, info.LocalPort)
	}
	if loaded.InitialSessionID != info.InitialSessionID {
		t.Errorf("InitialSessionID = %q; want %q", loaded.InitialSessionID, info.InitialSessionID)
	}

	if err := RemoveTunnelInfo(); err != nil {
		t.Fatalf("RemoveTunnelInfo failed: %v", err)
	}

	if _, err := LoadTunnelInfo(); err == nil {
		t.Errorf("expected error after RemoveTunnelInfo, got nil")
	}
}

func TestGenerateTerminalQRCode(t *testing.T) {
	url := "https://quiet-river-lake.trycloudflare.com/?key=secret123"
	qr, err := GenerateTerminalQRCode(url)
	if err != nil {
		t.Fatalf("GenerateTerminalQRCode failed: %v", err)
	}
	if len(qr) == 0 {
		t.Fatal("GenerateTerminalQRCode returned empty string")
	}
}

func TestReleaseAssetInfo(t *testing.T) {
	asset, _, err := releaseAssetInfo()
	if err != nil {
		t.Fatalf("releaseAssetInfo failed for current OS: %v", err)
	}
	if len(asset) == 0 {
		t.Fatal("releaseAssetInfo returned empty string")
	}
}
