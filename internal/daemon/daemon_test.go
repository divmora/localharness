package daemon

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestDaemonInfoSaveAndLoad(t *testing.T) {
	tmpDir := t.TempDir()
	origHome := os.Getenv("HOME")
	os.Setenv("HOME", tmpDir)
	defer os.Setenv("HOME", origHome)

	info := &Info{
		PID:       os.Getpid(),
		Port:      8080,
		Socket:    filepath.Join(tmpDir, "harness.sock"),
		APIKey:    "test-api-key-123",
		StartedAt: time.Now().Truncate(time.Second),
		Version:   "0.0.0-dev",
	}

	if err := SaveDaemonInfo(info); err != nil {
		t.Fatalf("SaveDaemonInfo failed: %v", err)
	}

	loaded, err := LoadDaemonInfo()
	if err != nil {
		t.Fatalf("LoadDaemonInfo failed: %v", err)
	}

	if loaded.PID != info.PID || loaded.Port != info.Port || loaded.APIKey != info.APIKey {
		t.Errorf("loaded daemon info mismatch: got %+v, want %+v", loaded, info)
	}

	running, activeInfo, err := IsDaemonRunning()
	if err != nil {
		t.Fatalf("IsDaemonRunning failed: %v", err)
	}
	if !running {
		t.Error("expected daemon to be reported as running since PID is current test process")
	}
	if activeInfo == nil || activeInfo.PID != info.PID {
		t.Errorf("activeInfo mismatch: got %+v", activeInfo)
	}

	if err := RemoveDaemonInfo(); err != nil {
		t.Fatalf("RemoveDaemonInfo failed: %v", err)
	}

	running, _, err = IsDaemonRunning()
	if err != nil {
		t.Fatalf("IsDaemonRunning after remove failed: %v", err)
	}
	if running {
		t.Error("expected daemon not to be running after RemoveDaemonInfo")
	}
}
