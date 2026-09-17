package tools

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"sort"

	pb "github.com/divmora/localharness/gen/go/localharness/v1"
)

func registerListDir(r *Registry) {
	r.Register("list_dir", executeListDir, ToolSchema{
		Group: ToolGroupRead,
		Name:  "list_dir",
		Description: "List the contents of a directory, including all files and subdirectories. " +
			"Use this instead of run_command with ls, dir, or find (for directory listing). " +
			"Directory path must be an absolute path to a directory that exists. " +
			"For each child: relative path, whether it is a directory or file, size in bytes if file, " +
			"and number of children if directory.",
		Parameters: map[string]interface{}{
			"type": "object",
			"properties": map[string]interface{}{
				"path": map[string]interface{}{"type": "string", "description": "Absolute path to the directory to list"},
			},
			"required": []string{"path"},
		},
	})
}

func executeListDir(ctx context.Context, step *pb.StepUpdate, r *Registry) error {
	ld := step.GetListDir()
	if ld == nil {
		return fmt.Errorf("list_dir: missing action")
	}

	dirPath := ld.Path
	if dirPath == "" {
		return fmt.Errorf("list_dir: path is required")
	}

	// Workspace validation
	validPath, err := r.ValidatePath(dirPath)
	if err != nil {
		return fmt.Errorf("list_dir: %w", err)
	}
	dirPath = validPath
	ld.Path = dirPath

	// Verify it's a directory
	info, err := os.Stat(dirPath)
	if err != nil {
		return fmt.Errorf("list_dir: %w", err)
	}
	if !info.IsDir() {
		return fmt.Errorf("list_dir: %s is not a directory", dirPath)
	}

	// Read directory entries
	dirEntries, err := os.ReadDir(dirPath)
	if err != nil {
		return fmt.Errorf("list_dir: %w", err)
	}

	// Sort: directories first, then files, alphabetically within each group
	sort.Slice(dirEntries, func(i, j int) bool {
		iDir := dirEntries[i].IsDir()
		jDir := dirEntries[j].IsDir()
		if iDir != jDir {
			return iDir // dirs first
		}
		return dirEntries[i].Name() < dirEntries[j].Name()
	})

	// Cap entries to prevent context blowup on large directories
	const maxDirEntries = 500
	if len(dirEntries) > maxDirEntries {
		dirEntries = dirEntries[:maxDirEntries]
	}

	var entries []*pb.DirEntry
	for _, entry := range dirEntries {
		de := &pb.DirEntry{
			Name:  entry.Name(),
			IsDir: entry.IsDir(),
		}

		if entry.IsDir() {
			if !listDirSkipNames[entry.Name()] {
				de.ChildCount = int32(shallowChildCount(filepath.Join(dirPath, entry.Name())))
			}
		} else {
			if fi, err := entry.Info(); err == nil {
				de.SizeBytes = fi.Size()
			}
		}

		entries = append(entries, de)
	}

	ld.Entries = entries
	return nil
}

// listDirSkipNames defines directory names that should strictly skip child counting
// to prevent synchronous disk storms on large dependency and VCS directories.
var listDirSkipNames = map[string]bool{
	".git":         true,
	"node_modules": true,
	"vendor":       true,
}

// shallowChildCount counts immediate items in a directory without recursion.
func shallowChildCount(dir string) int {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return 0
	}
	return len(entries)
}
