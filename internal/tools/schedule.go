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
		Description: "Schedule a one-shot timer or a recurring cron job that sends notifications in the background.\n\n" +
			"**NOTE**: This tool call returns immediately and does not pause execution. To wait for the timer to fire, you must stop calling tools to end your turn.\n\n" +
			"Modes:\n" +
			"1. **One-shot timer**: Set a timer for a specified duration that will notify you with your Prompt when it expires. You can control early termination behavior using TimerCondition:\n\n" +
			"- 'never' (default): The timer will always fire after the specified duration, unless explicitly cancelled.\n" +
			"Usage: Use when setting unconditional timers that should always fire after DurationSeconds, unless explicitly cancelled.\n" +
			"- 'any': The timer will be cancelled early if ANY message from any sender is received before the duration.\n" +
			"Usage: Useful when multiple background tasks are running and you want to wait for any update, but with some guarantee that you won't be idle forever in case they are all stuck.\n" +
			"- <sender-id>: The timer will be cancelled early if a message is received from that specific sender ID.\n" +
			"Usage: Use when you're waiting for an update from a specific subagent or task, but want to set some limit on how long to wait.\n\n" +
			"NOTE: When a timer is cancelled early, no separate cancellation notification is sent — the message that satisfied the condition is itself your wakeup, and the timer's tool step result records the cancellation.\n\n" +
			"NOTE: You cannot have multiple concurrently active timers that would early terminate on the same sender ID.\n" +
			"For example, if you already have a liveness timer set with \"any\", you cannot set another timer with \"any\" or any other condition.\n" +
			"If you already have a timer set with early termination on \"task-123\", you cannot set another timer with \"task-123\" or \"any\".\n" +
			"You should rely on the existing timer, or cancel and replace it if needed.\n\n" +
			"Examples:\n\n" +
			"Scenario: User asks explicitly for a reminder in 10 minutes.\n" +
			"Args: DurationSeconds=600, Prompt=\"Remind the user\", TimerCondition=\"never\"\n" +
			"Comments: TimerCondition=\"never\" is appropriate since this timer is unrelated to other ongoing tasks.\n\n" +
			"Scenario: You just ran a command as \"task-123\". You already set a notification on it for 5 minutes, and it just notified you that it's still running. After checking the output, you want to set a new reminder to check on it in 10 minutes if it still hasn't finished.\n" +
			"Args: DurationSeconds=600, Prompt=\"Check on the command status\", TimerCondition=\"task-123\"\n" +
			"Comments: TimerCondition=\"task-123\" is appropriate since the timer is not needed if the command finishes ahead of time.\n\n" +
			"Scenario: You just spawned 10 subagents, and you want to check in on progress after 5 minutes if you haven't heard back from any of them.\n" +
			"Args: DurationSeconds=300, Prompt=\"Check in on the subagents' progress\", TimerCondition=\"any\"\n" +
			"Comments: TimerCondition=\"any\" is appropriate since you are not waiting for any specific subagent.\n\n" +
			"Scenario: You are running a command that you're sure will terminate, and you want to wait for it to finish.\n" +
			"Args: N/A\n" +
			"Comments: A timer is not needed at all in this scenario and will wastefully generate extra messages. Stop calling tools to end your turn instead.\n\n" +
			"2. **Recurring cron**: Set CronExpression to a standard 5-field cron expression (e.g., '*/5 * * * *' for every 5 minutes). Each time the cron triggers, a notification with your Prompt is sent. The cron runs as a background task. Optionally set MaxIterations to limit the number of triggers. Optionally set IsDaemon to declare how the cron relates to your current task: leave it false (the default) when the cron is how your current task makes progress — polling or monitoring a job until it completes, heartbeat/liveness, or reminders — so your task stays active until the cron ends; set it true only when the cron is an independent standing job that should keep running after your current task is done — e.g. a recurring report or a maintenance job the user asked you to keep going — so you can finish now while it keeps firing in the background.\n\n" +
			"Examples:\n" +
			"- Poll deployment status every 5 minutes until it passes: CronExpression=\"*/5 * * * *\", Prompt=\"Check deployment status and report progress\", IsDaemon=false\n" +
			"- Run a health check every hour, up to 3 times: CronExpression=\"0 * * * *\", MaxIterations=3, Prompt=\"Run the health check script and report results\", IsDaemon=false\n" +
			"- Inspect newly filed issues in the last 24h and post a daily summary report: CronExpression=\"0 9 * * *\", Prompt=\"Summarize issues filed in the last 24h and post the report\", IsDaemon=true\n\n" +
			"General Reminders:\n" +
			"- You must specify exactly one of DurationSeconds or CronExpression.\n" +
			"- Always provide a Prompt describing what the notification should say.\n" +
			"- Never run a background 'sleep' command to set a timer, use this tool instead.\n" +
			"- To cancel a running timer or cron schedule, use the manage_task tool with the task ID returned by this tool.",
		Parameters: map[string]interface{}{
			"type": "object",
			"properties": map[string]interface{}{
				"CronExpression": map[string]interface{}{
					"type":        "string",
					"description": "A standard cron expression (5 fields: minute hour day-of-month month day-of-week). Use for recurring schedules. Mutually exclusive with DurationSeconds. Example: '*/5 * * * *' for every 5 minutes.",
				},
				"DurationSeconds": map[string]interface{}{
					"type":        "integer",
					"description": "The number of seconds to wait. Use for one-shot timers. Mutually exclusive with CronExpression.",
				},
				"IsDaemon": map[string]interface{}{
					"type":        "boolean",
					"description": "Optional. Set to true only when the cron is an independent, standing job that should keep firing even after your current task is done (e.g. a recurring daily/weekly report or a standing maintenance job). Leave false (the default) whenever the cron is part of finishing your current task — including polling or monitoring a running job until it completes, heartbeat/liveness, or reminders.",
				},
				"MaxIterations": map[string]interface{}{
					"type":        "integer",
					"description": "Optional. Maximum number of times the cron schedule will fire before stopping. Only applicable when CronExpression is set. Defaults to unlimited.",
				},
				"Prompt": map[string]interface{}{
					"type":        "string",
					"description": "The message content to include in the notification when the timer fires or cron triggers. This is sent to the agent as a high-priority message.",
				},
				"TimerCondition": map[string]interface{}{
					"type":        "string",
					"description": "Optional. Controls when a one-shot timer should early terminate upon receiving a message. Options: 'never' (default, timer unconditionally waits until expiry), 'any' (timer cancels if any message is received), or a specific sender ID (timer cancels only if a message is received from that specific subagent conversation ID or background task ID). Only applicable when DurationSeconds is set.",
				},
				"ToolAction": map[string]interface{}{
					"type":        "string",
					"description": "Brief 2-5 word phrase in -ing form describing the specific action. Capitalize like a sentence. Some examples: 'Analyzing directory', 'Searching the web', 'Checking git status', 'Running tests', 'Searching code'.",
				},
				"ToolSummary": map[string]interface{}{
					"type":        "string",
					"description": "Brief 2-5 word noun phrase describing the specific task. Capitalize like a sentence. Some examples: 'Directory analysis', 'Web search', 'Git status check', 'Test execution', 'Code search'.",
				},
			},
			"required": []string{"Prompt", "ToolSummary", "ToolAction"},
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
		sched.FormattedOutput = fmt.Sprintf("Scheduled a timer to fire in %d seconds. Task ID: %s", sched.DurationSeconds, taskID)

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
		sched.FormattedOutput = fmt.Sprintf("Scheduled recurring cron (%s). Task ID: %s", sched.CronExpression, taskID)

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
