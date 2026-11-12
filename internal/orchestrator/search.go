package orchestrator

import (
	"context"

	"github.com/tk/t3go/internal/proto"
)

// SearchMessages finds messages in visible threads.
func (s *Service) SearchMessages(ctx context.Context, query string, limit int) ([]proto.MessageHit, error) {
	return s.Store.SearchMessageHits(ctx, query, limit)
}
