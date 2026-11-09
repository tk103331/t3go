// Package events fans server events out to connected clients and keeps a
// short replay window in the store.
package events

import (
	"context"
	"encoding/json"
	"log"
	"sync"
	"time"

	"github.com/tk/t3go/internal/proto"
	"github.com/tk/t3go/internal/store"
)

// Bus publishes events to WebSocket subscribers.
type Bus struct {
	store *store.Store
	mu    sync.Mutex
	subs  map[int]chan proto.Event
	next  int
}

// New returns a bus that records events in store.
func New(s *store.Store) *Bus {
	return &Bus{store: s, subs: map[int]chan proto.Event{}}
}

// Publish records e and sends it to every subscriber. It never blocks on
// a slow client; that client will resynchronize from a snapshot.
func (b *Bus) Publish(ctx context.Context, e proto.Event) proto.Event {
	if e.At.IsZero() {
		e.At = time.Now()
	}
	data := ""
	if e.Data != nil {
		if raw, err := json.Marshal(e.Data); err == nil {
			data = string(raw)
		}
	}
	seq, err := b.store.AppendEvent(ctx, e, data)
	if err != nil {
		log.Printf("t3go: record event: %v", err)
	}
	e.Seq = seq

	b.mu.Lock()
	for _, ch := range b.subs {
		select {
		case ch <- e:
		default:
			// The client's frame loop will see a gap and can refetch.
		}
	}
	b.mu.Unlock()
	return e
}

// Subscribe returns a channel of events and a function that stops it.
func (b *Bus) Subscribe() (<-chan proto.Event, func()) {
	ch := make(chan proto.Event, 512)
	b.mu.Lock()
	id := b.next
	b.next++
	b.subs[id] = ch
	b.mu.Unlock()
	return ch, func() {
		b.mu.Lock()
		if c, ok := b.subs[id]; ok {
			delete(b.subs, id)
			close(c)
		}
		b.mu.Unlock()
	}
}

// Replay returns recorded events after seq.
func (b *Bus) Replay(ctx context.Context, after int64, limit int) ([]proto.Event, error) {
	return b.store.Events(ctx, after, limit)
}

// LastSeq returns the newest recorded event sequence.
func (b *Bus) LastSeq(ctx context.Context) int64 {
	seq, _ := b.store.LastEventSeq(ctx)
	return seq
}

// Prune keeps the newest retain events.
func (b *Bus) Prune(ctx context.Context, retain int) {
	if err := b.store.PruneEvents(ctx, retain); err != nil {
		log.Printf("t3go: prune events: %v", err)
	}
}
