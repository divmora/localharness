package codegraph

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"
)

var defaultIgnoredDirs = map[string]bool{
	".git":         true,
	".agents":      true,
	"node_modules": true,
	"vendor":       true,
	"bin":          true,
	"dist":         true,
	"gen":          true,
	".gemini":      true,
	".divmora":     true,
	"__pycache__":  true,
	"target":       true,
}

var supportedExtensions = map[string]bool{
	".go":    true,
	".py":    true,
	".ts":    true,
	".tsx":   true,
	".js":    true,
	".jsx":   true,
	".rs":    true,
	".proto": true,
}

// Indexer scans a workspace directory and incrementally synchronizes the code graph.
type Indexer struct {
	workspacePath string
	store         *Store
}

// NewIndexer creates an Indexer for the given workspace and store.
func NewIndexer(workspacePath string, store *Store) *Indexer {
	return &Indexer{
		workspacePath: workspacePath,
		store:         store,
	}
}

// IndexWorkspace performs full or incremental synchronization of the repository.
func (idx *Indexer) IndexWorkspace(ctx context.Context, branch string) (*IndexStats, error) {
	start := time.Now()

	if branch == "" {
		branch = idx.DetectGitBranch()
	}
	idx.store.SetActiveBranch(branch)

	gitignore := LoadGitIgnore(idx.workspacePath)

	fileCache, err := idx.store.GetFileCache()
	if err != nil || fileCache == nil {
		fileCache = make(map[string]FileCacheEntry)
	}

	activeFiles := make(map[string]bool)
	parsedFiles := 0
	cachedFiles := 0
	totalFiles := 0

	const batchSize = 250
	batch := make([]IndexedFile, 0, batchSize)
	knownBlobs := make(map[string]bool)

	flushBatch := func() error {
		if len(batch) == 0 {
			return nil
		}
		if err := idx.store.AddFilesBatch(branch, batch); err != nil {
			return err
		}
		batch = batch[:0]
		return nil
	}

	walkErr := filepath.WalkDir(idx.workspacePath, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return nil // Skip inaccessible paths
		}

		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
		}

		relPath, relErr := filepath.Rel(idx.workspacePath, path)
		if relErr != nil {
			return nil
		}

		if d.IsDir() {
			name := d.Name()
			if defaultIgnoredDirs[name] || (strings.HasPrefix(name, ".") && name != ".") || gitignore.Matches(relPath, true) {
				return filepath.SkipDir
			}
			return nil
		}

		if gitignore.Matches(relPath, false) {
			return nil
		}

		ext := strings.ToLower(filepath.Ext(d.Name()))
		if !supportedExtensions[ext] {
			return nil
		}

		totalFiles++
		activeFiles[relPath] = true

		info, infoErr := d.Info()
		if infoErr != nil {
			return nil
		}
		mtimeNs := info.ModTime().UnixNano()
		fileSize := info.Size()

		// Check if file is unchanged based on mtime and size
		if cached, ok := fileCache[relPath]; ok && cached.MtimeNs == mtimeNs && cached.Size == fileSize && cached.BlobHash != "" {
			if knownBlobs[cached.BlobHash] || idx.store.HasBlob(cached.BlobHash) {
				knownBlobs[cached.BlobHash] = true
				batch = append(batch, IndexedFile{
					FilePath: relPath,
					BlobHash: cached.BlobHash,
					MtimeNs:  mtimeNs,
					Size:     fileSize,
				})
				cachedFiles++
				if len(batch) >= batchSize {
					if err := flushBatch(); err != nil {
						return err
					}
				}
				return nil
			}
		}

		content, err := os.ReadFile(path)
		if err != nil {
			return nil
		}

		blobHash := ComputeBlobHash(content)

		// Check if AST already cached in store or current run
		if knownBlobs[blobHash] || idx.store.HasBlob(blobHash) {
			knownBlobs[blobHash] = true
			batch = append(batch, IndexedFile{
				FilePath: relPath,
				BlobHash: blobHash,
				MtimeNs:  mtimeNs,
				Size:     fileSize,
			})
			cachedFiles++
		} else {
			// Parse source file
			nodes, edges, parseErr := ParseSourceFile(relPath, content)
			if parseErr != nil {
				// Best-effort: continue indexing other files
				return nil
			}

			knownBlobs[blobHash] = true
			batch = append(batch, IndexedFile{
				FilePath: relPath,
				BlobHash: blobHash,
				MtimeNs:  mtimeNs,
				Size:     fileSize,
				Nodes:    nodes,
				Edges:    edges,
			})
			parsedFiles++
		}

		if len(batch) >= batchSize {
			if err := flushBatch(); err != nil {
				return err
			}
		}

		return nil
	})

	if walkErr != nil {
		return nil, fmt.Errorf("codegraph walk %s: %w", idx.workspacePath, walkErr)
	}

	if err := flushBatch(); err != nil {
		return nil, fmt.Errorf("codegraph flush batch: %w", err)
	}

	// Clean up deleted files from manifest
	idx.store.PruneDeletedFiles(branch, activeFiles)

	// Persist to disk
	if err := idx.store.Save(); err != nil {
		return nil, fmt.Errorf("codegraph save: %w", err)
	}

	branchNodes := idx.store.GetBranchNodes(branch)
	branchEdges := idx.store.GetBranchEdges(branch)

	stats := &IndexStats{
		TotalFiles:  totalFiles,
		ParsedFiles: parsedFiles,
		CachedFiles: cachedFiles,
		TotalNodes:  len(branchNodes),
		TotalEdges:  len(branchEdges),
		Duration:    time.Since(start),
		Branch:      branch,
	}

	return stats, nil
}

// UpdateFile incrementally indexes a single modified or created file.
func (idx *Indexer) UpdateFile(ctx context.Context, relPath, branch string) error {
	if branch == "" {
		branch = idx.store.ActiveBranch()
	}

	absPath := filepath.Join(idx.workspacePath, relPath)
	info, statErr := os.Stat(absPath)
	content, err := os.ReadFile(absPath)
	if err != nil {
		if os.IsNotExist(err) {
			return idx.RemoveFile(ctx, relPath, branch)
		}
		return fmt.Errorf("codegraph read %s: %w", absPath, err)
	}

	gitignore := LoadGitIgnore(idx.workspacePath)
	if gitignore.Matches(relPath, false) {
		return idx.RemoveFile(ctx, relPath, branch)
	}

	ext := strings.ToLower(filepath.Ext(relPath))
	if !supportedExtensions[ext] {
		return nil
	}

	var mtimeNs, fileSize int64
	if statErr == nil && info != nil {
		mtimeNs = info.ModTime().UnixNano()
		fileSize = info.Size()
	}

	blobHash := ComputeBlobHash(content)
	nodes, edges, err := ParseSourceFile(relPath, content)
	if err != nil {
		return fmt.Errorf("codegraph parse %s: %w", relPath, err)
	}

	if err := idx.store.AddFilesBatch(branch, []IndexedFile{
		{
			FilePath: relPath,
			BlobHash: blobHash,
			MtimeNs:  mtimeNs,
			Size:     fileSize,
			Nodes:    nodes,
			Edges:    edges,
		},
	}); err != nil {
		return fmt.Errorf("codegraph add file: %w", err)
	}
	return idx.store.Save()
}

// RemoveFile removes a file from the code graph.
func (idx *Indexer) RemoveFile(ctx context.Context, relPath, branch string) error {
	if branch == "" {
		branch = idx.store.ActiveBranch()
	}
	idx.store.RemoveFile(branch, relPath)
	return idx.store.Save()
}

// DetectGitBranch inspects the workspace's .git metadata to find the current active branch.
func (idx *Indexer) DetectGitBranch() string {
	headPath := filepath.Join(idx.workspacePath, ".git", "HEAD")
	data, err := os.ReadFile(headPath)
	if err != nil {
		return "main"
	}

	line := strings.TrimSpace(string(data))
	if strings.HasPrefix(line, "ref: refs/heads/") {
		branch := strings.TrimPrefix(line, "ref: refs/heads/")
		if branch != "" {
			return branch
		}
	}

	return "main"
}
