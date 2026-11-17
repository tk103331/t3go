package main

import (
	"fmt"
	"sort"
	"strings"

	"github.com/oneo-me/nsgo/base"
	"github.com/oneo-me/nsgo/core"
	"github.com/oneo-me/nsgo/icons/lucide"
	"github.com/oneo-me/nsgo/ui"
	"github.com/tk/t3go/internal/i18n"
	"github.com/tk/t3go/internal/proto"
)

func (a *app) loadMobileUsage() {
	for _, session := range a.mobileSessions(a.mobileUsageEnv.GetUntracked()) {
		session.loadUsage(a.mobileUsageDays.GetUntracked())
	}
}

func (a *app) mobileUsage(cx *core.Scope) core.Node {
	colors := ui.ColorsOf(cx)
	return core.View{TestID: "mobile-usage-page", Style: ui.FillRest, Children: []core.Node{
		core.View{TestID: "mobile-usage-filters", Style: core.Style{Padding: core.Edges{Top: 4, Right: 16, Bottom: 12, Left: 16}, Gap: 10,
			FlexShrink: 0}, Children: []core.Node{
			mobileSegments(cx, a.mobileUsageMetric.Get, a.mobileUsageMetric.Set, "usage-metric",
				[]string{"cost", "tokens", "limits"}, []string{"Cost", "Tokens", "Limits"}),
			core.Dynamic(func(cx *core.Scope) core.Node {
				var options []core.Node
				for _, option := range []struct {
					days  int
					label string
				}{{1, i18n.T("Today")}, {7, "7d"}, {30, "30d"}, {90, "90d"}} {
					option := option
					options = append(options, mobileSegment(cx, option.label, fmt.Sprintf("settings-usage-%d", option.days),
						a.mobileUsageDays.Get() == option.days, func() { a.mobileUsageDays.Set(option.days); a.loadMobileUsage() }))
				}
				return core.View{Style: core.Style{Direction: core.Row, CornerRadius: 14, Background: colors.Inset(),
					BorderWidth: 1, BorderColor: colors.Border(), Padding: core.All(3)}, Children: options}
			}),
			core.Func(a.mobileUsageEnvironment),
		}},
		core.Scroll{TestID: "mobile-usage-report-scroll", Style: ui.FillRest, Children: []core.Node{
			core.View{Style: core.Style{Padding: core.Edges{Top: 4, Right: 16, Bottom: 24, Left: 16}},
				Children: []core.Node{core.Dynamic(a.mobileUsageReport)}},
		}},
	}}
}

func (a *app) mobileUsageEnvironment(cx *core.Scope) core.Node {
	colors := ui.ColorsOf(cx)
	return core.Dynamic(func(cx *core.Scope) core.Node {
		name := i18n.T("All environments")
		choices := []core.Node{ui.MenuItem{Text: name, TestID: "mobile-usage-environment-all",
			Checked:  func() bool { return a.mobileUsageEnv.Get() == "" },
			OnSelect: func() { a.mobileUsageEnv.Set(""); a.loadMobileUsage() }}}
		for _, session := range a.mobileSessions("") {
			id := session.profile.ID
			if a.mobileUsageEnv.Get() == id {
				name = session.envName()
			}
			choices = append(choices, ui.MenuItem{Text: session.envName(), TestID: "mobile-usage-environment-" + id,
				Checked:  func() bool { return a.mobileUsageEnv.Get() == id },
				OnSelect: func() { a.mobileUsageEnv.Set(id); a.loadMobileUsage() }})
		}
		return ui.Menu{TestID: "mobile-usage-environment", Label: i18n.T("Usage environment"), MaxHeight: 360,
			Trigger: ui.Button{Text: name, Label: i18n.T("Usage environment"), Flex: 1},
			Face: core.View{Style: core.Style{Direction: core.Row, AlignItems: core.AlignCenter,
				MinHeight: core.Pt(44), Gap: 10, Padding: core.Symmetric(8, 12),
				BorderWidth: 1, BorderColor: colors.Border(), CornerRadius: 12}, Children: []core.Node{
				ui.Icon(lucide.Monitor, 18, colors.Muted),
				core.Text{Content: name, MaxLines: 1, TestID: "mobile-usage-environment-name",
					Style:     core.Style{FontSize: 13, FlexGrow: 1, FlexShrink: 1, MinWidth: core.Pt(0)},
					StyleFunc: func() core.Style { return core.Style{Color: colors.Text()} }},
				ui.Icon(lucide.ChevronDown, 16, colors.Muted),
			}}, Children: choices}
	})
}

func mobileSegment(cx *core.Scope, label, id string, selected bool, action func()) core.Node {
	colors := ui.ColorsOf(cx)
	return base.Button{Label: label, TestID: id, Selected: func() bool { return selected },
		OnPress: func(core.PressEvent) { action() },
		Style: core.Style{FlexGrow: 1, FlexShrink: 1, MinWidth: core.Pt(0), Height: core.Pt(44),
			CornerRadius: 11, AlignItems: core.AlignCenter, JustifyContent: core.JustifyCenter},
		StyleFunc: func() core.Style {
			s := core.Style{Focus: ui.FocusRing(cx, -2)}
			if selected {
				s.Background = colors.AccentFill()
			}
			return s
		}, Children: []core.Node{core.Text{Content: label, MaxLines: 1,
			Style: core.Style{FontSize: 14, FontWeight: ui.Medium}, StyleFunc: func() core.Style {
				if selected {
					return core.Style{Color: colors.OnAccent()}
				}
				return core.Style{Color: colors.Muted()}
			}}}}
}

func mobileSegments(cx *core.Scope, value func() string, changed func(string), id string, values, labels []string) core.Node {
	return core.Dynamic(func(cx *core.Scope) core.Node {
		var nodes []core.Node
		for i, option := range values {
			option := option
			nodes = append(nodes, mobileSegment(cx, labels[i], id+"-"+option, value() == option, func() { changed(option) }))
		}
		return core.View{Style: core.Style{Direction: core.Row, CornerRadius: 14, Padding: core.All(3),
			BorderWidth: 1, BorderColor: ui.ColorsOf(cx).Border(), Background: ui.ColorsOf(cx).Inset()}, Children: nodes}
	})
}

// Reports retain their environment ownership. The overview sums only complete
// reports for the requested range, never a previous range's cached numbers.
func (a *app) mobileUsageReport(cx *core.Scope) core.Node {
	colors := ui.ColorsOf(cx)
	metric := a.mobileUsageMetric.Get()
	var children []core.Node
	if metric == "limits" {
		return core.View{TestID: "mobile-usage-limits", Style: core.Style{Padding: core.All(20), Gap: 12,
			CornerRadius: 18, BorderWidth: 1, BorderColor: colors.Border(), Background: colors.Inset()}, Children: []core.Node{
			ui.Icon(lucide.Gauge, 28, colors.Muted),
			ui.Text(i18n.T("Provider limits"), 18, ui.Semibold, colors.Text),
			ui.Text(i18n.T("The connected servers do not report account quotas or reset times. Cost and Tokens show usage recorded by T3Go runs."), 14, ui.Regular, colors.Muted),
		}}
	}
	report := proto.UsageReport{}
	byDay := map[string]proto.UsageDay{}
	byProvider := map[string]proto.UsageProvider{}
	busy, available, unavailable := false, 0, 0
	for _, session := range a.mobileSessions(a.mobileUsageEnv.Get()) {
		name := session.envName()
		if !session.settingsConnectionReady() {
			unavailable++
			children = append(children, ui.Text(name+" · "+session.connectionLabel(), 12, ui.Regular, colors.Danger))
			continue
		}
		if session.usageBusy.Get() {
			busy = true
			continue
		}
		if err := session.usageErr.Get(); err != "" {
			unavailable++
			children = append(children, ui.Text(name+": "+err, 12, ui.Regular, colors.Danger))
			continue
		}
		if session.usageDays.Get() != a.mobileUsageDays.Get() {
			continue
		}
		current := session.usageReport.Get()
		available++
		report.Totals.Runs += current.Totals.Runs
		report.Totals.Tokens += current.Totals.Tokens
		report.Totals.Cost += current.Totals.Cost
		if report.Since == "" || current.Since < report.Since {
			report.Since = current.Since
		}
		for _, day := range current.Days {
			sum := byDay[day.Date]
			sum.Date, sum.Runs, sum.Tokens, sum.Cost = day.Date, sum.Runs+day.Runs, sum.Tokens+day.Tokens, sum.Cost+day.Cost
			byDay[day.Date] = sum
		}
		for _, provider := range current.Providers {
			sum := byProvider[provider.Provider]
			sum.Provider, sum.Runs, sum.Tokens, sum.Cost = provider.Provider, sum.Runs+provider.Runs, sum.Tokens+provider.Tokens, sum.Cost+provider.Cost
			byProvider[provider.Provider] = sum
		}
	}
	if busy {
		children = append(children, core.View{TestID: "mobile-usage-loading", Style: core.Style{Padding: core.All(24),
			Gap: 12, AlignItems: core.AlignCenter}, Children: []core.Node{
			ui.Spinner{Size: 24, Label: i18n.T("Loading usage")}, ui.Text(i18n.T("Loading usage…"), 14, ui.Regular, colors.Muted),
		}})
		// Avoid presenting partial totals as the full amount.
		return core.View{Style: core.Style{Gap: 12}, Children: children}
	}
	if unavailable > 0 {
		label := i18n.T("environments")
		if available == 1 {
			label = i18n.T("environment")
		}
		children = append(children, ui.Text(i18n.T("Showing %d available %s · %d unavailable", available, label, unavailable),
			12, ui.Medium, colors.Muted))
	}
	if available == 0 {
		children = append(children, ui.Text(i18n.T("Usage is unavailable. Reconnect an environment or try again."), 14, ui.Regular, colors.Muted),
			ui.Button{Text: i18n.T("Try again"), Icon: lucide.RefreshCw, TestID: "mobile-usage-refresh", OnPress: a.loadMobileUsage})
		return core.View{Style: core.Style{Gap: 12}, Children: children}
	}
	title, total, note := i18n.T("Recorded cost"), fmt.Sprintf("$%.2f", report.Totals.Cost), i18n.T("Cost reported by runs in the selected environments.")
	if metric == "tokens" {
		title, total, note = i18n.T("Recorded tokens"), humanTokens(report.Totals.Tokens), i18n.T("Token usage reported by runs in the selected environments.")
	}
	var dates []string
	var peak float64
	for date, day := range byDay {
		dates = append(dates, date)
		value := day.Cost
		if metric == "tokens" {
			value = float64(day.Tokens)
		}
		peak = max(peak, value)
	}
	sort.Strings(dates)
	var bars []core.Node
	for _, date := range dates {
		day := byDay[date]
		value := day.Cost
		if metric == "tokens" {
			value = float64(day.Tokens)
		}
		height := 2.0
		if peak > 0 {
			height += 166 * value / peak
		}
		bars = append(bars, core.View{TestID: "mobile-usage-bar-" + date, Label: fmt.Sprintf("%s: %.2f", date, value),
			Style: core.Style{FlexGrow: 1, MinWidth: core.Pt(1), Height: core.Pt(core.Points(height)),
				CornerRadius: 3, Background: colors.Accent()}})
	}
	rangeLabel := i18n.T("Last %d days", a.mobileUsageDays.Get())
	if a.mobileUsageDays.Get() == 1 {
		rangeLabel = i18n.T("Today")
	}
	var chart core.Node = core.View{TestID: "usage-chart", Style: core.Style{Direction: core.Row, AlignItems: core.AlignEnd,
		Gap: 3, Height: core.Pt(174)}, Children: bars}
	if peak == 0 {
		chart = core.View{TestID: "usage-chart-empty", Style: core.Style{Padding: core.Symmetric(18, 0), Gap: 8}, Children: []core.Node{
			ui.Icon(lucide.ChartColumn, 24, colors.Faint),
			ui.Text(i18n.T("No ")+metric+i18n.T(" reported in this range."), 13, ui.Regular, colors.Muted),
		}}
	}
	children = append(children, core.View{TestID: "mobile-usage-summary",
		Style: core.Style{Padding: core.All(18), CornerRadius: 18, BorderWidth: 1, BorderColor: colors.Border(),
			Background: colors.Inset(), Gap: 10}, Children: []core.Node{
			core.View{Style: core.Style{Direction: core.Row, AlignItems: core.AlignCenter, Gap: 8}, Children: []core.Node{
				ui.Text(title, 13, ui.Medium, colors.Muted), ui.Spacer{}, ui.Text(rangeLabel, 12, ui.Regular, colors.Muted),
			}},
			core.Text{Content: total, TestID: "mobile-usage-total", Style: core.Style{FontSize: 36, FontWeight: ui.Bold},
				StyleFunc: func() core.Style { return core.Style{Color: colors.Text()} }},
			ui.Text(i18n.T("%d runs · %s tokens", report.Totals.Runs, humanTokens(report.Totals.Tokens)), 12, ui.Regular, colors.Muted),
			chart,
			core.View{Style: core.Style{Direction: core.Row}, Children: []core.Node{
				ui.Text(report.Since, 11, ui.Regular, colors.Faint), ui.Spacer{}, ui.Text(i18n.T("Today"), 11, ui.Regular, colors.Faint),
			}},
			ui.Text(note, 12, ui.Regular, colors.Muted),
		}})
	if report.Totals.Runs == 0 {
		children = append(children, ui.Text(i18n.T("No usage recorded in this range."), 14, ui.Regular, colors.Muted))
	}
	if len(byProvider) > 0 {
		children = append(children, ui.Text(i18n.T("Providers"), 14, ui.Medium, colors.Muted))
	}
	var providers []proto.UsageProvider
	for _, provider := range byProvider {
		providers = append(providers, provider)
	}
	sort.Slice(providers, func(i, j int) bool {
		if metric == "tokens" && providers[i].Tokens != providers[j].Tokens {
			return providers[i].Tokens > providers[j].Tokens
		}
		if metric == "cost" && providers[i].Cost != providers[j].Cost {
			return providers[i].Cost > providers[j].Cost
		}
		return providers[i].Provider < providers[j].Provider
	})
	var rows []core.Node
	for _, provider := range providers {
		amount, share := fmt.Sprintf("$%.2f", provider.Cost), 0.0
		if metric == "cost" && report.Totals.Cost > 0 {
			share = provider.Cost / report.Totals.Cost
		}
		if metric == "tokens" {
			amount = humanTokens(provider.Tokens)
			if report.Totals.Tokens > 0 {
				share = float64(provider.Tokens) / float64(report.Totals.Tokens)
			}
		}
		rows = append(rows, core.View{TestID: "usage-provider-" + provider.Provider,
			Style: core.Style{Padding: core.All(16), Gap: 10}, Children: []core.Node{
				core.View{Style: core.Style{Direction: core.Row, AlignItems: core.AlignCenter, Gap: 8}, Children: []core.Node{
					ui.Text(providerName(provider.Provider), 16, ui.Medium, colors.Text), ui.Spacer{},
					ui.Text(amount, 16, ui.Regular, colors.Text),
				}},
				core.View{Style: core.Style{Height: core.Pt(4), CornerRadius: 2, Background: colors.Border()}, Children: []core.Node{
					core.View{Style: core.Style{Width: core.Pct(float32(min(1, share) * 100)), Height: core.Pt(4),
						CornerRadius: 2, Background: colors.Accent()}},
				}},
				ui.Text(i18n.T("%.1f%% · %s tokens · %d runs", share*100, humanTokens(provider.Tokens), provider.Runs), 12, ui.Regular, colors.Muted),
			}})
	}
	if len(rows) > 0 {
		children = append(children, core.View{Style: core.Style{CornerRadius: 18, Background: colors.Inset(),
			BorderWidth: 1, BorderColor: colors.Border()}, Children: rows})
	}
	children = append(children, ui.Button{Text: i18n.T("Refresh"), Icon: lucide.RefreshCw, Variant: ui.ButtonGhost,
		TestID: "mobile-usage-refresh", OnPress: a.loadMobileUsage})
	return core.View{Style: core.Style{Gap: 16}, Children: children}
}

func providerName(id string) string {
	switch strings.ToLower(id) {
	case "claude":
		return "Claude Code"
	case "codex":
		return "Codex"
	case "opencode":
		return "OpenCode"
	}
	return id
}
