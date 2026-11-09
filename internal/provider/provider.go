// Package provider runs the agent CLIs T3Go supports.
package provider

import (
	"context"
	"errors"
	"sync"
	"time"

	"github.com/tk/t3go/internal/proto"
)

// Turn is one request sent to a provider.
type Turn struct {
	ThreadID   string
	RunID      string
	CWD        string
	Prompt     string
	Model      string
	Permission proto.PermissionMode
	SessionID  string
}

// Sink receives a turn's output while it runs. Calls are made on the
// provider's goroutine and must not block for long.
type Sink interface {
	// Text receives a piece of the assistant's response.
	Text(delta string)
	// Reasoning receives a piece of the agent's thinking.
	Reasoning(delta string)
	// ToolStart begins a tool activity. detail is the input, such as the
	// command or file path.
	ToolStart(id, name, detail string)
	// ToolFinish completes a tool activity, with its output.
	ToolFinish(id, output string, failed bool)
	// Session reports the provider's conversation id, for a later turn.
	Session(id string)
	// Usage reports the turn's token use.
	Usage(usage proto.Usage)
	// Notice is a non-fatal message worth showing in the transcript.
	Notice(message string)
}

// Provider is one agent CLI adapter.
type Provider interface {
	ID() string
	Name() string
	// Snapshot probes the CLI and returns what it offers.
	Snapshot(ctx context.Context) proto.Provider
}

// Runner is a provider that can execute a turn.
type Runner interface {
	Provider
	Run(ctx context.Context, turn Turn, sink Sink) error
}

// ErrNotInstalled means a provider's CLI is missing.
var ErrNotInstalled = errors.New("provider is not installed")

// Registry holds the supported providers and caches their snapshots.
type Registry struct {
	providers []Provider
	mu        sync.Mutex
	cache     []proto.Provider
	cachedAt  time.Time
}

// NewRegistry returns the built-in providers.
func NewRegistry() *Registry {
	return &Registry{providers: []Provider{
		&Claude{}, &Codex{}, &OpenCode{},
	}}
}

// Register adds or replaces a provider, for a custom adapter or a test's
// fake. Register before the first Snapshot.
func (r *Registry) Register(p Provider) {
	r.mu.Lock()
	defer r.mu.Unlock()
	for i, existing := range r.providers {
		if existing.ID() == p.ID() {
			r.providers[i] = p
			r.cache = nil
			return
		}
	}
	r.providers = append(r.providers, p)
	r.cache = nil
}

// Get returns a runner by id.
func (r *Registry) Get(id string) (Runner, bool) {
	for _, p := range r.providers {
		if p.ID() == id {
			run, ok := p.(Runner)
			return run, ok
		}
	}
	return nil, false
}

// Providers returns the supported providers' ids.
func (r *Registry) Providers() []string {
	out := make([]string, len(r.providers))
	for i, p := range r.providers {
		out[i] = p.ID()
	}
	return out
}

// Snapshot probes every provider, reusing a recent result.
func (r *Registry) Snapshot(ctx context.Context) []proto.Provider {
	r.mu.Lock()
	if time.Since(r.cachedAt) < 20*time.Second && r.cache != nil {
		out := append([]proto.Provider(nil), r.cache...)
		r.mu.Unlock()
		return out
	}
	r.mu.Unlock()

	ctx, cancel := context.WithTimeout(ctx, 20*time.Second)
	defer cancel()
	out := make([]proto.Provider, len(r.providers))
	var wg sync.WaitGroup
	for i, p := range r.providers {
		wg.Add(1)
		go func(i int, p Provider) {
			defer wg.Done()
			out[i] = p.Snapshot(ctx)
		}(i, p)
	}
	wg.Wait()

	r.mu.Lock()
	r.cache, r.cachedAt = append([]proto.Provider(nil), out...), time.Now()
	r.mu.Unlock()
	return out
}

// Invalidate clears a cached snapshot, after a model change for example.
func (r *Registry) Invalidate() {
	r.mu.Lock()
	r.cache, r.cachedAt = nil, time.Time{}
	r.mu.Unlock()
}

// Models flattens the snapshots' model lists.
func (r *Registry) Models(ctx context.Context) []proto.Model {
	var out []proto.Model
	for _, p := range r.Snapshot(ctx) {
		if p.Installed {
			out = append(out, p.Models...)
		}
	}
	return out
}

// FindModel returns a provider's model.
func FindModel(providers []proto.Provider, providerID, modelID string) (proto.Model, bool) {
	for _, p := range providers {
		if p.ID != providerID {
			continue
		}
		for _, m := range p.Models {
			if m.ID == modelID {
				return m, true
			}
		}
		// A custom model id is allowed even when it is not listed.
		if modelID != "" {
			return proto.Model{ID: modelID, Name: modelID, Provider: p.ID}, true
		}
	}
	return proto.Model{}, false
}
