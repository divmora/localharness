package middleware

import (
	"encoding/json"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"sort"
	"sync"
	"time"
)

// CheckpointStore provides persistent storage for TurnCheckpoints.
// Checkpoints are stored as JSON files in a directory, organized by
// conversation or session identifier.
//
// Note: Metadata values (map[string]any) are serialized as JSON, which means
// Go numeric types may lose precision on round-trip (e.g., int becomes float64).
// If exact type fidelity is needed, use string values in metadata.
//
// This is designed to plug directly into InterruptResume.OnInterrupt:
//
//	store := middleware.NewCheckpointStore("/path/to/checkpoints", logger)
//	ir := middleware.NewInterruptResume(middleware.InterruptResumeConfig{
//	    OnInterrupt: store.Save,
//	})
//
// Later, resume from the latest checkpoint:
//
//	cp, err := store.Latest()
//	if err == nil {
//	    prompt := ir.BuildResumePrompt(*cp)
//	    agent.Chat(ctx, prompt)
//	}
type CheckpointStore struct {
	dir    string
	logger *slog.Logger
	mu     sync.Mutex
}

// NewCheckpointStore creates a store that persists checkpoints as JSON
// files in the given directory. The directory is created if it doesn't exist.
func NewCheckpointStore(dir string, logger *slog.Logger) *CheckpointStore {
	if logger == nil {
		logger = slog.Default()
	}
	return &CheckpointStore{
		dir:    dir,
		logger: logger,
	}
}

// checkpointFile is the on-disk JSON representation.
type checkpointFile struct {
	TurnIndex       int            `json:"turn_index"`
	Prompt          string         `json:"prompt"`
	PartialResponse string         `json:"partial_response,omitempty"`
	TotalTokens     int            `json:"total_tokens"`
	StepCount       int            `json:"step_count"`
	Reason          string         `json:"reason"`
	ReasonDetail    string         `json:"reason_detail,omitempty"`
	Timestamp       string         `json:"timestamp"`
	Metadata        map[string]any `json:"metadata,omitempty"`
}

// Save persists a checkpoint to disk. Safe to use as InterruptResume.OnInterrupt.
func (s *CheckpointStore) Save(cp TurnCheckpoint) {
	s.mu.Lock()
	defer s.mu.Unlock()

	if err := os.MkdirAll(s.dir, 0o755); err != nil {
		s.logger.Error("checkpoint store: failed to create directory",
			"dir", s.dir,
			"error", err,
		)
		return
	}

	file := checkpointFile{
		TurnIndex:       cp.TurnIndex,
		Prompt:          cp.Prompt,
		PartialResponse: cp.PartialResponse,
		TotalTokens:     cp.TotalTokens,
		StepCount:       cp.StepCount,
		Reason:          string(cp.Reason),
		ReasonDetail:    cp.ReasonDetail,
		Timestamp:       cp.Timestamp.Format(time.RFC3339Nano),
		Metadata:        cp.Metadata,
	}

	data, err := json.MarshalIndent(file, "", "  ")
	if err != nil {
		s.logger.Error("checkpoint store: marshal error", "error", err)
		return
	}

	// Filename: checkpoint_<turn>_<timestamp>.json
	name := fmt.Sprintf("checkpoint_%04d_%s.json",
		cp.TurnIndex,
		cp.Timestamp.Format("20060102T150405"),
	)
	path := filepath.Join(s.dir, name)

	if err := os.WriteFile(path, data, 0o644); err != nil {
		s.logger.Error("checkpoint store: write error",
			"path", path,
			"error", err,
		)
		return
	}

	s.logger.Info("checkpoint saved",
		"path", path,
		"turn", cp.TurnIndex,
		"reason", cp.Reason,
	)
}

// Latest returns the most recent checkpoint, or an error if none exist.
func (s *CheckpointStore) Latest() (*TurnCheckpoint, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	files, err := s.listFiles()
	if err != nil {
		return nil, err
	}
	if len(files) == 0 {
		return nil, fmt.Errorf("no checkpoints found in %s", s.dir)
	}

	return s.loadFile(files[len(files)-1])
}

// Load returns the checkpoint for a specific turn index.
// If multiple checkpoints exist for the same turn, returns the latest.
func (s *CheckpointStore) Load(turnIndex int) (*TurnCheckpoint, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	files, err := s.listFiles()
	if err != nil {
		return nil, err
	}

	prefix := fmt.Sprintf("checkpoint_%04d_", turnIndex)
	var match string
	for _, f := range files {
		if len(f) >= len(prefix) && f[:len(prefix)] == prefix {
			match = f // last match wins (sorted, so latest timestamp)
		}
	}

	if match == "" {
		return nil, fmt.Errorf("no checkpoint found for turn %d", turnIndex)
	}

	return s.loadFile(match)
}

// List returns all stored checkpoints, ordered by turn index (ascending).
func (s *CheckpointStore) List() ([]TurnCheckpoint, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	files, err := s.listFiles()
	if err != nil {
		return nil, err
	}

	var result []TurnCheckpoint
	for _, f := range files {
		cp, err := s.loadFile(f)
		if err != nil {
			s.logger.Warn("checkpoint store: skipping corrupt file",
				"file", f,
				"error", err,
			)
			continue
		}
		result = append(result, *cp)
	}
	return result, nil
}

// Clear removes all checkpoints from the store directory.
func (s *CheckpointStore) Clear() error {
	s.mu.Lock()
	defer s.mu.Unlock()

	files, err := s.listFiles()
	if err != nil {
		return err
	}

	for _, f := range files {
		path := filepath.Join(s.dir, f)
		if err := os.Remove(path); err != nil {
			return fmt.Errorf("failed to remove %s: %w", path, err)
		}
	}

	s.logger.Info("checkpoint store cleared", "removed", len(files))
	return nil
}

// Count returns the number of stored checkpoints.
func (s *CheckpointStore) Count() int {
	s.mu.Lock()
	defer s.mu.Unlock()

	files, _ := s.listFiles()
	return len(files)
}

// Dir returns the store's directory path.
func (s *CheckpointStore) Dir() string {
	return s.dir
}

// listFiles returns sorted checkpoint filenames. Caller must hold mu.
func (s *CheckpointStore) listFiles() ([]string, error) {
	entries, err := os.ReadDir(s.dir)
	if os.IsNotExist(err) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("checkpoint store: read dir: %w", err)
	}

	var files []string
	for _, e := range entries {
		if !e.IsDir() && filepath.Ext(e.Name()) == ".json" {
			files = append(files, e.Name())
		}
	}
	sort.Strings(files)
	return files, nil
}

// loadFile reads and parses a checkpoint file. Caller must hold mu.
func (s *CheckpointStore) loadFile(name string) (*TurnCheckpoint, error) {
	path := filepath.Join(s.dir, name)
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read checkpoint %s: %w", name, err)
	}

	var file checkpointFile
	if err := json.Unmarshal(data, &file); err != nil {
		return nil, fmt.Errorf("parse checkpoint %s: %w", name, err)
	}

	ts, _ := time.Parse(time.RFC3339Nano, file.Timestamp)

	return &TurnCheckpoint{
		TurnIndex:       file.TurnIndex,
		Prompt:          file.Prompt,
		PartialResponse: file.PartialResponse,
		TotalTokens:     file.TotalTokens,
		StepCount:       file.StepCount,
		Reason:          InterruptReason(file.Reason),
		ReasonDetail:    file.ReasonDetail,
		Timestamp:       ts,
		Metadata:        file.Metadata,
	}, nil
}
