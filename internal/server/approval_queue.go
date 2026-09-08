package server

import (
	"sync"
	"time"

	pb "github.com/divmora/localharness/gen/go/localharness/v1"
)

// PendingApproval represents a tool call awaiting user authorization in daemon mode.
type PendingApproval struct {
	RequestID   string
	ToolName    string
	Description string
	DiffPreview string
	CreatedAt   time.Time
	ResponseCh  chan *pb.PermissionResponse
}

// ApprovalQueue manages pending tool execution approvals when clients are detached.
type ApprovalQueue struct {
	mu      sync.RWMutex
	pending map[string]*PendingApproval
}

// NewApprovalQueue creates a new pending approvals queue.
func NewApprovalQueue() *ApprovalQueue {
	return &ApprovalQueue{
		pending: make(map[string]*PendingApproval),
	}
}

// Enqueue adds a pending approval request to the queue.
func (q *ApprovalQueue) Enqueue(requestID, toolName, description, diff string) <-chan *pb.PermissionResponse {
	q.mu.Lock()
	defer q.mu.Unlock()

	ch := make(chan *pb.PermissionResponse, 1)
	q.pending[requestID] = &PendingApproval{
		RequestID:   requestID,
		ToolName:    toolName,
		Description: description,
		DiffPreview: diff,
		CreatedAt:   time.Now(),
		ResponseCh:  ch,
	}
	return ch
}

// Resolve fulfills a pending approval with the user's decision.
func (q *ApprovalQueue) Resolve(requestID string, resp *pb.PermissionResponse) bool {
	q.mu.Lock()
	defer q.mu.Unlock()

	req, ok := q.pending[requestID]
	if !ok {
		return false
	}

	delete(q.pending, requestID)
	req.ResponseCh <- resp
	close(req.ResponseCh)
	return true
}

// List returns all currently pending approvals.
func (q *ApprovalQueue) List() []*PendingApproval {
	q.mu.RLock()
	defer q.mu.RUnlock()

	out := make([]*PendingApproval, 0, len(q.pending))
	for _, p := range q.pending {
		out = append(out, p)
	}
	return out
}

// Clear rejects and flushes all pending approvals.
func (q *ApprovalQueue) Clear(reason string) {
	q.mu.Lock()
	defer q.mu.Unlock()

	for reqID, p := range q.pending {
		p.ResponseCh <- &pb.PermissionResponse{
			RequestId:    reqID,
			Approved:     false,
			DenialReason: reason,
		}
		close(p.ResponseCh)
	}
	q.pending = make(map[string]*PendingApproval)
}
