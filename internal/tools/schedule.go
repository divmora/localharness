package tools

import (
	"context"
	"fmt"
	"log/slog"
	"strings"
	"sync"
	"time"

	pb "github.com/divmora/localharness/gen/go/localharness/v1"
	"github.com/divmora/localharness/internal/util"
)

func registerSchedule(r *Registry) {
	r.Register("schedule", executeSchedule, ToolSchema{
		Name: "schedule",
		Description: "Schedule a one-shot timer or a recurring cron job that sends notifications in the background. " +
			"One-shot timers fire once after the specified duration (max 900 seconds). " +
			"If you receive any message before the timer expires, the timer is cancelled silently. " +
			"Cron uses a standard 5-field expression (minute hour day-of-month month day-of-week). " +
			"You must specify exactly one of duration_seconds or cron_expression. " +
			"Never run a background 'sleep' command to set a timer, use this tool instead.",
		Parameters: map[string]interface{}{
			"type": "object",
			"properties": map[string]interface{}{
				"duration_seconds": map[string]interface{}{
					"type":        "integer",
					"description": "Fire once after this many seconds (max 900). Mutually exclusive with cron_expression.",
				},
				"cron_expression": map[string]interface{}{
					"type":        "string",
					"description": "Standard 5-field cron expression (minute hour day-of-month month day-of-week). Mutually exclusive with duration_seconds.",
				},
				"max_iterations": map[string]interface{}{
					"type":        "integer",
					"description": "Maximum number of cron triggers before stopping. Only for cron schedules. 0 = unlimited.",
				},
				"prompt": map[string]interface{}{
					"type":        "string",
					"description": "The notification message when the timer/cron fires.",
				},
			},
			"required": []string{"prompt"},
		},
	})
}

func executeSchedule(ctx context.Context, step *pb.StepUpdate, r *Registry) error {
	sched := step.GetSchedule()
	if sched == nil {
		return fmt.Errorf("schedule: missing schedule action")
	}

	if sched.Prompt == "" {
		return fmt.Errorf("schedule: prompt is required")
	}

	hasDuration := sched.DurationSeconds > 0
	hasCron := sched.CronExpression != ""

	if hasDuration && hasCron {
		return fmt.Errorf("schedule: specify either duration_seconds or cron_expression, not both")
	}
	if !hasDuration && !hasCron {
		return fmt.Errorf("schedule: specify either duration_seconds or cron_expression")
	}

	if r.taskMgr == nil {
		return fmt.Errorf("schedule: task manager not initialized")
	}

	sm := r.taskMgr.ScheduleManager()

	if hasDuration {
		// One-shot timer
		if sched.DurationSeconds > 900 {
			return fmt.Errorf("schedule: duration_seconds max is 900 (15 minutes), got %d", sched.DurationSeconds)
		}

		taskID := sm.StartOneShot(
			time.Duration(sched.DurationSeconds)*time.Second,
			sched.Prompt,
		)

		sched.TaskId = taskID
		sched.Success = true

		r.logger.Info("scheduled one-shot timer",
			"task_id", taskID,
			"seconds", sched.DurationSeconds,
		)
	} else {
		// Recurring cron
		taskID, err := sm.StartCron(
			sched.CronExpression,
			sched.Prompt,
			int(sched.MaxIterations),
		)
		if err != nil {
			return fmt.Errorf("schedule: %w", err)
		}

		sched.TaskId = taskID
		sched.Success = true

		r.logger.Info("scheduled cron job",
			"task_id", taskID,
			"cron", sched.CronExpression,
			"max_iterations", sched.MaxIterations,
		)
	}

	return nil
}

// ─── Schedule Manager ───────────────────────────────────────────────────

// ScheduleEntry represents an active timer or cron schedule.
type ScheduleEntry struct {
	ID             string
	Type           string // "one_shot" or "cron"
	Prompt         string
	CronExpression string
	MaxIterations  int
	Iterations     int
	CreatedAt      time.Time
	LastFiredAt    time.Time
	Status         TaskStatus // running, completed, killed
	cancel         context.CancelFunc
	done           chan struct{}
}

// ScheduleManager manages timers and cron schedules.
// It stores notifications that can be polled by the engine.
type ScheduleManager struct {
	mu            sync.RWMutex
	entries       map[string]*ScheduleEntry
	notifications chan SystemMessage
	logger        *slog.Logger
}

// NewScheduleManager creates a new schedule manager.
func NewScheduleManager(logger *slog.Logger) *ScheduleManager {
	return &ScheduleManager{
		entries:       make(map[string]*ScheduleEntry),
		notifications: make(chan SystemMessage, 100),
		logger:        logger,
	}
}

// Notifications returns the read-only notification channel for the engine to consume.
func (sm *ScheduleManager) Notifications() <-chan SystemMessage {
	return sm.notifications
}

// NotifyChannel returns the writable end of the notification channel.
// Used to let TaskManager push completion events to the same unified channel.
func (sm *ScheduleManager) NotifyChannel() chan<- SystemMessage {
	return sm.notifications
}

// StartOneShot starts a one-shot timer that fires after the given duration.
func (sm *ScheduleManager) StartOneShot(duration time.Duration, prompt string) string {
	s := util.NewUUID()
	id := "sched-" + s[len(s)-8:]
	ctx, cancel := context.WithCancel(context.Background())

	entry := &ScheduleEntry{
		ID:        id,
		Type:      "one_shot",
		Prompt:    prompt,
		CreatedAt: time.Now(),
		Status:    TaskRunning,
		cancel:    cancel,
		done:      make(chan struct{}),
	}

	sm.mu.Lock()
	sm.entries[id] = entry
	sm.mu.Unlock()

	go func() {
		defer close(entry.done)

		select {
		case <-ctx.Done():
			// Cancelled
			sm.mu.Lock()
			entry.Status = TaskKilled
			sm.mu.Unlock()
			return
		case <-time.After(duration):
			// Timer fired
			sm.mu.Lock()
			entry.LastFiredAt = time.Now()
			entry.Iterations = 1
			entry.Status = TaskCompleted
			sm.mu.Unlock()

			sm.notify(id, "timer", prompt)
			sm.logger.Info("one-shot timer fired", "task_id", id)
		}
	}()

	return id
}

// StartCron starts a recurring schedule based on a cron expression.
// Supported: standard 5-field cron (minute hour dom month dow).
func (sm *ScheduleManager) StartCron(expression, prompt string, maxIterations int) (string, error) {
	// Parse and validate the cron expression
	sched, err := parseCronExpression(expression)
	if err != nil {
		return "", fmt.Errorf("invalid cron expression %q: %w", expression, err)
	}

	s := util.NewUUID()
	id := "cron-" + s[len(s)-8:]
	ctx, cancel := context.WithCancel(context.Background())

	entry := &ScheduleEntry{
		ID:             id,
		Type:           "cron",
		Prompt:         prompt,
		CronExpression: expression,
		MaxIterations:  maxIterations,
		CreatedAt:      time.Now(),
		Status:         TaskRunning,
		cancel:         cancel,
		done:           make(chan struct{}),
	}

	sm.mu.Lock()
	sm.entries[id] = entry
	sm.mu.Unlock()

	go func() {
		defer close(entry.done)

		for {
			next := sched.Next(time.Now())
			wait := time.Until(next)

			select {
			case <-ctx.Done():
				sm.mu.Lock()
				entry.Status = TaskKilled
				sm.mu.Unlock()
				return
			case <-time.After(wait):
				sm.mu.Lock()
				entry.Iterations++
				entry.LastFiredAt = time.Now()
				iter := entry.Iterations
				sm.mu.Unlock()

				sm.notify(id, "cron", prompt)
				sm.logger.Info("cron fired", "task_id", id, "iteration", iter)

				// Check max iterations
				if maxIterations > 0 && iter >= maxIterations {
					sm.mu.Lock()
					entry.Status = TaskCompleted
					sm.mu.Unlock()
					sm.logger.Info("cron completed max iterations", "task_id", id)
					return
				}
			}
		}
	}()

	return id, nil
}

// Cancel cancels a running schedule.
func (sm *ScheduleManager) Cancel(id string) error {
	sm.mu.RLock()
	entry, ok := sm.entries[id]
	sm.mu.RUnlock()

	if !ok {
		return fmt.Errorf("unknown schedule: %s", id)
	}

	if entry.Status != TaskRunning {
		return fmt.Errorf("schedule %s is not running (status: %s)", id, entry.Status)
	}

	entry.cancel()
	<-entry.done
	return nil
}

// List returns info about all schedule entries.
func (sm *ScheduleManager) List() []ScheduleEntry {
	sm.mu.RLock()
	defer sm.mu.RUnlock()

	result := make([]ScheduleEntry, 0, len(sm.entries))
	for _, e := range sm.entries {
		result = append(result, *e)
	}
	return result
}

// Shutdown cancels all running schedules.
func (sm *ScheduleManager) Shutdown() {
	sm.mu.Lock()
	defer sm.mu.Unlock()

	for _, entry := range sm.entries {
		if entry.Status == TaskRunning {
			entry.cancel()
		}
	}
	sm.entries = make(map[string]*ScheduleEntry)
}

func (sm *ScheduleManager) notify(taskID, source, prompt string) {
	select {
	case sm.notifications <- SystemMessage{
		Source:  source,
		TaskID:  taskID,
		Content: prompt,
		FiredAt: time.Now(),
	}:
	default:
		sm.logger.Warn("schedule notification channel full, dropping", "task_id", taskID)
	}
}

// ─── Simple Cron Parser ─────────────────────────────────────────────────

// cronSchedule represents a parsed cron expression.
type cronSchedule struct {
	minutes     []int // 0-59
	hours       []int // 0-23
	daysOfMonth []int // 1-31
	months      []int // 1-12
	daysOfWeek  []int // 0-6 (0=Sunday)
}

// Next returns the next time the cron should fire after the given time.
func (cs *cronSchedule) Next(after time.Time) time.Time {
	// Start from the next minute
	t := after.Truncate(time.Minute).Add(time.Minute)

	// Search for up to 2 years
	deadline := after.Add(2 * 365 * 24 * time.Hour)

	for t.Before(deadline) {
		if cs.matches(t) {
			return t
		}
		t = t.Add(time.Minute)
	}

	// Fallback: 1 hour from now (should never happen with valid cron)
	return after.Add(time.Hour)
}

func (cs *cronSchedule) matches(t time.Time) bool {
	return containsInt(cs.minutes, t.Minute()) &&
		containsInt(cs.hours, t.Hour()) &&
		containsInt(cs.daysOfMonth, t.Day()) &&
		containsInt(cs.months, int(t.Month())) &&
		containsInt(cs.daysOfWeek, int(t.Weekday()))
}

// parseCronExpression parses a 5-field cron expression.
// Fields: minute hour day-of-month month day-of-week
// Supports: *, */N, N, N-M, N,M,O
func parseCronExpression(expr string) (*cronSchedule, error) {
	fields := strings.Fields(expr)
	if len(fields) != 5 {
		return nil, fmt.Errorf("expected 5 fields, got %d", len(fields))
	}

	minutes, err := parseField(fields[0], 0, 59)
	if err != nil {
		return nil, fmt.Errorf("minute field: %w", err)
	}
	hours, err := parseField(fields[1], 0, 23)
	if err != nil {
		return nil, fmt.Errorf("hour field: %w", err)
	}
	dom, err := parseField(fields[2], 1, 31)
	if err != nil {
		return nil, fmt.Errorf("day-of-month field: %w", err)
	}
	months, err := parseField(fields[3], 1, 12)
	if err != nil {
		return nil, fmt.Errorf("month field: %w", err)
	}
	dow, err := parseField(fields[4], 0, 6)
	if err != nil {
		return nil, fmt.Errorf("day-of-week field: %w", err)
	}

	return &cronSchedule{
		minutes:     minutes,
		hours:       hours,
		daysOfMonth: dom,
		months:      months,
		daysOfWeek:  dow,
	}, nil
}

// parseField parses a single cron field into a list of matching values.
func parseField(field string, min, max int) ([]int, error) {
	if field == "*" {
		return rangeSlice(min, max), nil
	}

	// Handle */N (step)
	if strings.HasPrefix(field, "*/") {
		step := 0
		if _, err := fmt.Sscanf(field, "*/%d", &step); err != nil || step <= 0 {
			return nil, fmt.Errorf("invalid step: %s", field)
		}
		var values []int
		for i := min; i <= max; i += step {
			values = append(values, i)
		}
		return values, nil
	}

	// Handle comma-separated values and ranges
	var values []int
	for _, part := range strings.Split(field, ",") {
		part = strings.TrimSpace(part)
		if strings.Contains(part, "-") {
			// Range: N-M
			var lo, hi int
			if _, err := fmt.Sscanf(part, "%d-%d", &lo, &hi); err != nil {
				return nil, fmt.Errorf("invalid range: %s", part)
			}
			if lo < min || hi > max || lo > hi {
				return nil, fmt.Errorf("range %d-%d out of bounds [%d-%d]", lo, hi, min, max)
			}
			for i := lo; i <= hi; i++ {
				values = append(values, i)
			}
		} else {
			// Single value
			var v int
			if _, err := fmt.Sscanf(part, "%d", &v); err != nil {
				return nil, fmt.Errorf("invalid value: %s", part)
			}
			if v < min || v > max {
				return nil, fmt.Errorf("value %d out of bounds [%d-%d]", v, min, max)
			}
			values = append(values, v)
		}
	}

	if len(values) == 0 {
		return nil, fmt.Errorf("no values parsed from: %s", field)
	}
	return values, nil
}

func rangeSlice(min, max int) []int {
	result := make([]int, 0, max-min+1)
	for i := min; i <= max; i++ {
		result = append(result, i)
	}
	return result
}

func containsInt(values []int, v int) bool {
	for _, val := range values {
		if val == v {
			return true
		}
	}
	return false
}
