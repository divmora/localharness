// Package conversation manages conversation state, directories, and transcript logging.
//
// Directory structure:
//
//	appDataDir/
//	├── conversations/
//	│   └── <uuid>.pb                    # ConversationState protobuf (single file)
//	├── brain/
//	│   └── <uuid>/
//	│       ├── .system_generated/
//	│       │   ├── steps/<N>/content.md  # Individual step content
//	│       │   ├── messages/             # Message content
//	│       │   ├── logs/
//	│       │   │   └── transcript.jsonl  # Human-readable log
//	│       │   └── tasks/                # Background task logs
//	│       ├── scratch/                  # Temp scripts/data
//	│       ├── implementation_plan.md    # Artifacts at brain root
//	│       ├── implementation_plan.md.metadata.json
//	│       ├── task.md
//	│       └── ...
package conversation

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"time"

	"github.com/google/uuid"
	"google.golang.org/protobuf/proto"

	pb "github.com/divmora/localharness/gen/go/localharness/v1"
	"github.com/divmora/localharness/internal/config"
	"github.com/divmora/localharness/internal/errors"
	"github.com/divmora/localharness/internal/util"
)

// Manager handles conversation lifecycle and persistence.
type Manager struct {
	appDataDir       string
	conversationsDir string // appDataDir/conversations/ — stores <uuid>.pb
	brainDir         string // appDataDir/brain/ — stores artifacts, logs, steps
}

// NewManager creates a conversation manager rooted at appDataDir.
func NewManager(appDataDir string) (*Manager, error) {
	convDir := filepath.Join(appDataDir, "conversations")
	brainDir := filepath.Join(appDataDir, "brain")

	for _, d := range []string{convDir, brainDir} {
		if err := os.MkdirAll(d, 0755); err != nil {
			return nil, errors.Wrap(err, errors.ErrCodeConfiguration,
				"cannot create conversation directory").
				WithContext("directory", d).
				WithContext("app_data_dir", appDataDir).
				WithComponent("conversation")
		}
	}

	return &Manager{
		appDataDir:       appDataDir,
		conversationsDir: convDir,
		brainDir:         brainDir,
	}, nil
}

// Create initializes a new conversation with a UUID v7 (time-ordered).
func (m *Manager) Create(cfg *pb.HarnessConfig) (*Conversation, error) {
	id := util.NewUUID()
	return m.init(id, cfg)
}

// CreateWithID initializes a new conversation with a pre-assigned ID.
// Used by the engine when spawning subagents that already have a UUID.
func (m *Manager) CreateWithID(id string, cfg *pb.HarnessConfig) (*Conversation, error) {
	return m.init(id, cfg)
}

// Resume loads an existing conversation by ID from its .pb file.
func (m *Manager) Resume(id string) (*Conversation, error) {
	pbPath := filepath.Join(m.conversationsDir, id+".pb")
	if _, err := os.Stat(pbPath); err != nil {
		return nil, errors.Wrap(err, errors.ErrCodeConversationNotFound,
			"conversation not found").
			WithContext("conversation_id", id).
			WithContext("path", pbPath).
			WithComponent("conversation")
	}

	conv := m.newConversation(id)

	// Load state from conversations/<uuid>.pb
	data, err := os.ReadFile(pbPath)
	if err != nil {
		return nil, errors.Wrap(err, errors.ErrCodeFileNotFound,
			"cannot read conversation state").
			WithContext("conversation_id", id).
			WithContext("path", pbPath).
			WithComponent("conversation")
	}

	state := &pb.ConversationState{}
	if err := proto.Unmarshal(data, state); err != nil {
		return nil, errors.Wrap(err, errors.ErrCodeStateCorruption,
			"corrupt conversation state").
			WithContext("conversation_id", id).
			WithContext("path", pbPath).
			WithComponent("conversation")
	}
	conv.State = state

	return conv, nil
}

// init creates the directory structure and initial state for a conversation.
func (m *Manager) init(id string, cfg *pb.HarnessConfig) (*Conversation, error) {
	conv := m.newConversation(id)

	// Create brain directory structure
	dirs := []string{
		conv.BrainDir,
		filepath.Join(conv.BrainDir, ".system_generated"),
		filepath.Join(conv.BrainDir, ".system_generated", "steps"),
		filepath.Join(conv.BrainDir, ".system_generated", "messages"),
		filepath.Join(conv.BrainDir, ".system_generated", "logs"),
		filepath.Join(conv.BrainDir, ".system_generated", "tasks"),
		filepath.Join(conv.BrainDir, "scratch"),
	}

	for _, d := range dirs {
		if err := os.MkdirAll(d, 0755); err != nil {
			return nil, errors.Wrap(err, errors.ErrCodeConfiguration,
				"cannot create conversation directory").
				WithContext("directory", d).
				WithContext("conversation_id", id).
				WithComponent("conversation")
		}
	}

	// Initialize state
	now := time.Now().UTC().Format(time.RFC3339)
	conv.State = &pb.ConversationState{
		ConversationId: id,
		CreatedAt:      now,
		UpdatedAt:      now,
		HarnessVersion: config.HarnessVersion,
		Config:         cfg,
		Status:         pb.ConversationState_STATUS_ACTIVE,
	}

	// Persist initial state to conversations/<uuid>.pb
	if err := conv.SaveState(); err != nil {
		return nil, errors.Wrap(err, errors.ErrCodeConfiguration,
			"cannot save initial conversation state").
			WithContext("conversation_id", id).
			WithComponent("conversation")
	}

	return conv, nil
}

// List returns all conversation IDs.
func (m *Manager) List() ([]string, error) {
	entries, err := os.ReadDir(m.conversationsDir)
	if err != nil {
		return nil, err
	}

	var ids []string
	for _, e := range entries {
		if !e.IsDir() && filepath.Ext(e.Name()) == ".pb" {
			name := e.Name()[:len(e.Name())-3] // strip .pb
			if _, parseErr := uuid.Parse(name); parseErr == nil {
				ids = append(ids, name)
			}
		}
	}
	return ids, nil
}

// newConversation creates a Conversation handle without loading state.
func (m *Manager) newConversation(id string) *Conversation {
	brainDir := filepath.Join(m.brainDir, id)
	sysGen := filepath.Join(brainDir, ".system_generated")

	return &Conversation{
		ID: id,

		// conversations/<uuid>.pb — the protobuf state file
		StatePath: filepath.Join(m.conversationsDir, id+".pb"),

		// brain/<uuid>/ — artifacts, steps, logs
		BrainDir:   brainDir,
		ScratchDir: filepath.Join(brainDir, "scratch"),

		// brain/<uuid>/.system_generated/
		StepsDir:       filepath.Join(sysGen, "steps"),
		MessagesDir:    filepath.Join(sysGen, "messages"),
		LogsDir:        filepath.Join(sysGen, "logs"),
		TasksDir:       filepath.Join(sysGen, "tasks"),
		TranscriptPath: filepath.Join(sysGen, "logs", "transcript.jsonl"),
	}
}

// ─── Conversation ───────────────────────────────────────────────────────

// Conversation represents a single conversation session with persistent state.
type Conversation struct {
	ID string

	// conversations/<uuid>.pb — protobuf state
	StatePath string

	// brain/<uuid>/ — artifacts live here
	BrainDir   string
	ScratchDir string

	// brain/<uuid>/.system_generated/
	StepsDir       string // steps/<N>/content.md
	MessagesDir    string // messages/
	LogsDir        string // logs/
	TasksDir       string // tasks/
	TranscriptPath string // logs/transcript.jsonl

	// In-memory state
	State *pb.ConversationState

	mu sync.Mutex
}

// ─── Step & Trajectory Management ───────────────────────────────────────

// NextStepIndex returns the next step index and increments the counter.
func (c *Conversation) NextStepIndex() int32 {
	c.mu.Lock()
	defer c.mu.Unlock()
	idx := c.State.StepCount
	c.State.StepCount++
	return idx
}

// NextTrajectoryID returns a new trajectory ID.
func (c *Conversation) NextTrajectoryID() string {
	c.mu.Lock()
	defer c.mu.Unlock()
	id := fmt.Sprintf("traj_%d", c.State.TrajectoryCount)
	c.State.TrajectoryCount++
	return id
}

// ─── Message History ────────────────────────────────────────────────────

// AddMessage appends a message to the conversation history.
func (c *Conversation) AddMessage(msg *pb.ConversationMessage) {
	c.mu.Lock()
	defer c.mu.Unlock()
	msg.Timestamp = time.Now().UTC().Format(time.RFC3339)
	c.State.Messages = append(c.State.Messages, msg)
}

// Messages returns the conversation messages for LLM context.
func (c *Conversation) Messages() []*pb.ConversationMessage {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.State.Messages
}

// SetMessages replaces the conversation message history.
func (c *Conversation) SetMessages(msgs []*pb.ConversationMessage) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.State.Messages = msgs
}

// AddUsage accumulates token usage.
func (c *Conversation) AddUsage(usage *pb.UsageMetadata) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.State.TotalUsage == nil {
		c.State.TotalUsage = &pb.UsageMetadata{}
	}
	c.State.TotalUsage.PromptTokens += usage.PromptTokens
	c.State.TotalUsage.CompletionTokens += usage.CompletionTokens
	c.State.TotalUsage.ThinkingTokens += usage.ThinkingTokens
	c.State.TotalUsage.TotalTokens += usage.TotalTokens
	c.State.TotalUsage.CachedTokens += usage.CachedTokens
}

// AllocateBudget sets the total budget.
func (c *Conversation) AllocateBudget(amount float64) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.State.BudgetAllocated = amount
}

// AddSpend adds to the spent budget.
func (c *Conversation) AddSpend(amount float64) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.State.BudgetSpent += amount
}

// ─── Transcript Logging (JSONL to logs/transcript.jsonl) ────────────────

// LogStep appends a step entry to the transcript.jsonl (human-readable log).
func (c *Conversation) LogStep(entry *TranscriptJSONEntry) error {
	c.mu.Lock()
	defer c.mu.Unlock()

	entry.CreatedAt = time.Now().UTC().Format(time.RFC3339)

	data, err := json.Marshal(entry)
	if err != nil {
		return errors.Wrap(err, errors.ErrCodeConfiguration,
			"transcript marshal error").
			WithContext("conversation_id", c.ID).
			WithComponent("conversation")
	}

	f, err := os.OpenFile(c.TranscriptPath, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0644)
	if err != nil {
		return errors.Wrap(err, errors.ErrCodeFileNotFound,
			"transcript open error").
			WithContext("conversation_id", c.ID).
			WithContext("path", c.TranscriptPath).
			WithComponent("conversation")
	}
	defer f.Close()

	if _, err := f.Write(append(data, '\n')); err != nil {
		return errors.Wrap(err, errors.ErrCodeConfiguration,
			"transcript write error").
			WithContext("conversation_id", c.ID).
			WithContext("path", c.TranscriptPath).
			WithComponent("conversation")
	}

	return nil
}

// SaveStepContent saves step content to .system_generated/steps/<N>/content.md.
func (c *Conversation) SaveStepContent(stepIndex int32, content string) error {
	stepDir := filepath.Join(c.StepsDir, fmt.Sprintf("%d", stepIndex))
	if err := os.MkdirAll(stepDir, 0755); err != nil {
		return err
	}
	return atomicWriteFile(filepath.Join(stepDir, "content.md"), []byte(content), 0644)
}

// TranscriptJSONEntry represents one line in transcript.jsonl.
// Format matches the Antigravity IDE transcript for compatibility.
type TranscriptJSONEntry struct {
	StepIndex int32  `json:"step_index"`
	Source    string `json:"source"`
	Type      string `json:"type"`
	Status    string `json:"status,omitempty"`
	CreatedAt string `json:"created_at"`

	// Content contains tool result text (truncated for large outputs),
	// model response text, user messages, or system messages.
	Content string `json:"content,omitempty"`

	// Thinking contains the model's thinking/reasoning text (if available).
	Thinking string `json:"thinking,omitempty"`

	// ToolCalls is populated on model response entries that include tool calls.
	// Each entry has "name" and "args" keys.
	ToolCalls []TranscriptToolCall `json:"tool_calls,omitempty"`

	// Error contains error details for ERROR_MESSAGE type entries.
	Error string `json:"error,omitempty"`
}

// TranscriptToolCall represents a single tool call in a transcript entry.
type TranscriptToolCall struct {
	Name string            `json:"name"`
	Args map[string]string `json:"args,omitempty"`
}

// ─── Artifact Helpers ───────────────────────────────────────────────────

// ArtifactPath returns the path for an artifact file in brain/<uuid>/.
func (c *Conversation) ArtifactPath(filename string) string {
	return filepath.Join(c.BrainDir, filename)
}

// SaveArtifactMetadata writes the .metadata.json sidecar for an artifact.
func (c *Conversation) SaveArtifactMetadata(filename string, meta *ArtifactMetadata) error {
	meta.UpdatedAt = time.Now().UTC().Format(time.RFC3339Nano)
	data, err := json.MarshalIndent(meta, "", "  ")
	if err != nil {
		return err
	}
	metaPath := filepath.Join(c.BrainDir, filename+".metadata.json")
	return atomicWriteFile(metaPath, data, 0644)
}

// ArtifactMetadata represents metadata for a conversation artifact.
type ArtifactMetadata struct {
	ArtifactType    string `json:"artifactType"`
	Summary         string `json:"summary"`
	RequestFeedback bool   `json:"requestFeedback,omitempty"`
	UpdatedAt       string `json:"updatedAt"`
}

// ─── State Persistence (conversations/<uuid>.pb) ────────────────────────

// SaveState persists the conversation state to conversations/<uuid>.pb.
func (c *Conversation) SaveState() error {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.saveStateLocked()
}

// SaveAll persists state (for convenience — transcript is append-only via LogStep).
func (c *Conversation) SaveAll() error {
	return c.SaveState()
}

func (c *Conversation) saveStateLocked() error {
	c.State.UpdatedAt = time.Now().UTC().Format(time.RFC3339)

	data, err := proto.Marshal(c.State)
	if err != nil {
		return errors.Wrap(err, errors.ErrCodeConfiguration,
			"state marshal error").
			WithContext("conversation_id", c.ID).
			WithComponent("conversation")
	}

	return atomicWriteFile(c.StatePath, data, 0644)
}

// atomicWriteFile writes data to a temp file in the same directory, then
// atomically renames it to the target path. This prevents corrupt reads
// if another process (e.g., lhctl) reads during a write, or if the
// process crashes mid-write.
func atomicWriteFile(path string, data []byte, perm os.FileMode) error {
	dir := filepath.Dir(path)

	tmp, err := os.CreateTemp(dir, ".tmp-*")
	if err != nil {
		return errors.Wrap(err, errors.ErrCodePersistenceError,
			"atomic write: create temp").
			WithContext("directory", dir).
			WithContext("path", path).
			WithComponent("conversation")
	}
	tmpPath := tmp.Name()

	// Clean up temp file on any error path.
	success := false
	defer func() {
		if !success {
			os.Remove(tmpPath)
		}
	}()

	if _, err := tmp.Write(data); err != nil {
		tmp.Close()
		return errors.Wrap(err, errors.ErrCodePersistenceError,
			"atomic write: write temp").
			WithContext("temp_path", tmpPath).
			WithContext("path", path).
			WithComponent("conversation")
	}

	// Sync to disk before rename to ensure durability.
	if err := tmp.Sync(); err != nil {
		tmp.Close()
		return errors.Wrap(err, errors.ErrCodePersistenceError,
			"atomic write: sync").
			WithContext("temp_path", tmpPath).
			WithComponent("conversation")
	}

	if err := tmp.Close(); err != nil {
		return errors.Wrap(err, errors.ErrCodePersistenceError,
			"atomic write: close").
			WithContext("temp_path", tmpPath).
			WithComponent("conversation")
	}

	// Set desired permissions (CreateTemp uses 0600 by default).
	if err := os.Chmod(tmpPath, perm); err != nil {
		return errors.Wrap(err, errors.ErrCodePersistenceError,
			"atomic write: chmod").
			WithContext("temp_path", tmpPath).
			WithComponent("conversation")
	}

	// Atomic rename — guaranteed on same filesystem (same directory).
	if err := os.Rename(tmpPath, path); err != nil {
		return errors.Wrap(err, errors.ErrCodePersistenceError,
			"atomic write: rename").
			WithContext("temp_path", tmpPath).
			WithContext("target_path", path).
			WithComponent("conversation")
	}

	success = true
	return nil
}
