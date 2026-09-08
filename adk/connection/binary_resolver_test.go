package connection

import (
	"os"
	"path/filepath"
	"runtime"
	"testing"
)

func TestBinaryResolver_ExplicitPath(t *testing.T) {
	// Create a temp "binary"
	tmpDir := t.TempDir()
	fakeBin := filepath.Join(tmpDir, "localharness")
	if err := os.WriteFile(fakeBin, []byte("#!/bin/sh\n"), 0755); err != nil {
		t.Fatal(err)
	}

	r := &BinaryResolver{}
	path, err := r.Resolve(fakeBin)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if path == "" {
		t.Fatal("expected non-empty path")
	}
}

func TestBinaryResolver_ExplicitPath_NotFound(t *testing.T) {
	r := &BinaryResolver{}
	_, err := r.Resolve("/nonexistent/path/to/localharness")
	if err == nil {
		t.Fatal("expected error for non-existent explicit path")
	}
}

func TestBinaryResolver_EnvVar(t *testing.T) {
	// Create a temp "binary"
	tmpDir := t.TempDir()
	fakeBin := filepath.Join(tmpDir, "localharness")
	if err := os.WriteFile(fakeBin, []byte("#!/bin/sh\n"), 0755); err != nil {
		t.Fatal(err)
	}

	t.Setenv("LOCALHARNESS_BIN", fakeBin)

	r := &BinaryResolver{}
	path, err := r.Resolve("")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if path == "" {
		t.Fatal("expected non-empty path from env var")
	}
}

func TestBinaryResolver_EnvVar_Invalid(t *testing.T) {
	t.Setenv("LOCALHARNESS_BIN", "/does/not/exist/localharness")

	r := &BinaryResolver{}
	_, err := r.Resolve("")
	if err == nil {
		t.Fatal("expected error for invalid $LOCALHARNESS_BIN")
	}
}

func TestBinaryResolver_CacheHit(t *testing.T) {
	// Create cache directory with a fake binary
	tmpDir := t.TempDir()
	versionDir := filepath.Join(tmpDir, "v1.2.3")
	if err := os.MkdirAll(versionDir, 0755); err != nil {
		t.Fatal(err)
	}
	cachedBin := filepath.Join(versionDir, "localharness")
	if err := os.WriteFile(cachedBin, []byte("#!/bin/sh\n"), 0755); err != nil {
		t.Fatal(err)
	}

	// Clear env to prevent env var resolution
	t.Setenv("LOCALHARNESS_BIN", "")

	r := &BinaryResolver{
		Version:  "1.2.3",
		CacheDir: tmpDir,
	}

	// We need PATH to NOT have localharness
	t.Setenv("PATH", t.TempDir())

	path, err := r.Resolve("")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if path != cachedBin {
		t.Errorf("expected cache path %s, got %s", cachedBin, path)
	}
}

func TestBinaryResolver_LocalDevPath(t *testing.T) {
	// Simulate the `make build` workflow: binary at ./bin/localharness
	tmpDir := t.TempDir()
	binDir := filepath.Join(tmpDir, "bin")
	if err := os.MkdirAll(binDir, 0755); err != nil {
		t.Fatal(err)
	}
	fakeBin := filepath.Join(binDir, "localharness")
	if err := os.WriteFile(fakeBin, []byte("#!/bin/sh\n"), 0755); err != nil {
		t.Fatal(err)
	}

	// Clear env and PATH
	t.Setenv("LOCALHARNESS_BIN", "")
	t.Setenv("PATH", t.TempDir())

	// Change working directory to tmpDir so ./bin/localharness is found
	origDir, _ := os.Getwd()
	if err := os.Chdir(tmpDir); err != nil {
		t.Fatal(err)
	}
	defer os.Chdir(origDir)

	r := &BinaryResolver{Version: "0.0.0-dev"}
	path, err := r.Resolve("")
	if err != nil {
		t.Fatalf("expected local dev path to resolve, got error: %v", err)
	}
	if !contains(path, "localharness") {
		t.Errorf("expected path to contain 'localharness', got: %s", path)
	}
}

func TestBinaryResolver_DevVersion_NoDownload(t *testing.T) {
	// With 0.0.0-dev version and no binary anywhere, should fail with help message
	t.Setenv("LOCALHARNESS_BIN", "")
	t.Setenv("PATH", t.TempDir()) // Empty PATH

	// Use a temp dir as cwd so no ./bin/localharness exists
	origDir, _ := os.Getwd()
	if err := os.Chdir(t.TempDir()); err != nil {
		t.Fatal(err)
	}
	defer os.Chdir(origDir)

	r := &BinaryResolver{
		Version: "0.0.0-dev",
	}

	_, err := r.Resolve("")
	if err == nil {
		t.Fatal("expected error for dev version with no binary")
	}
	// Should contain both dev and production install instructions
	errMsg := err.Error()
	if !contains(errMsg, "make build") {
		t.Errorf("error should mention 'make build' for dev, got: %s", errMsg)
	}
	if !contains(errMsg, "go install") {
		t.Errorf("error should contain install instructions, got: %s", errMsg)
	}
}

func TestPlatformSuffix(t *testing.T) {
	suffix, err := platformSuffix()
	if err != nil {
		// Only fail if we're on a supported platform
		if runtime.GOOS == "linux" || runtime.GOOS == "darwin" {
			t.Fatalf("expected valid suffix on %s/%s: %v", runtime.GOOS, runtime.GOARCH, err)
		}
		t.Skipf("unsupported platform: %s/%s", runtime.GOOS, runtime.GOARCH)
	}

	expected := runtime.GOOS + "-" + runtime.GOARCH
	if suffix != expected {
		t.Errorf("expected %s, got %s", expected, suffix)
	}
}

func TestIsExecutable(t *testing.T) {
	tmpDir := t.TempDir()

	// Executable file
	execFile := filepath.Join(tmpDir, "exec")
	os.WriteFile(execFile, []byte("test"), 0755)
	if !isExecutable(execFile) {
		t.Error("expected file to be executable")
	}

	// Non-executable file
	noExecFile := filepath.Join(tmpDir, "noexec")
	os.WriteFile(noExecFile, []byte("test"), 0644)
	if isExecutable(noExecFile) {
		t.Error("expected file to not be executable")
	}

	// Non-existent
	if isExecutable(filepath.Join(tmpDir, "nonexistent")) {
		t.Error("expected non-existent to not be executable")
	}

	// Directory
	if isExecutable(tmpDir) {
		t.Error("expected directory to not be executable")
	}
}

func contains(s, substr string) bool {
	return len(s) >= len(substr) && findSubstring(s, substr)
}

func findSubstring(s, substr string) bool {
	for i := 0; i <= len(s)-len(substr); i++ {
		if s[i:i+len(substr)] == substr {
			return true
		}
	}
	return false
}
