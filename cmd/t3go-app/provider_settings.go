package main

import (
	"context"
	"fmt"
	"sort"
	"strings"

	"github.com/oneo-me/nsgo/base"
	"github.com/oneo-me/nsgo/core"
	"github.com/oneo-me/nsgo/icons/lucide"
	"github.com/oneo-me/nsgo/ui"
	"github.com/tk/t3go/internal/client"
	"github.com/tk/t3go/internal/i18n"
	"github.com/tk/t3go/internal/proto"
)

func commandArgsText(args []string) string {
	return strings.Join(args, "\n")
}

// commandArgs parses one literal argument per line. Unlike a shell string,
// spaces and quotes remain part of the argument.
func commandArgs(text string) []string {
	var args []string
	for _, line := range strings.Split(strings.ReplaceAll(text, "\r\n", "\n"), "\n") {
		if value := strings.TrimSpace(line); value != "" {
			args = append(args, value)
		}
	}
	return args
}

func providerTextKeyACP(id, field string) string {
	return "acp:" + id + ":" + field
}

func (a *app) loadProviderTextDraft(settings proto.Settings) {
	values := map[string]string{}
	for _, configured := range settings.ACPProviders {
		values[providerTextKeyACP(configured.ID, "name")] = configured.Name
		values[providerTextKeyACP(configured.ID, "path")] = configured.CommandPath
		values[providerTextKeyACP(configured.ID, "args")] = commandArgsText(configured.CommandArgs)
	}
	a.providerTextDraft.Set(values)
}

func (a *app) providerTextValue(key, fallback string) string {
	if value, ok := a.providerTextDraft.Get()[key]; ok {
		return value
	}
	return fallback
}

func (a *app) setProviderTextValue(key, value string) {
	next := make(map[string]string, len(a.providerTextDraft.GetUntracked())+1)
	for existingKey, existingValue := range a.providerTextDraft.GetUntracked() {
		next[existingKey] = existingValue
	}
	next[key] = value
	a.providerTextDraft.Set(next)
}

func (a *app) syncProviderTextDraft() {
	a.settingsDraft.Update(func(settings proto.Settings) proto.Settings {
		for index := range settings.ACPProviders {
			id := settings.ACPProviders[index].ID
			if value, ok := a.providerTextDraft.GetUntracked()[providerTextKeyACP(id, "name")]; ok {
				settings.ACPProviders[index].Name = strings.TrimSpace(value)
			}
			if value, ok := a.providerTextDraft.GetUntracked()[providerTextKeyACP(id, "path")]; ok {
				settings.ACPProviders[index].CommandPath = strings.TrimSpace(value)
			}
			if value, ok := a.providerTextDraft.GetUntracked()[providerTextKeyACP(id, "args")]; ok {
				settings.ACPProviders[index].CommandArgs = commandArgs(value)
			}
		}
		return settings
	})
}

func (a *app) updateProviderSettings(update func(*proto.Settings)) {
	a.settingsDraft.Update(func(settings proto.Settings) proto.Settings {
		update(&settings)
		return settings
	})
}

func providerSettingsSource(source string) string {
	switch source {
	case "registry":
		return i18n.T("ACP Registry")
	case "local":
		return i18n.T("Local ACP")
	default:
		return source
	}
}

func settingsSwitch(cx *core.Scope, label, testID string, checked func() bool, onChange func(bool)) core.Node {
	look := ui.NewSwitchLook(cx, checked, ui.SizeSmall)
	return base.Switch{Checked: checked, OnChange: onChange, Label: label, TestID: testID,
		Style:          core.Style{Margin: core.All(-8), Padding: core.All(8)},
		TrackStyleFunc: look.Track, ThumbStyleFunc: look.Thumb}
}

func providerSettingsCard(cx *core.Scope, testID string, children ...core.Node) core.Node {
	colors := ui.ColorsOf(cx)
	return core.View{TestID: testID, Style: core.Style{Direction: core.Column, Gap: 7,
		Padding: core.Symmetric(10, 12), Background: colors.Inset(), CornerRadius: 9,
		BorderWidth: 1, BorderColor: colors.Border()}, Children: children}
}

func providerSettingsHeader(cx *core.Scope, name, detail, testID string, enabled func() bool,
	onEnabled func(bool), deleteLabel, deleteTestID string, onDelete func()) core.Node {
	colors := ui.ColorsOf(cx)
	var trailing []core.Node
	if onDelete != nil {
		trailing = append(trailing, ui.Button{Text: i18n.T("Remove"), Icon: lucide.Trash, Variant: ui.ButtonGhost,
			Size: controlSize(cx), TestID: deleteTestID, OnPress: onDelete})
	}
	trailing = append(trailing, settingsSwitch(cx, i18n.T("Enable ")+name, testID+"-enabled", enabled, onEnabled))
	return core.View{TestID: testID + "-header", Style: core.Style{Direction: core.Row, AlignItems: core.AlignCenter,
		Wrap: core.Wrap, Gap: 8}, Children: []core.Node{
		core.View{Style: core.Style{Direction: core.Column, Gap: 1, FlexGrow: 1, FlexShrink: 1, MinWidth: core.Pt(140)},
			Children: []core.Node{
				core.Text{Content: name, MaxLines: 1, Style: core.Style{FontSize: 13, FontWeight: ui.Semibold},
					StyleFunc: func() core.Style { return core.Style{Color: colors.Text()} }},
				core.Text{Content: detail, MaxLines: 2, Style: core.Style{FontSize: 10.5},
					StyleFunc: func() core.Style { return core.Style{Color: colors.Faint()} }},
			}},
		core.View{Style: core.Style{Direction: core.Row, AlignItems: core.AlignCenter, Gap: 4}, Children: trailing},
	}}
}

func (a *app) acpProviderSettingsCards(cx *core.Scope) []core.Node {
	colors := ui.ColorsOf(cx)
	settings := a.settingsDraft.Get()
	cards := make([]core.Node, 0, len(settings.ACPProviders))
	for index, configured := range settings.ACPProviders {
		index, configured := index, configured
		detail := providerSettingsSource(configured.Source)
		if configured.Version != "" {
			detail += " · " + configured.Version
		}
		if configured.Distribution != "" && configured.Distribution != "auto" {
			detail += " · " + configured.Distribution
		}
		body := []core.Node{
			providerSettingsHeader(cx, configured.Name, detail, "settings-acp-"+configured.ID,
				func() bool {
					providers := a.settingsDraft.Get().ACPProviders
					return index < len(providers) && providers[index].Enabled
				},
				func(enabled bool) {
					a.updateProviderSettings(func(settings *proto.Settings) {
						if index < len(settings.ACPProviders) {
							settings.ACPProviders[index].Enabled = enabled
						}
					})
				}, i18n.T("Remove"), "settings-acp-remove-"+configured.ID, func() {
					a.updateProviderSettings(func(settings *proto.Settings) {
						if index < len(settings.ACPProviders) {
							settings.ACPProviders = append(settings.ACPProviders[:index], settings.ACPProviders[index+1:]...)
						}
					})
				}),
			mobileInput{Scope: cx, Input: base.Input{Value: func() string {
				return a.providerTextValue(providerTextKeyACP(configured.ID, "name"), configured.Name)
			}, OnChange: func(name string) {
				a.setProviderTextValue(providerTextKeyACP(configured.ID, "name"), name)
			}, Placeholder: i18n.T("Provider name"), Label: i18n.T("ACP provider name")},
				TestID: "settings-acp-name-" + configured.ID},
		}
		if configured.Source == "local" {
			body = append(body,
				mobileInput{Scope: cx, Input: base.Input{Value: func() string {
					return a.providerTextValue(providerTextKeyACP(configured.ID, "path"), configured.CommandPath)
				}, OnChange: func(path string) {
					a.setProviderTextValue(providerTextKeyACP(configured.ID, "path"), path)
				}, Placeholder: i18n.T("/absolute/path/to/agent or executable on PATH"), Label: i18n.T("ACP executable"),
					NoAutoCorrect: true, AutoCapitalize: core.AutoCapitalizeNone},
					TestID: "settings-acp-path-" + configured.ID},
			)
		} else {
			body = append(body, core.Text{Content: i18n.T("Registry agent %s · %s distribution",
				configured.AgentID, configured.Distribution), MaxLines: 2,
				Style:     core.Style{FontSize: 10.5, FontFamily: "Menlo, monospace"},
				StyleFunc: func() core.Style { return core.Style{Color: colors.Faint()} }})
		}
		body = append(body,
			mobileInput{Scope: cx, Input: base.Input{Value: func() string {
				return a.providerTextValue(providerTextKeyACP(configured.ID, "args"), commandArgsText(configured.CommandArgs))
			}, OnChange: func(value string) {
				a.setProviderTextValue(providerTextKeyACP(configured.ID, "args"), value)
			}, Placeholder: i18n.T("One literal argument per line"), Label: i18n.T("ACP arguments"),
				Multiline: true, NoAutoCorrect: true, AutoCapitalize: core.AutoCapitalizeNone},
				TestID: "settings-acp-args-" + configured.ID},
			core.Text{Content: i18n.T("Arguments are passed directly to the process; shell expansion is never applied."),
				MaxLines: 2, Style: core.Style{FontSize: 10.5},
				StyleFunc: func() core.Style { return core.Style{Color: colors.Faint()} }},
		)
		cards = append(cards, providerSettingsCard(cx, "settings-acp-provider-"+configured.ID, body...))
	}
	return cards
}

func preferredRegistryDistribution(agent proto.ACPRegistryAgent) string {
	for _, distribution := range agent.Distributions {
		if distribution == "npx" || distribution == "uvx" || distribution == "binary" {
			return distribution
		}
	}
	return ""
}

func (a *app) openProviderAdd() {
	a.providerAddOpen.Set(true)
	a.providerManageErr.Set("")
	a.searchProviderCatalog(false)
}

func (a *app) searchProviderCatalog(refresh bool) {
	if !a.settingsConnectionReady() {
		a.providerCatalogErr.Set(i18n.T("Connect this environment to search the ACP Registry."))
		return
	}
	a.providerSeq++
	seq := a.providerSeq
	query := a.providerSearchQuery.GetUntracked()
	a.providerCatalogBusy.Set(true)
	a.providerCatalogErr.Set("")
	a.call(func(ctx context.Context, c *client.Client) error {
		agents, err := c.ACPRegistry(ctx, query, refresh)
		if err != nil {
			return err
		}
		a.cx.Post(func() {
			if seq != a.providerSeq {
				return
			}
			sort.SliceStable(agents, func(i, j int) bool {
				return strings.ToLower(agents[i].Name) < strings.ToLower(agents[j].Name)
			})
			a.providerCatalog.Set(agents)
			a.providerCatalogBusy.Set(false)
		})
		return nil
	}, func(err error) {
		if seq != a.providerSeq {
			return
		}
		a.providerCatalogBusy.Set(false)
		if err != nil {
			a.providerCatalogErr.Set(friendlyError(err))
		}
	})
}

func acpProviderID(name, agentID string, existing []proto.ACPProviderSettings) string {
	baseID := strings.ToLower(strings.TrimSpace(agentID))
	if baseID == "" {
		baseID = strings.ToLower(strings.TrimSpace(name))
	}
	var cleaned strings.Builder
	for _, r := range baseID {
		switch {
		case r >= 'a' && r <= 'z', r >= '0' && r <= '9':
			cleaned.WriteRune(r)
		case r == '-', r == '_':
			cleaned.WriteRune('-')
		default:
			if cleaned.Len() > 0 && !strings.HasSuffix(cleaned.String(), "-") {
				cleaned.WriteByte('-')
			}
		}
	}
	id := "acp-" + strings.Trim(cleaned.String(), "-")
	if id == "acp-" {
		id = "acp-provider"
	}
	candidate := id
	for n := 2; ; n++ {
		used := false
		for _, configured := range existing {
			if configured.ID == candidate {
				used = true
				break
			}
		}
		if !used {
			return candidate
		}
		candidate = fmt.Sprintf("%s-%d", id, n)
	}
}

func (a *app) addRegistryProvider(agent proto.ACPRegistryAgent, distribution string) {
	if a.providerPrepareID.GetUntracked() != "" {
		return
	}
	settings := a.settingsDraft.GetUntracked()
	for _, configured := range settings.ACPProviders {
		if configured.Source == "registry" && configured.AgentID == agent.ID {
			a.providerManageErr.Set(agent.Name + i18n.T(" is already configured for this environment."))
			return
		}
	}
	add := func() {
		a.updateProviderSettings(func(settings *proto.Settings) {
			settings.ACPProviders = append(settings.ACPProviders, proto.ACPProviderSettings{
				ID: acpProviderID(agent.Name, agent.ID, settings.ACPProviders), Name: agent.Name,
				Enabled: true, Source: "registry", AgentID: agent.ID, Version: agent.Version,
				Distribution: distribution,
			})
		})
		a.providerAddOpen.Set(false)
	}
	if distribution != "binary" {
		add()
		return
	}
	a.providerPrepareID.Set(agent.ID)
	a.providerManageErr.Set("")
	a.call(func(ctx context.Context, c *client.Client) error {
		_, err := c.PrepareACPRegistry(ctx, agent.ID)
		return err
	}, func(err error) {
		a.providerPrepareID.Set("")
		if err != nil {
			a.providerManageErr.Set(friendlyError(err))
			return
		}
		add()
	})
}

func (a *app) addLocalACPProvider() {
	name := strings.TrimSpace(a.providerLocalName.GetUntracked())
	path := strings.TrimSpace(a.providerLocalPath.GetUntracked())
	if name == "" {
		a.providerManageErr.Set(i18n.T("Enter a provider name."))
		return
	}
	if path == "" {
		a.providerManageErr.Set(i18n.T("Enter the local ACP executable."))
		return
	}
	a.updateProviderSettings(func(settings *proto.Settings) {
		settings.ACPProviders = append(settings.ACPProviders, proto.ACPProviderSettings{
			ID: acpProviderID(name, "", settings.ACPProviders), Name: name, Enabled: true, Source: "local",
			CommandPath: path, CommandArgs: commandArgs(a.providerLocalArgs.GetUntracked()),
		})
	})
	a.providerLocalName.Set("")
	a.providerLocalPath.Set("")
	a.providerLocalArgs.Set("")
	a.providerAddOpen.Set(false)
}

func (a *app) providerRegistryPanel(cx *core.Scope) core.Node {
	colors := ui.ColorsOf(cx)
	var resultCards []core.Node
	for _, agent := range a.providerCatalog.Get() {
		agent := agent
		var actions []core.Node
		for _, distribution := range agent.Distributions {
			if distribution != "binary" && distribution != "npx" && distribution != "uvx" {
				continue
			}
			distribution := distribution
			label := i18n.T("Add via ") + distribution
			icon := lucide.Plus
			if distribution == "binary" {
				label, icon = i18n.T("Install"), lucide.Download
			}
			actions = append(actions, ui.Button{Text: label, Icon: icon, Variant: ui.ButtonSecondary,
				Size: controlSize(cx), TestID: "settings-acp-add-" + agent.ID + "-" + distribution,
				Disabled: func() bool { return a.providerPrepareID.Get() != "" },
				OnPress:  func() { a.addRegistryProvider(agent, distribution) }})
		}
		detail := agent.Description
		if detail == "" {
			detail = agent.ID
		}
		resultCards = append(resultCards, core.View{TestID: "settings-acp-registry-" + agent.ID,
			Style: core.Style{Direction: core.Column, Gap: 6, Padding: core.Symmetric(8, 10),
				BorderWidth: 1, BorderColor: colors.Border(), CornerRadius: 7}, Children: []core.Node{
				core.View{Style: core.Style{Direction: core.Row, AlignItems: core.AlignCenter, Gap: 8}, Children: []core.Node{
					core.View{Style: core.Style{Direction: core.Column, Gap: 1, FlexGrow: 1, FlexShrink: 1, MinWidth: core.Pt(0)},
						Children: []core.Node{
							core.Text{Content: agent.Name, MaxLines: 1, Style: core.Style{FontSize: 12.5, FontWeight: ui.Semibold},
								StyleFunc: func() core.Style { return core.Style{Color: colors.Text()} }},
							core.Text{Content: agent.Version + " · " + strings.Join(agent.Distributions, ", "), MaxLines: 1,
								Style:     core.Style{FontSize: 10, FontFamily: "Menlo, monospace"},
								StyleFunc: func() core.Style { return core.Style{Color: colors.Faint()} }},
						}},
				}},
				core.Text{Content: detail, MaxLines: 3, Style: core.Style{FontSize: 10.5, LineHeight: 15},
					StyleFunc: func() core.Style { return core.Style{Color: colors.Muted()} }},
				core.View{Style: core.Style{Direction: core.Row, Wrap: core.Wrap, Gap: 4}, Children: actions},
			}})
	}
	if a.providerCatalogBusy.Get() {
		resultCards = []core.Node{ui.Spinner{Size: 14, Label: i18n.T("Loading ACP Registry")}}
	} else if len(resultCards) == 0 && a.providerCatalogErr.Get() == "" {
		resultCards = []core.Node{ui.Text(i18n.T("No compatible registry agents found."), 11.5, ui.Regular, colors.Faint)}
	}
	return core.View{Style: core.Style{Direction: core.Column, Gap: 7}, Children: []core.Node{
		core.View{Style: core.Style{Direction: core.Row, AlignItems: core.AlignCenter, Gap: 6}, Children: []core.Node{
			core.View{Style: core.Style{FlexGrow: 1, FlexShrink: 1, MinWidth: core.Pt(0)}, Children: []core.Node{
				mobileInput{Scope: cx, Input: base.Input{Value: a.providerSearchQuery.Get, OnChange: a.providerSearchQuery.Set,
					Placeholder: i18n.T("Search agents"), Label: i18n.T("Search ACP Registry"), NoAutoCorrect: true,
					SubmitBehavior: core.SubmitBehaviorSubmit, OnSubmit: func(string) { a.searchProviderCatalog(false) }},
					TestID: "settings-acp-search"},
			}},
			ui.Button{Text: i18n.T("Search"), Icon: lucide.Search, Variant: ui.ButtonSecondary, Size: controlSize(cx),
				TestID: "settings-acp-search-button", Disabled: a.providerCatalogBusy.Get,
				OnPress: func() { a.searchProviderCatalog(true) }},
		}},
		core.View{Style: core.Style{Direction: core.Column, Gap: 5}, Children: resultCards},
	}}
}

func (a *app) providerLocalPanel(cx *core.Scope) core.Node {
	colors := ui.ColorsOf(cx)
	return core.View{Style: core.Style{Direction: core.Column, Gap: 7}, Children: []core.Node{
		mobileInput{Scope: cx, Input: base.Input{Value: a.providerLocalName.Get, OnChange: a.providerLocalName.Set,
			Placeholder: i18n.T("Provider name"), Label: i18n.T("Local ACP provider name")}, TestID: "settings-acp-local-name"},
		mobileInput{Scope: cx, Input: base.Input{Value: a.providerLocalPath.Get, OnChange: a.providerLocalPath.Set,
			Placeholder: i18n.T("/absolute/path/to/agent or executable on PATH"), Label: i18n.T("Local ACP executable"),
			NoAutoCorrect: true, AutoCapitalize: core.AutoCapitalizeNone}, TestID: "settings-acp-local-path"},
		mobileInput{Scope: cx, Input: base.Input{Value: a.providerLocalArgs.Get, OnChange: a.providerLocalArgs.Set,
			Placeholder: i18n.T("One literal argument per line"), Label: i18n.T("Local ACP arguments"), Multiline: true,
			NoAutoCorrect: true, AutoCapitalize: core.AutoCapitalizeNone}, TestID: "settings-acp-local-args"},
		core.Text{Content: i18n.T("The executable is started directly without a shell. Relative executable names are resolved on the environment's PATH."),
			MaxLines: 3, Style: core.Style{FontSize: 10.5, LineHeight: 15},
			StyleFunc: func() core.Style { return core.Style{Color: colors.Faint()} }},
		ui.Button{Text: i18n.T("Add local ACP provider"), Icon: lucide.Plus, Variant: ui.ButtonPrimary,
			Size: controlSize(cx), TestID: "settings-acp-add-local", OnPress: a.addLocalACPProvider},
	}}
}

func (a *app) providerAddPanel(cx *core.Scope) core.Node {
	colors := ui.ColorsOf(cx)
	if !a.providerAddOpen.Get() {
		return ui.Button{Text: i18n.T("Add ACP provider"), Icon: lucide.Plus, Variant: ui.ButtonSecondary,
			Size: controlSize(cx), TestID: "settings-provider-add", OnPress: a.openProviderAdd}
	}
	var body core.Node
	if a.providerAddMode.Get() == "local" {
		body = a.providerLocalPanel(cx)
	} else {
		body = a.providerRegistryPanel(cx)
	}
	return providerSettingsCard(cx, "settings-provider-add-panel",
		core.View{Style: core.Style{Direction: core.Row, AlignItems: core.AlignCenter, Gap: 5}, Children: []core.Node{
			core.Text{Content: i18n.T("Add provider"), Style: core.Style{FontSize: 13, FontWeight: ui.Semibold, FlexGrow: 1},
				StyleFunc: func() core.Style { return core.Style{Color: colors.Text()} }},
			ui.Button{Text: i18n.T("Close"), Variant: ui.ButtonGhost, Size: controlSize(cx), TestID: "settings-provider-add-close",
				OnPress: func() { a.providerAddOpen.Set(false) }},
		}},
		core.View{Style: core.Style{Direction: core.Row, Gap: 5}, Children: []core.Node{
			ui.Button{Text: i18n.T("ACP Registry"), Variant: func() ui.ButtonVariant {
				if a.providerAddMode.Get() == "registry" {
					return ui.ButtonPrimary
				}
				return ui.ButtonSecondary
			}(), Size: controlSize(cx), TestID: "settings-provider-mode-registry",
				OnPress: func() { a.providerAddMode.Set("registry") }},
			ui.Button{Text: i18n.T("Local command"), Variant: func() ui.ButtonVariant {
				if a.providerAddMode.Get() == "local" {
					return ui.ButtonPrimary
				}
				return ui.ButtonSecondary
			}(), Size: controlSize(cx), TestID: "settings-provider-mode-local",
				OnPress: func() { a.providerAddMode.Set("local") }},
		}},
		body,
	)
}

func (a *app) providerSettingsSection(cx *core.Scope) core.Node {
	colors := ui.ColorsOf(cx)
	cards := a.acpProviderSettingsCards(cx)
	return core.View{Style: core.Style{Direction: core.Column, Gap: 7}, Children: []core.Node{
		core.Text{Content: i18n.T("Configured providers"), Style: core.Style{FontSize: 11, FontWeight: ui.Semibold},
			StyleFunc: func() core.Style { return core.Style{Color: colors.Muted()} }},
		core.View{Style: core.Style{Direction: core.Column, Gap: 6}, Children: cards},
		core.Show{When: func() bool { return a.providerPrepareID.Get() != "" }, Then: func(*core.Scope) core.Node {
			return ui.Spinner{Size: 14, Label: i18n.T("Installing ") + a.providerPrepareID.Get()}
		}},
		a.providerAddPanel(cx),
	}}
}
