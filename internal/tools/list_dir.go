package tools

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"sync"

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

	type dirToCount struct {
		index int
		path  string
	}
	var dirsToCount []dirToCount

	entries := make([]*pb.DirEntry, len(dirEntries))
	for i, entry := range dirEntries {
		de := &pb.DirEntry{
			Name:  entry.Name(),
			IsDir: entry.IsDir(),
		}

		if entry.IsDir() {
			if !listDirSkipNames[entry.Name()] {
				dirsToCount = append(dirsToCount, dirToCount{
					index: i,
					path:  filepath.Join(dirPath, entry.Name()),
				})
			}
		} else {
			if fi, err := entry.Info(); err == nil {
				de.SizeBytes = fi.Size()
			}
		}

		entries[i] = de
	}

	if len(dirsToCount) == 1 {
		entries[dirsToCount[0].index].ChildCount = int32(shallowChildCount(dirsToCount[0].path))
	} else if len(dirsToCount) > 1 {
		numWorkers := 8
		if numWorkers > len(dirsToCount) {
			numWorkers = len(dirsToCount)
		}

		workCh := make(chan dirToCount, len(dirsToCount))
		for _, d := range dirsToCount {
			workCh <- d
		}
		close(workCh)

		var wg sync.WaitGroup
		for w := 0; w < numWorkers; w++ {
			wg.Add(1)
			go func() {
				defer wg.Done()
				for d := range workCh {
					select {
					case <-ctx.Done():
						return
					default:
					}
					cnt := shallowChildCount(d.path)
					entries[d.index].ChildCount = int32(cnt)
				}
			}()
		}
		wg.Wait()
	}

	ld.Entries = entries
	return nil
}

// listDirSkipNames defines directory names that should strictly skip child counting
// to prevent synchronous disk storms on large dependency, build, cache, and VCS directories.
var listDirSkipNames = map[string]bool{
	".git":         true,
	"node_modules": true,
	"vendor":       true,
	".gemini":      true,
	".divmora":     true,
	"dist":         true,
	"build":        true,
	"target":       true,
	"bin":          true,
	"__pycache__":  true,
	".venv":        true,
	".agents":      true,
	".next":        true,
	".turbo":       true,
	".cache":       true,
}

// shallowChildCount counts immediate items in a directory without allocating full DirEntry structs or sorting.
func shallowChildCount(dir string) int {
	f, err := os.Open(dir)
	if err != nil {
		return 0
	}
	defer f.Close()

	count := 0
	for {
		names, err := f.Readdirnames(512)
		count += len(names)
		if err != nil || len(names) == 0 {
			break
		}
	}
	return count
}
