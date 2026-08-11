# Schedule/Timer Tool

The `schedule` tool allows the agent to set one-shot timers and recurring cron jobs that deliver notifications. This is useful for background monitoring, polling long-running tasks, and periodic health checks.

## One-Shot Timers

Fire once after a delay (max 900 seconds / 15 minutes).

```json
{
  "duration_seconds": 60,
  "prompt": "Check if the build has completed"
}
```

When the timer fires, a `ScheduleNotification` is delivered via the notification channel with the prompt message and task ID.

## Recurring Cron

Standard 5-field cron expressions: `minute hour day-of-month month day-of-week`.

```json
{
  "cron_expression": "*/5 * * * *",
  "prompt": "Check deployment status",
  "max_iterations": 10
}
```

### Supported Syntax

| Syntax | Example | Description |
|:---|:---|:---|
| `*` | `* * * * *` | Every value |
| `*/N` | `*/5 * * * *` | Every N units |
| `N` | `30 * * * *` | Specific value |
| `N-M` | `0-5 * * * *` | Range |
| `N,M,O` | `0,15,30,45 * * * *` | Multiple values |
| Mixed | `1,5-7 * * * *` | Combinations |

### Field Ranges

| Field | Min | Max |
|:---|:---|:---|
| Minute | 0 | 59 |
| Hour | 0 | 23 |
| Day of Month | 1 | 31 |
| Month | 1 | 12 |
| Day of Week | 0 (Sunday) | 6 (Saturday) |

## Task IDs

- One-shot timers: `sched-<uuid>` (e.g., `sched-a1b2c3d4`)
- Cron jobs: `cron-<uuid>` (e.g., `cron-e5f6g7h8`)

These IDs are compatible with `manage_task` — you can kill a running timer/cron via:

```json
{
  "action": "kill",
  "task_id": "cron-e5f6g7h8"
}
```

## Architecture

### ScheduleManager

The `ScheduleManager` (`internal/tools/schedule.go`) manages all active schedule entries:

- **Goroutine per entry** — each timer/cron runs in its own goroutine with a `context.CancelFunc`
- **Notification channel** — buffered channel (100) delivers `ScheduleNotification` events
- **Thread-safe** — all entry access protected by `sync.RWMutex`
- **Built-in cron parser** — no external dependencies; parses standard 5-field expressions

### Integration with TaskManager

The `ScheduleManager` is embedded in `TaskManager`:
- `TaskManager.ScheduleManager()` — accessor
- `TaskManager.KillTask()` — detects `sched-*` / `cron-*` prefixes and delegates to `ScheduleManager.Cancel()`
- `TaskManager.Shutdown()` — calls `ScheduleManager.Shutdown()` to cancel all running schedules

## Proto Reference

```protobuf
message ActionSchedule {
  int32  duration_seconds = 1;  // One-shot: fire after N seconds (max 900)
  string cron_expression  = 2;  // Cron: standard 5-field expression
  int32  max_iterations   = 3;  // Cron: max triggers (0 = unlimited)
  string prompt           = 4;  // Notification message

  // Result
  string task_id = 10;
  bool   success = 11;
}
```

## Key Files

| File | Purpose |
|:---|:---|
| `internal/tools/schedule.go` | ScheduleManager, cron parser, tool registration |
| `internal/tools/schedule_test.go` | 14 tests (parser, timers, cron, shutdown, integration) |
| `internal/tools/task_manager.go` | ScheduleManager integration |
