package server

import (
	"testing"

	pb "github.com/divmora/localharness/gen/go/localharness/v1"
)

func TestEventRingBuffer(t *testing.T) {
	buf := NewEventRingBuffer(3)

	ev1 := &pb.ServerMessage{Payload: &pb.ServerMessage_Error{Error: &pb.ErrorEvent{Message: "msg1"}}}
	ev2 := &pb.ServerMessage{Payload: &pb.ServerMessage_Error{Error: &pb.ErrorEvent{Message: "msg2"}}}
	ev3 := &pb.ServerMessage{Payload: &pb.ServerMessage_Error{Error: &pb.ErrorEvent{Message: "msg3"}}}
	ev4 := &pb.ServerMessage{Payload: &pb.ServerMessage_Error{Error: &pb.ErrorEvent{Message: "msg4"}}}

	buf.Push(ev1)
	buf.Push(ev2)

	all := buf.All()
	if len(all) != 2 {
		t.Fatalf("expected 2 events, got %d", len(all))
	}
	if all[0].GetError().Message != "msg1" || all[1].GetError().Message != "msg2" {
		t.Errorf("unexpected event contents: %+v", all)
	}

	buf.Push(ev3)
	buf.Push(ev4) // Evicts ev1

	allAfterWrap := buf.All()
	if len(allAfterWrap) != 3 {
		t.Fatalf("expected 3 events after overflow, got %d", len(allAfterWrap))
	}
	if allAfterWrap[0].GetError().Message != "msg2" ||
		allAfterWrap[1].GetError().Message != "msg3" ||
		allAfterWrap[2].GetError().Message != "msg4" {
		t.Errorf("unexpected event order after wrap: %+v", allAfterWrap)
	}
}

func TestApprovalQueue(t *testing.T) {
	q := NewApprovalQueue()

	respCh := q.Enqueue("req-1", "replace_file_content", "edit main.go", "--- a\n+++ b")
	if len(q.List()) != 1 {
		t.Fatalf("expected 1 pending approval, got %d", len(q.List()))
	}

	go func() {
		q.Resolve("req-1", &pb.PermissionResponse{
			RequestId: "req-1",
			Approved:  true,
		})
	}()

	resp := <-respCh
	if !resp.Approved {
		t.Errorf("expected approved decision, got %v", resp.Approved)
	}
	if len(q.List()) != 0 {
		t.Errorf("expected 0 pending approvals after resolve, got %d", len(q.List()))
	}
}
