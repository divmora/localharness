package tui

import (
	"strings"
	"testing"
	"time"

	"github.com/charmbracelet/bubbles/spinner"
)

func TestChatHistory_AddSideQuestion(t *testing.T) {
	h := NewChatHistory()
	h.AddSideQuestion("what port is the daemon?", "The daemon runs on 8080 by default.")

	if len(h.items) != 1 {
		t.Fatalf("expected 1 item, got %d", len(h.items))
	}
	if h.items[0].Type != ChatItemSideQuestion {
		t.Errorf("expected ChatItemSideQuestion, got %v", h.items[0].Type)
	}

	spin := spinner.New()
	rendered := h.RenderView(spin, 80)
	if !strings.Contains(rendered, "Side Question (/btw)") {
		t.Errorf("expected Side Question header in rendered view: %s", rendered)
	}
	if !strings.Contains(rendered, "what port is the daemon?") {
		t.Errorf("expected question text in rendered view: %s", rendered)
	}
	if !strings.Contains(rendered, "runs on 8080") {
		t.Errorf("expected answer text in rendered view: %s", rendered)
	}
}

func TestAskSideQuestionCmd_HistoryFallback(t *testing.T) {
	h := NewChatHistory()
	h.items = append(h.items, ChatItem{
		Type:      ChatItemAssistant,
		Content:   "Database configuration initialized using PostgreSQL on port 5432.",
		Timestamp: time.Now(),
	})

	cmd := AskSideQuestionCmd("PostgreSQL", h, "test-model")
	msg := cmd()

	res, ok := msg.(SideQuestionResultMsg)
	if !ok {
		t.Fatalf("expected SideQuestionResultMsg, got %T", msg)
	}
	if res.Question != "PostgreSQL" {
		t.Errorf("expected question PostgreSQL, got %s", res.Question)
	}
	if !strings.Contains(res.Answer, "5432") {
		t.Errorf("expected answer to match session history: %s", res.Answer)
	}
}
