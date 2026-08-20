package tui

import (
	"strings"
	"testing"
	"time"

	pb "github.com/divmora/localharness/gen/go/localharness/v1"
)

func TestTasksViewManager_Lifecycle(t *testing.T) {
	mgr := NewTasksViewManager()
	if mgr.RunningCount() != 0 || mgr.TotalCount() != 0 {
		t.Errorf("expected 0 tasks initially")
	}

	// Add background task
	mgr.AddOrUpdate(&TaskItemState{
		TaskID:    "task-1",
		Command:   "go test ./...",
		Cwd:       "/workspace",
		Status:    "RUNNING",
		StartedAt: time.Now().Add(-10 * time.Second),
	})

	// Add timer task
	mgr.AddOrUpdate(&TaskItemState{
		TaskID:     "sched-1",
		Command:    "Check deployment in 5m",
		Status:     "RUNNING",
		StartedAt:  time.Now(),
		IsSchedule: true,
	})

	if mgr.RunningCount() != 2 || mgr.TotalCount() != 2 {
		t.Errorf("expected 2 running tasks, got %d", mgr.RunningCount())
	}

	// Update from proto
	mgr.UpdateFromProto([]*pb.TaskInfo{
		{
			TaskId:       "task-1",
			Command:      "go test ./...",
			Status:       "completed",
			ExitCode:     0,
			RecentOutput: "PASS\nok  internal/util  0.05s",
		},
	})

	if mgr.RunningCount() != 1 {
		t.Errorf("expected 1 running task after task-1 completion, got %d", mgr.RunningCount())
	}

	// Test navigation
	if mgr.SelectedTask().TaskID != "task-1" {
		t.Errorf("expected task-1 selected by default")
	}
	mgr.NavigateDown()
	if mgr.SelectedTask().TaskID != "sched-1" {
		t.Errorf("expected sched-1 selected after navigate down")
	}
	mgr.NavigateUp()
	if mgr.SelectedTask().TaskID != "task-1" {
		t.Errorf("expected task-1 selected after navigate up")
	}

	// Render view
	view := mgr.Render(90, 24)
	if !strings.Contains(view, "BACKGROUND TASKS") {
		t.Errorf("expected title in view: %s", view)
	}
	if !strings.Contains(view, "task-1") || !strings.Contains(view, "sched-1") {
		t.Errorf("expected task IDs in view: %s", view)
	}
	if !strings.Contains(view, "Recent Output") {
		t.Errorf("expected recent output section in view: %s", view)
	}
}

func TestRenderActiveBackgroundStrip(t *testing.T) {
	taskMgr := NewTasksViewManager()
	taskMgr.AddOrUpdate(&TaskItemState{
		TaskID:    "task-1",
		Command:   "npm run build --watch",
		Status:    "RUNNING",
		StartedAt: time.Now().Add(-5 * time.Second),
	})

	subMgr := NewSubagentViewManager()
	subMgr.AddOrUpdate(&SubagentState{
		ConversationID: "sub-1",
		Role:           "Researcher",
		TypeName:       "research",
		State:          "RUNNING",
	})

	strip := RenderActiveBackgroundStrip(taskMgr, subMgr, 100)
	if !strings.Contains(strip, "task-1:") || !strings.Contains(strip, "npm run build") {
		t.Errorf("expected task-1 in active strip: %s", strip)
	}
	if !strings.Contains(strip, "Researcher:") || !strings.Contains(strip, "running") {
		t.Errorf("expected Researcher in active strip: %s", strip)
	}

	// Empty case
	emptyStrip := RenderActiveBackgroundStrip(nil, nil, 100)
	if emptyStrip != "" {
		t.Errorf("expected empty strip for nil managers, got %q", emptyStrip)
	}
}
