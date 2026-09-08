package tools

import (
	"context"
	"log/slog"
	"os"
	"strings"
	"testing"
	"time"

	pb "github.com/divmora/localharness/gen/go/localharness/v1"
)

// ─── Cron Parser Tests ──────────────────────────────────────────────────

func TestParseCronExpression(t *testing.T) {
	tests := []struct {
		name    string
		expr    string
		wantErr bool
	}{
		{"every minute", "* * * * *", false},
		{"every 5 minutes", "*/5 * * * *", false},
		{"specific minute", "30 * * * *", false},
		{"specific time", "0 9 * * *", false},
		{"weekday at 9am", "0 9 * * 1-5", false},
		{"first of month", "0 0 1 * *", false},
		{"multiple values", "0,15,30,45 * * * *", false},
		{"range", "0-5 * * * *", false},

		// Errors
		{"too few fields", "* * *", true},
		{"too many fields", "* * * * * *", true},
		{"invalid step", "*/0 * * * *", true},
		{"out of range minute", "60 * * * *", true},
		{"out of range hour", "* 25 * * *", true},
		{"out of range dow", "* * * * 7", true},
		{"invalid range", "5-2 * * * *", true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := parseCronExpression(tt.expr)
			if tt.wantErr && err == nil {
				t.Error("expected error but got nil")
			}
			if !tt.wantErr && err != nil {
				t.Errorf("unexpected error: %v", err)
			}
		})
	}
}

func TestCronScheduleNext(t *testing.T) {
	// Every 5 minutes
	sched, err := parseCronExpression("*/5 * * * *")
	if err != nil {
		t.Fatalf("parse error: %v", err)
	}

	now := time.Date(2025, 6, 1, 10, 3, 0, 0, time.UTC)
	next := sched.Next(now)

	// Should be 10:05
	if next.Minute() != 5 || next.Hour() != 10 {
		t.Errorf("expected 10:05, got %s", next.Format("15:04"))
	}
}

func TestCronScheduleNextHourRollover(t *testing.T) {
	// At minute 30
	sched, err := parseCronExpression("30 * * * *")
	if err != nil {
		t.Fatalf("parse error: %v", err)
	}

	now := time.Date(2025, 6, 1, 10, 35, 0, 0, time.UTC)
	next := sched.Next(now)

	// Should be 11:30
	if next.Minute() != 30 || next.Hour() != 11 {
		t.Errorf("expected 11:30, got %s", next.Format("15:04"))
	}
}

func TestParseFieldVariants(t *testing.T) {
	tests := []struct {
		name     string
		field    string
		min, max int
		wantLen  int
		wantErr  bool
	}{
		{"wildcard", "*", 0, 59, 60, false},
		{"step", "*/15", 0, 59, 4, false},
		{"single", "5", 0, 59, 1, false},
		{"range", "1-5", 0, 59, 5, false},
		{"comma", "1,3,5", 0, 59, 3, false},
		{"mixed", "1,5-7", 0, 59, 4, false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			values, err := parseField(tt.field, tt.min, tt.max)
			if tt.wantErr {
				if err == nil {
					t.Error("expected error")
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if len(values) != tt.wantLen {
				t.Errorf("expected %d values, got %d: %v", tt.wantLen, len(values), values)
			}
		})
	}
}

// ─── Schedule Manager Tests ─────────────────────────────────────────────

func TestOneShotTimer(t *testing.T) {
	logger := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelError}))
	sm := NewScheduleManager(logger)

	taskID := sm.StartOneShot(100*time.Millisecond, "check build")

	if !strings.HasPrefix(taskID, "sched-") {
		t.Errorf("expected sched- prefix, got %q", taskID)
	}

	// Wait for notification
	select {
	case n := <-sm.Notifications():
		if n.Content != "check build" {
			t.Errorf("expected content 'check build', got %q", n.Content)
		}
		if n.TaskID != taskID {
			t.Errorf("expected task_id %q, got %q", taskID, n.TaskID)
		}
	case <-time.After(500 * time.Millisecond):
		t.Fatal("timed out waiting for notification")
	}

	// Verify entry is completed
	entries := sm.List()
	found := false
	for _, e := range entries {
		if e.ID == taskID {
			found = true
			if e.Status != TaskCompleted {
				t.Errorf("expected completed status, got %s", e.Status)
			}
			if e.Iterations != 1 {
				t.Errorf("expected 1 iteration, got %d", e.Iterations)
			}
		}
	}
	if !found {
		t.Error("schedule entry not found")
	}
}

func TestOneShotTimerCancel(t *testing.T) {
	logger := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelError}))
	sm := NewScheduleManager(logger)

	taskID := sm.StartOneShot(5*time.Second, "should be cancelled")

	// Cancel immediately
	if err := sm.Cancel(taskID); err != nil {
		t.Fatalf("cancel error: %v", err)
	}

	// Should not receive notification
	select {
	case <-sm.Notifications():
		t.Fatal("should not receive notification after cancel")
	case <-time.After(200 * time.Millisecond):
		// Good
	}

	// Verify status is killed
	entries := sm.List()
	for _, e := range entries {
		if e.ID == taskID {
			if e.Status != TaskKilled {
				t.Errorf("expected killed status, got %s", e.Status)
			}
		}
	}
}

func TestCronSchedule(t *testing.T) {
	logger := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelError}))
	sm := NewScheduleManager(logger)

	// Invalid cron
	_, err := sm.StartCron("invalid", "test", 0)
	if err == nil {
		t.Fatal("expected error for invalid cron")
	}

	// Valid cron (but we won't wait for it to fire in this test)
	taskID, err := sm.StartCron("*/1 * * * *", "every minute", 3)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if !strings.HasPrefix(taskID, "cron-") {
		t.Errorf("expected cron- prefix, got %q", taskID)
	}

	// Cancel it
	if err := sm.Cancel(taskID); err != nil {
		t.Fatalf("cancel error: %v", err)
	}
}

func TestScheduleManagerShutdown(t *testing.T) {
	logger := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelError}))
	sm := NewScheduleManager(logger)

	sm.StartOneShot(10*time.Second, "timer 1")
	sm.StartOneShot(10*time.Second, "timer 2")

	// Shutdown should cancel all
	sm.Shutdown()

	entries := sm.List()
	if len(entries) != 0 {
		t.Errorf("expected no entries after shutdown, got %d", len(entries))
	}
}

// ─── Schedule Tool Integration Tests ────────────────────────────────────

func TestExecuteScheduleOneShot(t *testing.T) {
	logger := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelError}))
	reg := NewRegistry(nil, logger)

	step := &pb.StepUpdate{
		Action: &pb.StepUpdate_Schedule{
			Schedule: &pb.ActionSchedule{
				DurationSeconds: 1,
				Prompt:          "check status",
			},
		},
	}

	err := executeSchedule(context.Background(), step, reg)
	if err != nil {
		t.Fatalf("executeSchedule failed: %v", err)
	}

	sched := step.GetSchedule()
	if !sched.Success {
		t.Error("expected success")
	}
	if sched.TaskId == "" {
		t.Error("expected task_id")
	}
	if !strings.HasPrefix(sched.TaskId, "sched-") {
		t.Errorf("expected sched- prefix, got %q", sched.TaskId)
	}

	// Clean up
	reg.taskMgr.ScheduleManager().Cancel(sched.TaskId)
}

func TestExecuteScheduleCron(t *testing.T) {
	logger := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelError}))
	reg := NewRegistry(nil, logger)

	step := &pb.StepUpdate{
		Action: &pb.StepUpdate_Schedule{
			Schedule: &pb.ActionSchedule{
				CronExpression: "*/5 * * * *",
				Prompt:         "health check",
				MaxIterations:  3,
			},
		},
	}

	err := executeSchedule(context.Background(), step, reg)
	if err != nil {
		t.Fatalf("executeSchedule failed: %v", err)
	}

	sched := step.GetSchedule()
	if !sched.Success {
		t.Error("expected success")
	}
	if !strings.HasPrefix(sched.TaskId, "cron-") {
		t.Errorf("expected cron- prefix, got %q", sched.TaskId)
	}

	// Clean up
	reg.taskMgr.ScheduleManager().Cancel(sched.TaskId)
}

func TestExecuteScheduleValidation(t *testing.T) {
	logger := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelError}))
	reg := NewRegistry(nil, logger)

	tests := []struct {
		name   string
		action *pb.ActionSchedule
	}{
		{"no prompt", &pb.ActionSchedule{DurationSeconds: 10}},
		{"both set", &pb.ActionSchedule{DurationSeconds: 10, CronExpression: "* * * * *", Prompt: "test"}},
		{"neither set", &pb.ActionSchedule{Prompt: "test"}},
		{"duration too large", &pb.ActionSchedule{DurationSeconds: 1000, Prompt: "test"}},
		{"invalid cron", &pb.ActionSchedule{CronExpression: "invalid", Prompt: "test"}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			step := &pb.StepUpdate{
				Action: &pb.StepUpdate_Schedule{Schedule: tt.action},
			}
			err := executeSchedule(context.Background(), step, reg)
			if err == nil {
				t.Error("expected error")
			}
		})
	}
}

func TestKillScheduleViaTaskManager(t *testing.T) {
	logger := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelError}))
	tm := NewTaskManager(logger, 10)

	// Start a schedule via the schedule manager
	taskID := tm.ScheduleManager().StartOneShot(10*time.Second, "test")

	// Kill it via TaskManager (as manage_task would)
	err := tm.KillTask(taskID)
	if err != nil {
		t.Fatalf("KillTask failed: %v", err)
	}
}
