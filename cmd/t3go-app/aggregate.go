package main

import (
	"context"
	"fmt"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/egoist/asuka/colors"
	"github.com/egoist/asuka/fonts"
	"github.com/egoist/asuka/signal"
	"github.com/egoist/asuka/ui"
	"github.com/tk/t3go/internal/client"
	"github.com/tk/t3go/internal/proto"
)

type prInboxRow struct {
	EnvironmentID   string
	EnvironmentName string
	ProjectName     string
	ThreadID        string
	ThreadTitle     string
	PR              proto.PullRequest
}

func (a *app) openPullRequestInbox() {
	a.showPRInbox.Set(true)
	a.loadPullRequestInbox()
}

func (a *app) loadPullRequestInbox() {
	a.prInboxSeq++
	seq := a.prInboxSeq
	profiles := append([]client.EnvironmentProfile(nil), a.profiles...)
	a.prInboxBusy.Set(true)
	a.prInboxErr.Set("")
	if len(profiles) == 0 {
		a.prInboxRows.Set(nil)
		a.prInboxBusy.Set(false)
		return
	}
	go func() {
		ctx, cancel := context.WithTimeout(a.lifetime, 60*time.Second)
		defer cancel()
		type result struct {
			rows     []prInboxRow
			warnings []string
		}
		results := make(chan result, len(profiles))
		var wg sync.WaitGroup
		limit := make(chan struct{}, 3)
		for _, profile := range profiles {
			profile := profile
			wg.Add(1)
			go func() {
				defer wg.Done()
				name := environmentBaseName(profile)
				select {
				case limit <- struct{}{}:
				case <-ctx.Done():
					results <- result{warnings: []string{name + ": " + ctx.Err().Error()}}
					return
				}
				defer func() { <-limit }()
				c, state, err := client.ConnectEnvironment(ctx, profile)
				if err != nil {
					results <- result{warnings: []string{name + " · " + friendlyError(err)}}
					return
				}
				projects := map[string]proto.Project{}
				for _, project := range state.Projects {
					projects[project.ID] = project
				}
				seen := map[string]bool{}
				var out result
				for _, thread := range state.Threads {
					if thread.SideChat {
						continue
					}
					project := projects[thread.ProjectID]
					key := project.Path + "\x00" + thread.WorktreePath + "\x00" + thread.Branch
					if seen[key] {
						continue
					}
					seen[key] = true
					prs, detail, err := c.PullRequests(ctx, thread.ID)
					if err != nil {
						out.warnings = append(out.warnings, name+" · "+project.Name+": "+friendlyError(err))
						continue
					}
					if detail != "" && len(prs) == 0 {
						out.warnings = append(out.warnings, name+" · "+project.Name+": "+detail)
						continue
					}
					for _, pr := range prs {
						out.rows = append(out.rows, prInboxRow{
							EnvironmentID: profile.ID, EnvironmentName: name,
							ProjectName: project.Name, ThreadID: thread.ID,
							ThreadTitle: threadTitle(thread), PR: pr,
						})
					}
				}
				results <- out
			}()
		}
		wg.Wait()
		close(results)
		var rows []prInboxRow
		var warnings []string
		seen := map[string]bool{}
		for result := range results {
			warnings = append(warnings, result.warnings...)
			for _, row := range result.rows {
				key := row.EnvironmentID + "/" + row.PR.URL
				if row.PR.URL == "" {
					key = fmt.Sprintf("%s/%s/%d", row.EnvironmentID, row.ProjectName, row.PR.Number)
				}
				if seen[key] {
					continue
				}
				seen[key] = true
				rows = append(rows, row)
			}
		}
		sort.SliceStable(rows, func(i, j int) bool {
			if rows[i].EnvironmentName != rows[j].EnvironmentName {
				return strings.ToLower(rows[i].EnvironmentName) < strings.ToLower(rows[j].EnvironmentName)
			}
			if rows[i].ProjectName != rows[j].ProjectName {
				return strings.ToLower(rows[i].ProjectName) < strings.ToLower(rows[j].ProjectName)
			}
			return rows[i].PR.Number > rows[j].PR.Number
		})
		ui.Post(func() {
			if seq != a.prInboxSeq {
				return
			}
			a.prInboxBusy.Set(false)
			a.prInboxRows.Set(rows)
			a.prInboxErr.Set(strings.Join(warnings, "\n"))
		})
	}()
}

func prInboxEnvironmentActions(a *app) []ui.Action {
	actions := []ui.Action{{
		Title: ui.Val("All environments"), Icon: ui.IconGlobe,
		Checked: signal.Func(func() bool { return a.prInboxEnv.Get() == "" }),
		OnPress: func() { a.prInboxEnv.Set("") },
	}}
	for _, profile := range a.profiles {
		id := profile.ID
		actions = append(actions, ui.Action{
			Title: ui.Val(environmentBaseName(profile)),
			Checked: signal.Func(func() bool {
				return a.prInboxEnv.Get() == id
			}),
			OnPress: func() { a.prInboxEnv.Set(id) },
		})
	}
	return actions
}

func prInboxStateActions(a *app) []ui.Action {
	var actions []ui.Action
	for _, option := range []struct{ value, label string }{
		{"OPEN", "Open"}, {"CLOSED", "Closed"}, {"MERGED", "Merged"}, {"", "All states"},
	} {
		option := option
		actions = append(actions, ui.Action{
			Title:   ui.Val(option.label),
			Checked: signal.Func(func() bool { return a.prInboxState.Get() == option.value }),
			OnPress: func() { a.prInboxState.Set(option.value) },
		})
	}
	return actions
}

func prInboxView(a *app) ui.Node {
	return ui.Dynamic(func() ui.Node { return prInboxContentView(a) })
}

func prInboxContentView(a *app) ui.Node {
	envLabel := "All environments"
	if id := a.prInboxEnv.Get(); id != "" {
		envLabel = a.environmentName(id)
	}
	stateLabel := map[string]string{"OPEN": "Open", "CLOSED": "Closed", "MERGED": "Merged", "": "All states"}[a.prInboxState.Get()]
	var rows []ui.Node
	open, closed, merged := 0, 0, 0
	for _, row := range a.prInboxRows.Get() {
		if env := a.prInboxEnv.Get(); env != "" && row.EnvironmentID != env {
			continue
		}
		state := strings.ToUpper(row.PR.State)
		if filter := strings.ToUpper(a.prInboxState.Get()); filter != "" && state != filter {
			continue
		}
		switch state {
		case "OPEN":
			open++
		case "CLOSED":
			closed++
		case "MERGED":
			merged++
		}
		row := row
		stateColor := colors.Orange
		if state == "MERGED" {
			stateColor = colors.Purple
		} else if state == "CLOSED" {
			stateColor = colors.SecondaryLabel
		}
		rows = append(rows, ui.View(ui.ViewProps{
			OnPress: func() {
				if row.PR.URL != "" {
					ui.OpenURL(row.PR.URL)
				}
			},
			Style: ui.Style{Padding: ui.Symmetric(10, 12), Gap: 3},
		}, ui.Column(ui.Style{Gap: 2},
			ui.Row(ui.Style{Align: ui.AlignCenter, Gap: 6},
				ui.Text(ui.Val(fmt.Sprintf("#%d", row.PR.Number)),
					ui.Style{Font: fonts.Custom("monospace", fonts.Caption), Color: colors.Accent}),
				ui.Text(ui.Val(row.PR.Title), ui.Style{Font: fonts.Footnote.Weighted(fonts.Medium), Grow: 1, Lines: 2}),
				ui.Text(ui.Val(state), ui.Style{Font: fonts.Caption2, Color: stateColor}),
			),
			ui.Text(ui.Val(row.ProjectName+" · "+row.EnvironmentName),
				ui.Style{Font: fonts.Caption2, Color: colors.SecondaryLabel}),
			ui.Text(ui.Val(row.PR.HeadBranch+" → "+row.PR.BaseBranch+" · "+row.ThreadTitle),
				ui.Style{Font: fonts.Caption2, Color: colors.TertiaryLabel, Lines: 1}),
		)))
	}
	if len(rows) == 0 && !a.prInboxBusy.Get() {
		rows = append(rows, emptySurface("No pull requests matched these filters."))
	}
	return ui.SafeArea(ui.Style{Grow: 1},
		ui.Row(ui.Style{Align: ui.AlignCenter, Gap: 8, Padding: ui.Symmetric(8, 12)},
			ui.Text(ui.Val("Pull requests"), ui.Style{Font: fonts.Title3.Weighted(fonts.Bold), Grow: 1}),
			ui.Button(ui.ButtonProps{Icon: ui.IconRefresh, Label: ui.Val("Refresh pull requests"),
				OnPress: a.loadPullRequestInbox}),
			ui.Button(ui.ButtonProps{Icon: ui.IconClose, Label: ui.Val("Close pull requests"),
				OnPress: func() { a.showPRInbox.Set(false) }}),
		),
		ui.Row(ui.Style{Align: ui.AlignCenter, Gap: 6, Padding: ui.Symmetric(0, 12)},
			ui.Button(ui.ButtonProps{Title: ui.Val(envLabel), Icon: ui.IconGlobe,
				Menu: prInboxEnvironmentActions(a)}),
			ui.Button(ui.ButtonProps{Title: ui.Val(stateLabel), Icon: ui.IconList,
				Menu: prInboxStateActions(a)}),
			ui.Text(ui.Val(fmt.Sprintf("%d open · %d merged · %d closed", open, merged, closed)),
				ui.Style{Font: fonts.Caption2, Color: colors.SecondaryLabel, Grow: 1}),
		),
		ui.Show(signal.Func(func() bool { return a.prInboxBusy.Get() }), func() ui.Node {
			return ui.Row(ui.Style{Align: ui.AlignCenter, Gap: 8, Padding: ui.All(12)},
				ui.Activity(), ui.Text(ui.Val("Checking project branches…"),
					ui.Style{Font: fonts.Caption, Color: colors.SecondaryLabel}))
		}),
		ui.Show(signal.Func(func() bool { return a.prInboxErr.Get() != "" }), func() ui.Node {
			return ui.Text(a.prInboxErr, ui.Style{Font: fonts.Caption2, Color: colors.Orange,
				Lines: 5, Padding: ui.Symmetric(2, 12)})
		}),
		ui.Scroll(ui.ScrollProps{Style: ui.Style{Grow: 1}},
			ui.Column(ui.Style{Padding: ui.Edges{Bottom: 18}}, rows...)),
	)
}

func (a *app) loadUsageForEnvironment(days int) {
	a.usageSeq++
	seq := a.usageSeq
	profiles := append([]client.EnvironmentProfile(nil), a.profiles...)
	a.usageBusy.Set(true)
	a.usageErr.Set("")
	if len(profiles) == 0 {
		a.usageReport.Set(proto.UsageReport{})
		a.usageBusy.Set(false)
		return
	}
	go func() {
		ctx, cancel := context.WithTimeout(a.lifetime, 60*time.Second)
		defer cancel()
		type result struct {
			name   string
			report proto.UsageReport
			err    string
		}
		results := make(chan result, len(profiles))
		var wg sync.WaitGroup
		limit := make(chan struct{}, 3)
		for _, profile := range profiles {
			profile := profile
			wg.Add(1)
			go func() {
				defer wg.Done()
				name := environmentBaseName(profile)
				select {
				case limit <- struct{}{}:
				case <-ctx.Done():
					results <- result{name: name, err: ctx.Err().Error()}
					return
				}
				defer func() { <-limit }()
				c, _, err := client.ConnectEnvironment(ctx, profile)
				if err != nil {
					results <- result{name: name, err: friendlyError(err)}
					return
				}
				report, err := c.Usage(ctx, days)
				if err != nil {
					results <- result{name: name, err: friendlyError(err)}
					return
				}
				results <- result{name: name, report: report}
			}()
		}
		wg.Wait()
		close(results)
		var aggregate proto.UsageReport
		byDay := map[string]proto.UsageDay{}
		byProvider := map[string]proto.UsageProvider{}
		var warnings []string
		available := 0
		for result := range results {
			if result.err != "" {
				warnings = append(warnings, result.name+": "+result.err)
				continue
			}
			available++
			current := result.report
			aggregate.Totals.Runs += current.Totals.Runs
			aggregate.Totals.Tokens += current.Totals.Tokens
			aggregate.Totals.Cost += current.Totals.Cost
			if aggregate.Since == "" || (current.Since != "" && current.Since < aggregate.Since) {
				aggregate.Since = current.Since
			}
			for _, day := range current.Days {
				sum := byDay[day.Date]
				sum.Date = day.Date
				sum.Runs += day.Runs
				sum.Tokens += day.Tokens
				sum.Cost += day.Cost
				byDay[day.Date] = sum
			}
			for _, provider := range current.Providers {
				sum := byProvider[provider.Provider]
				sum.Provider = provider.Provider
				sum.Runs += provider.Runs
				sum.Tokens += provider.Tokens
				sum.Cost += provider.Cost
				byProvider[provider.Provider] = sum
			}
		}
		for _, day := range byDay {
			aggregate.Days = append(aggregate.Days, day)
		}
		sort.SliceStable(aggregate.Days, func(i, j int) bool { return aggregate.Days[i].Date < aggregate.Days[j].Date })
		for _, provider := range byProvider {
			aggregate.Providers = append(aggregate.Providers, provider)
		}
		sort.SliceStable(aggregate.Providers, func(i, j int) bool {
			return aggregate.Providers[i].Provider < aggregate.Providers[j].Provider
		})
		aggregate.DaysCount = len(aggregate.Days)
		ui.Post(func() {
			if seq != a.usageSeq {
				return
			}
			a.usageBusy.Set(false)
			a.usageReport.Set(aggregate)
			if len(warnings) > 0 {
				a.usageErr.Set(fmt.Sprintf("%d environments available\n%s", available, strings.Join(warnings, "\n")))
			}
		})
	}()
}

func usageEnvironmentActions(a *app) []ui.Action {
	actions := []ui.Action{
		{Title: ui.Val("This environment"), Icon: ui.IconGlobe,
			Checked: signal.Func(func() bool { return a.usageEnv.Get() == "" }),
			OnPress: func() {
				a.usageEnv.Set("")
				a.loadUsage(a.usageDays.Peek())
			}},
		{Title: ui.Val("All environments"), Icon: ui.IconList,
			Checked: signal.Func(func() bool { return a.usageEnv.Get() == "all" }),
			OnPress: func() {
				a.usageEnv.Set("all")
				a.loadUsage(a.usageDays.Peek())
			}},
	}
	return actions
}
