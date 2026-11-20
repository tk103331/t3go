package app

import (
	"fmt"
	"sort"
	"strings"

	"github.com/egoist/mygo/ui"

	"github.com/tk/t3go/internal/app/icons/lucide"
	"github.com/tk/t3go/internal/i18n"
	"github.com/tk/t3go/internal/proto"
)

// mobileEnvironmentMenu picks the environments a phone page aggregates.
func (s *Session) mobileEnvironmentMenu(c *ui.Context, chosen *string, reload func()) {
	label := i18n.T("All environments")
	if *chosen != "" {
		if s.manager != nil {
			label = s.manager.environmentNames()[*chosen]
		}
		if label == "" {
			label = *chosen
		}
	}
	ui.Button(c, "").Label(i18n.T("Environments")).Children(func() {
		ui.Icon(c, lucide.Network()).FontSize(14)
		ui.Text(c, label).SingleLine().MaxWidth(180)
	}).PopoverMenuButton(func(m *ui.Menu) {
		if m.Item(i18n.T("All environments")).Checked(*chosen == "").Chosen() {
			*chosen = ""
			reload()
		}
		if s.manager != nil {
			names := s.manager.environmentNames()
			for _, p := range s.manager.profiles {
				p := p
				if m.Item(names[p.ID]).Checked(*chosen == p.ID).Chosen() {
					*chosen = p.ID
					reload()
				}
			}
		}
	})
}

// mobilePullRequestsPage is the phone's pull request list: an
// environment filter and a state filter above the rows.
func (s *Session) mobilePullRequestsPage(c *ui.Context) {
	t := c.Theme()
	ui.Scroll(c).Fill().Children(func() {
		ui.Column(c).Padding(10, 14, 8, 14).Gap(6).Children(func() {
			s.mobileEnvironmentMenu(c, &s.mobilePREnv, s.loadMobilePullRequests)
			stateLabel := map[string]string{"OPEN": i18n.T("Open"), "CLOSED": "Closed", "MERGED": "Merged", "": i18n.T("All PRs")}[s.mobilePRState]
			ui.Button(c, "").Label(i18n.T("PR state")).Children(func() {
				ui.Text(c, stateLabel).SingleLine()
				ui.Icon(c, lucide.ChevronDown()).FontSize(14)
			}).PopoverMenuButton(func(m *ui.Menu) {
				for _, value := range []string{"OPEN", "CLOSED", "MERGED", ""} {
					value := value
					text := map[string]string{"OPEN": i18n.T("Open"), "CLOSED": "Closed", "MERGED": "Merged", "": i18n.T("All PRs")}[value]
					if m.Item(text).Checked(s.mobilePRState == value).Chosen() {
						s.mobilePRState = value
					}
				}
			})
			ui.Text(c, i18n.T("Pull requests associated with your project threads.")).
				FontSize(12).TextColor(t.TextMuted)
		})
		ui.Column(c).Padding(0, 14, 24, 14).Gap(4).Children(func() {
			if s.mobilePRBusy {
				ui.Row(c).Padding(32).Center().Children(func() {
					ui.Spinner(c).Size(22, 22).Label(i18n.T("Loading pull requests"))
				})
			} else if s.mobilePRErr != "" {
				ui.Column(c).Padding(18).Gap(8).Children(func() {
					text(c, i18n.T("Some projects could not be checked"), 14, t.Text, 600)
					ui.Text(c, s.mobilePRErr).FontSize(12).TextColor(t.Danger)
				})
			}
			count := 0
			for _, row := range s.mobilePRRows {
				row := row
				if state := s.mobilePRState; state != "" && strings.ToUpper(row.PR.State) != state {
					continue
				}
				count++
				rowEl := ui.Column(c).Padding(18).MinHeight(84).Gap(8).Children(func() {
					ui.Text(c, fmt.Sprintf("#%d  %s", row.PR.Number, row.PR.Title)).FontSize(16).FontWeight(500).TextColor(t.Text)
					faint(c, row.Project+" · "+row.Environment, 12)
					ui.Text(c, row.PR.HeadBranch+" → "+row.PR.BaseBranch+" · "+row.PR.State).
						FontSize(12).TextColor(t.TextMuted).SingleLine()
				})
				if rowEl.RowClicked() {
					c.OpenURL(row.PR.URL)
				}
			}
			if count == 0 && !s.mobilePRBusy && s.mobilePRErr == "" {
				ui.Column(c).Padding(28).Gap(12).Center().Children(func() {
					ui.Text(c, i18n.T("No matching pull requests")).FontSize(20).FontWeight(600).TextColor(t.Text)
					ui.Text(c, i18n.T("Pull requests from the selected environments' project threads appear here.")).
						FontSize(14).TextColor(t.TextMuted)
				})
			}
		})
	})
}

// mobileUsagePage is the phone's usage summary: the metric and range
// pickers above the totals, the daily bars, and the providers.
func (s *Session) mobileUsagePage(c *ui.Context) {
	t := c.Theme()
	ui.Scroll(c).Fill().Children(func() {
		ui.Column(c).Padding(4, 16, 24, 16).Gap(10).Children(func() {
			// The metric and range pickers.
			ui.Row(c).Gap(4).Children(func() {
				for _, option := range []struct {
					value, label string
				}{{"cost", i18n.T("Cost")}, {"tokens", i18n.T("Tokens")}, {"limits", i18n.T("Limits")}} {
					option := option
					var b ui.Element
					if s.mobileUsageMetric == option.value {
						b = ui.PrimaryButton(c, option.label)
					} else {
						b = ui.Button(c, option.label)
					}
					b.OnClick(func() { s.mobileUsageMetric = option.value })
				}
			})
			ui.Row(c).Gap(4).Children(func() {
				for _, option := range []struct {
					days  int
					label string
				}{{1, i18n.T("Today")}, {7, "7d"}, {30, "30d"}, {90, "90d"}} {
					option := option
					var b ui.Element
					if s.mobileUsageDays == option.days {
						b = ui.PrimaryButton(c, option.label)
					} else {
						b = ui.Button(c, option.label)
					}
					b.OnClick(func() {
						s.mobileUsageDays = option.days
						s.loadMobileUsage()
					})
				}
			})
			s.mobileEnvironmentMenu(c, &s.mobileUsageEnv, s.loadMobileUsage)

			// The report: every selected environment's usage summed.
			var report proto.UsageReport
			byDay := map[string]proto.UsageDay{}
			byProvider := map[string]proto.UsageProvider{}
			busy, available, unavailable := false, 0, 0
			for _, session := range s.mobileSessions(s.mobileUsageEnv) {
				if !session.settingsConnectionReady() {
					unavailable++
					continue
				}
				if session.usageBusy {
					busy = true
					continue
				}
				if session.usageErr != "" {
					unavailable++
					continue
				}
				if session.usageDays != s.mobileUsageDays {
					continue
				}
				current := session.usageReport
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
				ui.Row(c).Padding(24).Gap(12).Center().Children(func() {
					ui.Spinner(c).Size(24, 24).Label(i18n.T("Loading usage"))
					faint(c, i18n.T("Loading usage…"), 14)
				})
				return
			}
			if unavailable > 0 {
				label := i18n.T("environments")
				if available == 1 {
					label = i18n.T("environment")
				}
				faint(c, i18n.T("Showing %d available %s · %d unavailable", available, label, unavailable), 12)
			}
			if available == 0 {
				ui.Column(c).Gap(8).Children(func() {
					faint(c, i18n.T("Usage is unavailable. Reconnect an environment or try again."), 14)
					ui.Button(c, i18n.T("Try again")).Children(func() {
						ui.Icon(c, lucide.RefreshCw()).FontSize(15)
						ui.Text(c, i18n.T("Try again"))
					}).OnClick(s.loadMobileUsage)
				})
				return
			}
			metric := s.mobileUsageMetric
			title, total, note := i18n.T("Recorded cost"), fmt.Sprintf("$%.2f", report.Totals.Cost),
				i18n.T("Cost reported by runs in the selected environments.")
			if metric == "tokens" {
				title, total, note = i18n.T("Recorded tokens"), humanTokens(report.Totals.Tokens),
					i18n.T("Token usage reported by runs in the selected environments.")
			}
			var dates []string
			var peak float64
			for date, day := range byDay {
				dates = append(dates, date)
				value := day.Cost
				if metric == "tokens" {
					value = float64(day.Tokens)
				}
				if value > peak {
					peak = value
				}
			}
			sort.Strings(dates)
			rangeLabel := i18n.T("Last %d days", s.mobileUsageDays)
			if s.mobileUsageDays == 1 {
				rangeLabel = i18n.T("Today")
			}
			chart := func() {
				if peak == 0 {
					ui.Row(c).Padding(18, 0).Gap(8).Children(func() {
						ui.Icon(c, lucide.ChartColumn()).FontSize(24).TextColor(t.TextMuted)
						faint(c, i18n.T("No ")+metric+i18n.T(" reported in this range."), 13)
					})
					return
				}
				ui.Row(c).AlignItems(ui.End).Gap(3).Height(174).Children(func() {
					for _, date := range dates {
						day := byDay[date]
						value := day.Cost
						if metric == "tokens" {
							value = float64(day.Tokens)
						}
						height := float32(2)
						if peak > 0 {
							height += 166 * float32(value) / float32(peak)
						}
						ui.Box(c).Grow(1).MinWidth(1).Height(height).Radius(3).Background(t.Accent)
					}
				})
			}
			ui.Column(c).Padding(18).Radius(18).Border(1, t.Border).Background(t.Surface).Gap(10).Children(func() {
				ui.Row(c).AlignItems(ui.Center).Gap(8).Children(func() {
					faint(c, title, 13)
					ui.Spacer(c)
					faint(c, rangeLabel, 12)
				})
				ui.Text(c, total).FontSize(36).Bold().TextColor(t.Text)
				ui.Text(c, i18n.T("%d runs · %s tokens", report.Totals.Runs, humanTokens(report.Totals.Tokens))).
					FontSize(12).TextColor(t.TextMuted)
				chart()
				ui.Row(c).Children(func() {
					faint(c, report.Since, 11)
					ui.Spacer(c)
					faint(c, i18n.T("Today"), 11)
				})
				faint(c, note, 12)
			})
			if report.Totals.Runs == 0 {
				faint(c, i18n.T("No usage recorded in this range."), 14)
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
			if len(providers) > 0 {
				faint(c, i18n.T("Providers"), 14)
				ui.Column(c).Radius(18).Border(1, t.Border).Background(t.Surface).Children(func() {
					for _, provider := range providers {
						provider := provider
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
						ui.Column(c).Padding(16).Gap(10).Children(func() {
							ui.Row(c).AlignItems(ui.Center).Gap(8).Children(func() {
								ui.Text(c, providerName(provider.Provider)).FontSize(16).FontWeight(500).TextColor(t.Text)
								ui.Spacer(c)
								ui.Text(c, amount).FontSize(16).TextColor(t.Text)
							})
							ui.Column(c).Height(4).Radius(2).Background(t.Border).Children(func() {
								ui.Box(c).WidthPercent(float32(min(1, share) * 100)).Height(4).Radius(2).Background(t.Accent)
							})
							faint(c, i18n.T("%.1f%% · %s tokens · %d runs", share*100, humanTokens(provider.Tokens), provider.Runs), 12)
						})
					}
				})
			}
			ui.Button(c, i18n.T("Refresh")).Children(func() {
				ui.Icon(c, lucide.RefreshCw()).FontSize(15)
				ui.Text(c, i18n.T("Refresh"))
			}).OnClick(s.loadMobileUsage)
		})
	})
}

// providerName names a provider the usage page shows.
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
