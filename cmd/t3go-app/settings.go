package main

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/egoist/asuka/colors"
	"github.com/egoist/asuka/fonts"
	"github.com/egoist/asuka/signal"
	"github.com/egoist/asuka/ui"
	"github.com/tk/t3go/internal/proto"
)

const (
	settingsGeneral = iota
	settingsEnvironments
	settingsProviders
	settingsUsage
	settingsWorktrees
	settingsShortcuts
	settingsAbout
)

var settingsSectionNames = []string{"General", "Environments", "Providers", "Usage", "Worktrees", "Shortcuts", "About"}

func (a *app) openSettings(section int) {
	if section >= 0 && section < len(settingsSectionNames) {
		a.settingsSection.Set(section)
	}
	s := a.settings.Peek()
	a.settingsDraft.Set(s)
	a.settingsTitle.Set(s.Title)
	a.settingsAccent.Set(s.Accent)
	a.settingsPerm.Set(permissionIndex(proto.PermissionMode(s.DefaultPermission)))
	a.settingsTheme.Set(themeIndex(s.Theme))
	a.settingsErr.Set("")
	options := a.modelOptions()
	index := 0
	for i, option := range options {
		if option.Provider == s.DefaultProvider && option.Model == s.DefaultModel {
			index = i
			break
		}
	}
	a.settingsModel.Set(index)
	a.showSettings.Set(true)
	if section == settingsEnvironments {
		a.refreshEnvironmentSnapshots()
	}
	if section == settingsUsage {
		a.loadUsage(a.usageDays.Peek())
	}
	if section == settingsProviders {
		a.refreshProviders()
	}
}

func themeIndex(theme string) int {
	switch strings.ToLower(strings.TrimSpace(theme)) {
	case "light":
		return 1
	case "dark":
		return 2
	}
	return 0
}

func themeAt(index int) string {
	switch index {
	case 1:
		return "light"
	case 2:
		return "dark"
	}
	return "system"
}

func (a *app) applyAppearance(settings proto.Settings) {
	switch strings.ToLower(strings.TrimSpace(settings.Theme)) {
	case "light":
		ui.SetColorScheme(ui.ColorSchemeLight)
	case "dark":
		ui.SetColorScheme(ui.ColorSchemeDark)
	default:
		ui.SetColorScheme(ui.ColorSchemeSystem)
	}
}

func (a *app) saveSettings() {
	if a.settingsSave.Peek() {
		return
	}
	c := a.client()
	if c == nil {
		a.settingsErr.Set("Connect an environment first.")
		return
	}
	s := a.settingsDraft.Peek()
	s.Title = strings.TrimSpace(a.settingsTitle.Peek())
	s.Accent = strings.TrimSpace(a.settingsAccent.Peek())
	s.DefaultPermission = string(permissionAt(a.settingsPerm.Peek()))
	s.Theme = themeAt(a.settingsTheme.Peek())
	if options := a.modelOptions(); len(options) > 0 {
		index := min(max(a.settingsModel.Peek(), 0), len(options)-1)
		s.DefaultProvider = options[index].Provider
		s.DefaultModel = options[index].Model
	}
	a.settingsSave.Set(true)
	a.settingsErr.Set("")
	go func() {
		ctx, cancel := context.WithTimeout(a.lifetime, 20*time.Second)
		defer cancel()
		saved, err := c.SaveSettings(ctx, s)
		ui.Post(func() {
			if a.client() != c {
				return
			}
			a.settingsSave.Set(false)
			if err != nil {
				a.settingsErr.Set(friendlyError(err))
				return
			}
			a.settings.Set(saved)
			a.applyAppearance(saved)
			if strings.TrimSpace(saved.Title) != "" {
				a.envName.Set(saved.Title)
			}
			a.showSettings.Set(false)
		})
	}()
}

func (a *app) refreshProviders() {
	c := a.client()
	if c == nil || a.providersBusy.Peek() {
		return
	}
	a.providersBusy.Set(true)
	a.providersErr.Set("")
	go func() {
		ctx, cancel := context.WithTimeout(a.lifetime, 30*time.Second)
		defer cancel()
		providers, models, err := c.Providers(ctx)
		ui.Post(func() {
			if a.client() != c {
				return
			}
			a.providersBusy.Set(false)
			if err != nil {
				a.providersErr.Set(friendlyError(err))
				return
			}
			a.providers.Set(providers)
			a.models.Set(models)
		})
	}()
}

func customModelView(a *app) ui.Node {
	providerID := a.customModelFrom.Peek()
	providerName := providerID
	for _, provider := range a.providers.Peek() {
		if provider.ID == providerID {
			providerName = provider.Name
			break
		}
	}
	return ui.SafeArea(ui.Style{Grow: 1},
		ui.Row(ui.Style{Justify: ui.JustifySpaceBetween, Align: ui.AlignCenter, Padding: ui.All(12)},
			ui.Text(ui.Val("Add model"), ui.Style{Font: fonts.Headline}),
			ui.Button(ui.ButtonProps{Icon: ui.IconClose, Label: ui.Val("Close"),
				OnPress: func() { a.showCustomModel.Set(false) }}),
		),
		ui.Scroll(ui.ScrollProps{Style: ui.Style{Grow: 1}},
			ui.Column(ui.Style{Padding: ui.All(16), Gap: 14},
				ui.Text(ui.Val(providerName), ui.Style{Font: fonts.Caption.Weighted(fonts.Semibold), Color: colors.SecondaryLabel}),
				ui.Text(ui.Val("Add a model ID the CLI accepts but does not report. It is saved on this client."),
					ui.Style{Font: fonts.Caption, Color: colors.SecondaryLabel, Lines: 3}),
				ui.Text(ui.Val("Model ID"), ui.Style{Font: fonts.Caption.Weighted(fonts.Semibold), Color: colors.SecondaryLabel}),
				ui.TextField(ui.TextFieldProps{Value: a.customModelID, Placeholder: ui.Val("model-id"),
					Autocapitalize: ui.CapitalizeNone, Autocorrect: ui.AutocorrectNo, ReturnKey: ui.ReturnNext}),
				ui.Text(ui.Val("Display name (optional)"), ui.Style{Font: fonts.Caption.Weighted(fonts.Semibold), Color: colors.SecondaryLabel}),
				ui.TextField(ui.TextFieldProps{Value: a.customModelName, Placeholder: ui.Val("Model name"),
					ReturnKey: ui.ReturnGo, OnSubmit: a.addCustomModel}),
				ui.Show(signal.Func(func() bool { return a.customModelErr.Get() != "" }), func() ui.Node {
					return ui.Text(a.customModelErr, ui.Style{Font: fonts.Caption, Color: colors.Red, Lines: 3})
				}),
				ui.Button(ui.ButtonProps{Title: ui.Val("Add model"), Icon: ui.IconPlus, Variant: ui.ButtonFilled,
					OnPress: a.addCustomModel}),
			)),
	)
}

func (a *app) runProviderCommand(command string) {
	command = strings.TrimSpace(command)
	if command == "" {
		return
	}
	threadID := a.selected.Peek()
	c := a.client()
	if threadID == "" || c == nil {
		ui.Copy(command)
		a.providersErr.Set("Command copied. Open a thread to run it in that environment's terminal.")
		return
	}
	a.providersBusy.Set(true)
	go func() {
		ctx, cancel := context.WithTimeout(a.lifetime, 30*time.Second)
		defer cancel()
		session, err := c.EnsureTerminal(ctx, threadID)
		if err == nil {
			err = c.TerminalInput(ctx, session.ID, command+"\r")
		}
		ui.Post(func() {
			if a.client() != c {
				return
			}
			a.providersBusy.Set(false)
			if err != nil {
				a.providersErr.Set(friendlyError(err))
				return
			}
			a.showSettings.Set(false)
			a.terminalID.Set(session.ID)
			a.openSurface(surfaceTerminal)
		})
	}()
}

func (a *app) loadUsage(days int) {
	if days <= 0 {
		days = 7
	}
	if a.usageEnv.Peek() == "all" {
		a.loadUsageForEnvironment(days)
		return
	}
	c := a.client()
	if c == nil {
		a.usageErr.Set("Connect an environment first.")
		return
	}
	a.usageDays.Set(days)
	a.usageBusy.Set(true)
	a.usageErr.Set("")
	go func() {
		ctx, cancel := context.WithTimeout(a.lifetime, 30*time.Second)
		defer cancel()
		report, err := c.Usage(ctx, days)
		ui.Post(func() {
			if a.client() != c {
				return
			}
			a.usageBusy.Set(false)
			if err != nil {
				a.usageErr.Set(friendlyError(err))
				return
			}
			a.usageReport.Set(report)
		})
	}()
}

func (a *app) loadSearch() {
	query := strings.TrimSpace(a.searchQuery.Peek())
	c := a.client()
	if c == nil || query == "" {
		a.searchHits.Set(nil)
		a.searchErr.Set("")
		return
	}
	a.searchBusy.Set(true)
	a.searchErr.Set("")
	go func() {
		ctx, cancel := context.WithTimeout(a.lifetime, 20*time.Second)
		defer cancel()
		hits, err := c.SearchMessages(ctx, query, 50)
		ui.Post(func() {
			if a.client() != c {
				return
			}
			a.searchBusy.Set(false)
			if err != nil {
				a.searchErr.Set(friendlyError(err))
				return
			}
			a.searchHits.Set(hits)
		})
	}()
}

func (a *app) openSearch() {
	a.searchErr.Set("")
	a.showSearch.Set(true)
}

func (a *app) pickSearchHit(hit proto.MessageHit) {
	a.showSearch.Set(false)
	a.selectThread(hit.ThreadID)
}

func humanTokens(tokens int64) string {
	switch {
	case tokens >= 1_000_000_000:
		return fmt.Sprintf("%.1fB", float64(tokens)/1_000_000_000)
	case tokens >= 1_000_000:
		return fmt.Sprintf("%.1fM", float64(tokens)/1_000_000)
	case tokens >= 1_000:
		return fmt.Sprintf("%.1fK", float64(tokens)/1_000)
	}
	return fmt.Sprintf("%d", tokens)
}

func humanSize(size int64) string {
	switch {
	case size >= 1<<30:
		return fmt.Sprintf("%.1f GB", float64(size)/(1<<30))
	case size >= 1<<20:
		return fmt.Sprintf("%.1f MB", float64(size)/(1<<20))
	case size >= 1<<10:
		return fmt.Sprintf("%.1f KB", float64(size)/(1<<10))
	}
	return fmt.Sprintf("%d B", size)
}
