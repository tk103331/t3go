package main

import (
	"context"
	"sort"
	"strings"
	"time"

	"github.com/egoist/asuka/signal"
	"github.com/egoist/asuka/ui"
	"github.com/tk/t3go/internal/client"
	"github.com/tk/t3go/internal/proto"
)

type environmentSnapshot struct {
	ID       string
	Name     string
	Host     string
	Projects []proto.Project
	Threads  []proto.Thread
	Err      string
}

func (a *app) syncActiveSnapshot() {
	if a.activeID == "" {
		return
	}
	profile := a.findProfile(a.activeID)
	name := environmentBaseName(profile)
	if name == "" || name == "T3Go" {
		name = a.envName.Peek()
	}
	host := environmentHost(profile)
	if host == "" {
		host = a.envHost.Peek()
	}
	a.envSnapshots[a.activeID] = environmentSnapshot{
		ID:       a.activeID,
		Name:     name,
		Host:     host,
		Projects: append([]proto.Project(nil), a.projects.Peek()...),
		Threads:  append([]proto.Thread(nil), a.threads.Peek()...),
	}
	a.emitEnvironmentSnapshots()
}

func (a *app) emitEnvironmentSnapshots() {
	out := make([]environmentSnapshot, 0, len(a.profiles))
	seen := map[string]bool{}
	for _, profile := range a.profiles {
		if seen[profile.ID] {
			continue
		}
		seen[profile.ID] = true
		if snapshot, ok := a.envSnapshots[profile.ID]; ok {
			out = append(out, snapshot)
			continue
		}
		out = append(out, environmentSnapshot{
			ID:   profile.ID,
			Name: environmentBaseName(profile),
			Host: environmentHost(profile),
		})
	}
	sort.SliceStable(out, func(i, j int) bool {
		if out[i].ID == a.activeID {
			return true
		}
		if out[j].ID == a.activeID {
			return false
		}
		return strings.ToLower(out[i].Name) < strings.ToLower(out[j].Name)
	})
	a.envSnapV.Set(out)
}

func (a *app) refreshEnvironmentSnapshots() {
	a.syncActiveSnapshot()
	profiles := append([]client.EnvironmentProfile(nil), a.profiles...)
	keep := map[string]bool{}
	for _, profile := range profiles {
		keep[profile.ID] = true
	}
	for id := range a.envSnapshots {
		if !keep[id] {
			delete(a.envSnapshots, id)
		}
	}
	activeID := a.activeID
	for _, profile := range profiles {
		profile := profile
		if profile.ID == activeID {
			continue
		}
		go func() {
			ctx, cancel := context.WithTimeout(a.lifetime, 15*time.Second)
			defer cancel()
			_, state, err := client.ConnectEnvironment(ctx, profile)
			ui.Post(func() {
				if a.findProfile(profile.ID).ID == "" {
					return
				}
				snapshot := environmentSnapshot{
					ID:       profile.ID,
					Name:     environmentBaseName(profile),
					Host:     environmentHost(profile),
					Projects: state.Projects,
					Threads:  state.Threads,
				}
				if snapshot.Name == "" || snapshot.Name == "T3Go" {
					snapshot.Name = strings.TrimSpace(state.Name)
				}
				if snapshot.Name == "" || snapshot.Name == "T3Go" {
					snapshot.Name = profile.Name
				}
				if snapshot.Host == "" {
					snapshot.Host = state.Hostname
				}
				if err != nil {
					snapshot.Err = friendlyError(err)
				}
				if profile.ID == a.activeID {
					a.syncActiveSnapshot()
					return
				}
				a.envSnapshots[profile.ID] = snapshot
				a.emitEnvironmentSnapshots()
			})
		}()
	}
}

func (a *app) openEnvironmentThread(environmentID, threadID string) {
	if environmentID == "" || threadID == "" {
		return
	}
	if environmentID == a.activeID {
		a.selectThread(threadID)
		return
	}
	if a.findProfile(environmentID).ID == "" {
		a.notice.Set("That environment is no longer saved.")
		return
	}
	a.pendingThread = threadID
	a.switchProfile(environmentID)
}

func (a *app) remoteEnvironmentThreads() []environmentSnapshot {
	var out []environmentSnapshot
	for _, snapshot := range a.envSnapV.Get() {
		if snapshot.ID == a.activeID || len(snapshot.Threads) == 0 {
			continue
		}
		out = append(out, snapshot)
	}
	return out
}

func (a *app) remoteThreadActions(environmentID, environmentName string, thread proto.Thread) []ui.Action {
	return []ui.Action{
		{Title: ui.Val("Open in " + environmentName), Icon: ui.IconGlobe,
			OnPress: func() { a.openEnvironmentThread(environmentID, thread.ID) }},
		{Title: ui.Val("Copy title"), Icon: ui.IconCopy, OnPress: func() { ui.Copy(threadTitle(thread)) }},
		{Title: ui.Val("Refresh environments"), Icon: ui.IconRefresh, OnPress: a.refreshEnvironmentSnapshots},
	}
}

func (a *app) environmentSnapshotsSignal() signal.Accessor[[]environmentSnapshot] {
	return a.envSnapV
}

func (a *app) environmentName(id string) string {
	for _, snapshot := range a.envSnapV.Peek() {
		if snapshot.ID == id {
			return snapshot.Name
		}
	}
	if profile := a.findProfile(id); profile.ID != "" {
		return environmentBaseName(profile)
	}
	return id
}

func (a *app) environmentSnapshotFor(id string) (environmentSnapshot, bool) {
	snapshot, ok := a.envSnapshots[id]
	return snapshot, ok
}
