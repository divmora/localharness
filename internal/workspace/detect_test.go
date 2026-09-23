package workspace

import (
	"os"
	"path/filepath"
	"testing"
)

func TestHasWebIndicators(t *testing.T) {
	t.Run("empty dirs", func(t *testing.T) {
		if HasWebIndicators(nil) {
			t.Errorf("expected false for nil dirs")
		}
		if HasWebIndicators([]string{}) {
			t.Errorf("expected false for empty dirs")
		}
		if HasWebIndicators([]string{""}) {
			t.Errorf("expected false for empty string dir")
		}
	})

	t.Run("non-existent dir", func(t *testing.T) {
		if HasWebIndicators([]string{"/non/existent/path/for/sure"}) {
			t.Errorf("expected false for non-existent dir")
		}
	})

	t.Run("backend-only go repo", func(t *testing.T) {
		dir := t.TempDir()
		if err := os.WriteFile(filepath.Join(dir, "main.go"), []byte("package main"), 0644); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(dir, "go.mod"), []byte("module test"), 0644); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(dir, "Makefile"), []byte("all:"), 0644); err != nil {
			t.Fatal(err)
		}

		if HasWebIndicators([]string{dir}) {
			t.Errorf("expected false for backend-only Go directory")
		}
	})

	t.Run("package.json in root", func(t *testing.T) {
		dir := t.TempDir()
		if err := os.WriteFile(filepath.Join(dir, "package.json"), []byte("{}"), 0644); err != nil {
			t.Fatal(err)
		}

		if !HasWebIndicators([]string{dir}) {
			t.Errorf("expected true when package.json is present")
		}
	})

	t.Run("index.html in root", func(t *testing.T) {
		dir := t.TempDir()
		if err := os.WriteFile(filepath.Join(dir, "index.html"), []byte("<!DOCTYPE html>"), 0644); err != nil {
			t.Fatal(err)
		}

		if !HasWebIndicators([]string{dir}) {
			t.Errorf("expected true when index.html is present")
		}
	})

	t.Run("vite.config.ts in root", func(t *testing.T) {
		dir := t.TempDir()
		if err := os.WriteFile(filepath.Join(dir, "vite.config.ts"), []byte("export default {}"), 0644); err != nil {
			t.Fatal(err)
		}

		if !HasWebIndicators([]string{dir}) {
			t.Errorf("expected true when vite.config.ts is present")
		}
	})

	t.Run("public/index.html subpath", func(t *testing.T) {
		dir := t.TempDir()
		pubDir := filepath.Join(dir, "public")
		if err := os.MkdirAll(pubDir, 0755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(pubDir, "index.html"), []byte("<!DOCTYPE html>"), 0644); err != nil {
			t.Fatal(err)
		}

		if !HasWebIndicators([]string{dir}) {
			t.Errorf("expected true when public/index.html is present")
		}
	})

	t.Run("multiple dirs with one match", func(t *testing.T) {
		dirBackend := t.TempDir()
		if err := os.WriteFile(filepath.Join(dirBackend, "main.go"), []byte("package main"), 0644); err != nil {
			t.Fatal(err)
		}

		dirFrontend := t.TempDir()
		if err := os.WriteFile(filepath.Join(dirFrontend, "package.json"), []byte("{}"), 0644); err != nil {
			t.Fatal(err)
		}

		if !HasWebIndicators([]string{dirBackend, dirFrontend}) {
			t.Errorf("expected true when at least one workspace has web indicators")
		}
	})
}
