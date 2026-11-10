package orchestrator

import (
	"context"
	"sync"
	"time"

	"github.com/tk/t3go/internal/id"
	"github.com/tk/t3go/internal/proto"
	"github.com/tk/t3go/internal/store"
)

// approvalManager holds the approvals whose providers wait for a person.
type approvalManager struct {
	mu      sync.Mutex
	pending map[string]*pendingApproval
	order   []string
}

type pendingApproval struct {
	request  proto.ApprovalRequest
	decision chan proto.ApprovalDecision
}

func newApprovalManager() *approvalManager {
	return &approvalManager{pending: map[string]*pendingApproval{}}
}

// RequestApproval publishes an approval and waits for the person or the
// run's end. A run that ends first denies it.
func (s *Service) RequestApproval(ctx context.Context, request proto.ApprovalRequest) proto.ApprovalDecision {
	request.ID = id.New("apr")
	request.CreatedAt = time.Now()
	pending := &pendingApproval{request: request, decision: make(chan proto.ApprovalDecision, 1)}

	s.approvals.mu.Lock()
	s.approvals.pending[request.ID] = pending
	s.approvals.order = append(s.approvals.order, request.ID)
	s.approvals.mu.Unlock()

	s.publish(ctx, proto.Event{Type: proto.EventApproval, ThreadID: request.ThreadID, Data: request})
	select {
	case decision := <-pending.decision:
		return decision
	case <-ctx.Done():
		s.dropApproval(request.ID)
		return proto.ApprovalDecision{Message: "the run ended before the request was answered"}
	}
}

// ResolveApproval answers a pending request.
func (s *Service) ResolveApproval(ctx context.Context, approvalID string, decision proto.ApprovalDecision) error {
	s.approvals.mu.Lock()
	pending := s.approvals.pending[approvalID]
	if pending != nil {
		delete(s.approvals.pending, approvalID)
		s.approvals.order = removeOrdered(s.approvals.order, approvalID)
	}
	s.approvals.mu.Unlock()
	if pending == nil {
		return store.ErrNotFound
	}
	pending.decision <- decision
	s.publish(ctx, proto.Event{Type: proto.EventApprovalResolved, ThreadID: pending.request.ThreadID,
		Data: map[string]any{"id": approvalID, "allow": decision.Allow}})
	return nil
}

// PendingApprovals lists the requests still waiting, oldest first.
func (s *Service) PendingApprovals(threadID string) []proto.ApprovalRequest {
	s.approvals.mu.Lock()
	defer s.approvals.mu.Unlock()
	out := make([]proto.ApprovalRequest, 0, len(s.approvals.pending))
	for _, approvalID := range s.approvals.order {
		pending := s.approvals.pending[approvalID]
		if pending == nil || (threadID != "" && pending.request.ThreadID != threadID) {
			continue
		}
		out = append(out, pending.request)
	}
	return out
}

func (s *Service) dropApproval(approvalID string) {
	s.approvals.mu.Lock()
	delete(s.approvals.pending, approvalID)
	s.approvals.order = removeOrdered(s.approvals.order, approvalID)
	s.approvals.mu.Unlock()
}

func removeOrdered(list []string, id string) []string {
	for i, v := range list {
		if v == id {
			return append(list[:i], list[i+1:]...)
		}
	}
	return list
}
