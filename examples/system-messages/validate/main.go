// Example: Validate the system message notification pipeline.
//
// This is a standalone test that exercises the notification plumbing
// WITHOUT needing an LLM API key. It directly tests:
//
//  1. TaskManager → completion notification → SystemMessage channel
//  2. ScheduleManager → timer fire → SystemMessage channel
//  3. EnrichUserMessage → renders <SYSTEM_MESSAGE> blocks
//
// Usage:
//
//	go run ./examples/system-messages/pipeline_test.go
package main

import (
	"context"
	"fmt"
	"log"
	"log/slog"
	"os"
	"strings"
	"time"

	"github.com/divmora/localharness/internal/engine"
	"github.com/divmora/localharness/internal/tools"
)

func main() {
	logger := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelInfo}))

	fmt.Println("╔════════════════════════════════════════════════════╗")
	fmt.Println("║  System Message Pipeline Validation                ║")
	fmt.Println("╚════════════════════════════════════════════════════╝")
	fmt.Println()

	// --- Step 1: Create unified notification channel ---
	fmt.Println("1. Creating TaskManager + ScheduleManager with unified channel...")
	tm := tools.NewTaskManager(logger, 10)
	sm := tm.ScheduleManager()
	tm.SetNotifyChannel(sm.NotifyChannel())
	notifyCh := sm.Notifications()
	fmt.Println("   ✓ Unified SystemMessage channel wired")
	fmt.Println()

	// --- Step 2: Start a short background task ---
	fmt.Println("2. Starting background task: echo 'hello from background'...")
	taskID, initialOutput, err := tm.StartBackground(
		context.Background(),
		"echo 'hello from background' && sleep 1 && echo 'task done!'",
		".",
		nil, // no env
		500, // wait 500ms for initial output
		nil, // no step update needed for this test
	)
	if err != nil {
		log.Fatalf("   ✗ Failed to start task: %v", err)
	}
	fmt.Printf("   ✓ Task started: %s\n", taskID)
	if initialOutput != "" {
		fmt.Printf("   ✓ Initial output: %s\n", strings.TrimSpace(initialOutput))
	}
	fmt.Println()

	// --- Step 3: Start a one-shot timer ---
	fmt.Println("3. Starting 2-second one-shot timer...")
	schedID := sm.StartOneShot(2*time.Second, "Check build status")
	fmt.Printf("   ✓ Timer started: %s\n", schedID)
	fmt.Println()

	// --- Step 4: Collect notifications ---
	fmt.Println("4. Waiting for notifications (max 5 seconds)...")
	var collected []tools.SystemMessage
	deadline := time.After(5 * time.Second)

	for len(collected) < 2 {
		select {
		case msg := <-notifyCh:
			collected = append(collected, msg)
			fmt.Printf("   ✓ Received [%s] from %s: %s\n",
				msg.Source, msg.TaskID, truncate(msg.Content, 80))
		case <-deadline:
			fmt.Printf("   ⚠ Timed out after collecting %d notification(s)\n", len(collected))
			goto render
		}
	}
render:
	fmt.Println()

	if len(collected) == 0 {
		log.Fatal("   ✗ No notifications received — pipeline is broken!")
	}

	// --- Step 5: Render as <SYSTEM_MESSAGE> blocks ---
	fmt.Println("5. Rendering notifications as enriched user message...")
	fmt.Println()

	var pendingMsgs []string
	for _, msg := range collected {
		pendingMsgs = append(pendingMsgs, msg.FormatForPrompt())
	}

	enrichedParts := engine.EnrichUserMessage("What happened with my tasks?", engine.MessageContextConfig{
		ConversationID:  "test-conv-001",
		Workspaces:      []engine.WorkspaceInfo{{Directory: "."}},
		PendingMessages: pendingMsgs,
	})

	enriched := strings.Join(enrichedParts, "\n")

	fmt.Println("─── Enriched Message (what the LLM sees) ───")
	fmt.Printf("Parts: %d\n", len(enrichedParts))
	for i, part := range enrichedParts {
		fmt.Printf("--- Part %d ---\n%s\n", i+1, part)
	}
	fmt.Println("─────────────────────────────────────────────")
	fmt.Println()

	// --- Verify ---
	if strings.Contains(enriched, "<SYSTEM_MESSAGE>") {
		fmt.Println("✅ SUCCESS: <SYSTEM_MESSAGE> blocks present in enriched message")
	} else {
		fmt.Println("❌ FAILURE: No <SYSTEM_MESSAGE> blocks found!")
		os.Exit(1)
	}

	if strings.Contains(enriched, "<USER_REQUEST>") {
		fmt.Println("✅ SUCCESS: <USER_REQUEST> still present")
	} else {
		fmt.Println("❌ FAILURE: <USER_REQUEST> missing!")
		os.Exit(1)
	}

	// Check ordering: SYSTEM_MESSAGE before USER_REQUEST
	sysIdx := strings.Index(enriched, "<SYSTEM_MESSAGE>")
	userIdx := strings.Index(enriched, "<USER_REQUEST>")
	if sysIdx < userIdx {
		fmt.Println("✅ SUCCESS: <SYSTEM_MESSAGE> appears before <USER_REQUEST>")
	} else {
		fmt.Println("❌ FAILURE: Wrong ordering!")
		os.Exit(1)
	}

	fmt.Println()
	fmt.Println("All validations passed! The notification pipeline is working correctly.")
}

func truncate(s string, maxLen int) string {
	// Take first line only
	if idx := strings.IndexByte(s, '\n'); idx >= 0 {
		s = s[:idx] + "..."
	}
	if len(s) > maxLen {
		return s[:maxLen] + "..."
	}
	return s
}
