package main

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/oneo-me/nsgo/base"
	"github.com/oneo-me/nsgo/core"
	"github.com/oneo-me/nsgo/icons/lucide"
	"github.com/oneo-me/nsgo/ui"
	"github.com/tk/t3go/internal/client"
	"github.com/tk/t3go/internal/proto"
)

type mobilePullRequest struct {
	EnvironmentID, Environment, Project string
	PR                                  proto.PullRequest
}

func (a *app) loadMobilePullRequests() {
	a.mobilePRSeq++
	seq := a.mobilePRSeq
	type job struct {
		session                             *app
		client                              *client.Client
		thread                              proto.Thread
		project, environment, environmentID string
	}
	var jobs []job
	var warnings []string
	for _, session := range a.mobileSessions(a.mobilePREnv.GetUntracked()) {
		name := session.envName()
		if !session.settingsConnectionReady() {
			warnings = append(warnings, name+" · "+session.connectionLabel())
			continue
		}
		projects := map[string]proto.Project{}
		for _, project := range session.projects.GetUntracked() {
			projects[project.ID] = project
		}
		seen := map[string]bool{}
		for _, thread := range session.threads.GetUntracked() {
			if thread.SideChat {
				continue
			}
			project := projects[thread.ProjectID]
			key := project.Path + "/" + thread.WorktreePath + ":" + thread.Branch
			if seen[key] {
				continue
			}
			seen[key] = true
			jobs = append(jobs, job{session, session.client(), thread, project.Name, name, session.profile.ID})
		}
	}
	a.mobilePRRows.Set(nil)
	a.mobilePRErr.Set(strings.Join(warnings, "\n"))
	a.mobilePRBusy.Set(len(jobs) > 0)
	if len(jobs) == 0 {
		return
	}
	// Bound CLI probes so a large inbox does not start a shell per thread.
	go func() {
		ctx, cancel := context.WithTimeout(a.lifetime, 30*time.Second)
		defer cancel()
		type result struct {
			rows    []mobilePullRequest
			warning string
		}
		results := make(chan result, len(jobs))
		limit := make(chan struct{}, 4)
		for _, task := range jobs {
			go func(j job) {
				select {
				case limit <- struct{}{}:
				case <-ctx.Done():
					results <- result{warning: j.environment + ": " + ctx.Err().Error()}
					return
				}
				defer func() { <-limit }()
				requestCtx, stop := context.WithCancel(ctx)
				stopLifetime := context.AfterFunc(j.session.lifetime, stop)
				defer stopLifetime()
				defer stop()
				prs, detail, err := j.client.PullRequests(requestCtx, j.thread.ID)
				r := result{}
				if err != nil {
					detail = friendlyError(err)
				}
				if detail != "" {
					r.warning = j.environment + " · " + j.project + ": " + detail
				}
				for _, pr := range prs {
					r.rows = append(r.rows, mobilePullRequest{j.environmentID, j.environment, j.project, pr})
				}
				results <- r
			}(task)
		}
		var rows []mobilePullRequest
		seen := map[string]bool{}
		for range jobs {
			r := <-results
			if r.warning != "" {
				warnings = append(warnings, r.warning)
			}
			for _, row := range r.rows {
				key := row.EnvironmentID + "/" + row.PR.URL
				if row.PR.URL == "" {
					key += row.Project + fmt.Sprint(row.PR.Number)
				}
				if !seen[key] {
					seen[key] = true
					rows = append(rows, row)
				}
			}
		}
		a.cx.Post(func() {
			if seq != a.mobilePRSeq {
				return
			}
			a.mobilePRRows.Set(rows)
			a.mobilePRErr.Set(strings.Join(warnings, "\n"))
			a.mobilePRBusy.Set(false)
		})
	}()
}

func (a *app) mobilePullRequests(cx *core.Scope) core.Node {
	colors := ui.ColorsOf(cx)
	return core.View{TestID: "mobile-pr-page", Style: ui.FillRest, Children: []core.Node{
		core.View{Style: core.Style{Padding: core.Symmetric(10, 14), Gap: 6}, Children: []core.Node{
			a.mobileEnvironmentMenu(cx, a.mobilePREnv, "mobile-pr-environment", a.loadMobilePullRequests, false),
			core.Dynamic(func(cx *core.Scope) core.Node {
				state := a.mobilePRState.Get()
				label := map[string]string{"OPEN": "Open", "CLOSED": "Closed", "MERGED": "Merged", "": "All PRs"}[state]
				var items []core.Node
				for _, value := range []string{"OPEN", "CLOSED", "MERGED", ""} {
					value := value
					text := map[string]string{"OPEN": "Open", "CLOSED": "Closed", "MERGED": "Merged", "": "All PRs"}[value]
					items = append(items, ui.MenuItem{Text: text, TestID: "mobile-pr-state-" + strings.ToLower(text),
						OnSelect: func() { a.mobilePRState.Set(value) }})
				}
				return ui.Menu{TestID: "mobile-pr-state", Trigger: ui.Button{Text: label, Icon: lucide.ChevronDown,
					Variant: ui.ButtonGhost, Size: controlSize(cx)}, Children: items}
			}),
			ui.Text("Pull requests associated with your project threads.", 12, ui.Regular, colors.Muted),
		}},
		core.Scroll{Style: ui.FillRest, Children: []core.Node{core.Dynamic(func(cx *core.Scope) core.Node {
			var rows []core.Node
			if a.mobilePRBusy.Get() {
				return core.View{Style: core.Style{Padding: core.All(32)}, Children: []core.Node{ui.Spinner{Size: 22, Label: "Loading pull requests"}}}
			}
			if err := a.mobilePRErr.Get(); err != "" {
				rows = append(rows, core.View{TestID: "mobile-pr-error", Style: core.Style{Padding: core.All(18), Gap: 8}, Children: []core.Node{
					ui.Text("Some projects could not be checked", 14, ui.Semibold, colors.Text),
					ui.Text(err, 12, ui.Regular, colors.Danger),
				}})
			}
			count := 0
			for _, row := range a.mobilePRRows.Get() {
				row := row
				if state := a.mobilePRState.Get(); state != "" && strings.ToUpper(row.PR.State) != state {
					continue
				}
				count++
				rows = append(rows, base.Button{Label: row.PR.Title,
					TestID:  fmt.Sprintf("mobile-pr-%s-%d", row.EnvironmentID, row.PR.Number),
					OnPress: func(core.PressEvent) { a.cx.OpenURL(row.PR.URL, nil) },
					Style:   core.Style{Padding: core.All(18), MinHeight: core.Pt(84), Gap: 8},
					Children: []core.Node{
						ui.Text(fmt.Sprintf("#%d  %s", row.PR.Number, row.PR.Title), 16, ui.Medium, colors.Text),
						ui.Text(row.Project+" · "+row.Environment, 12, ui.Regular, colors.Muted),
						ui.Text(row.PR.HeadBranch+" → "+row.PR.BaseBranch+" · "+row.PR.State, 12, ui.Regular, colors.Muted),
					}})
			}
			if count == 0 {
				rows = append(rows, core.View{TestID: "mobile-pr-empty", Style: core.Style{Padding: core.All(28), Gap: 12,
					AlignItems: core.AlignCenter}, Children: []core.Node{
					ui.Text("No matching pull requests", 20, ui.Semibold, colors.Text),
					ui.Text("Pull requests from the selected environments' project threads appear here.", 14, ui.Regular, colors.Muted),
				}})
			}
			return core.View{Style: core.Style{Gap: 4}, Children: rows}
		})}},
	}}
}
