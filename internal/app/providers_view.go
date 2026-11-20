package app

import (
	"strings"

	"github.com/egoist/mygo/ui"

	"github.com/tk/t3go/internal/app/icons/lucide"
	"github.com/tk/t3go/internal/i18n"
	"github.com/tk/t3go/internal/proto"
)

// providerSettingsCard is one bordered card of the provider settings.
func providerSettingsCard(c *ui.Context, build func()) {
	t := c.Theme()
	ui.Column(c).Gap(7).Padding(10, 12).Radius(9).Border(1, t.Border).
		Background(t.Surface).Children(build)
}

// providerSettingsHeader names a configured provider with its enable
// switch and its remove action.
func providerSettingsHeader(c *ui.Context, name, detail string, enabled *bool, onEnabled func(), remove func()) {
	t := c.Theme()
	ui.Row(c).AlignItems(ui.Center).Gap(8).Wrap().Children(func() {
		ui.Column(c).Gap(1).Grow(1).MinWidth(140).Children(func() {
			ui.Text(c, name).FontSize(13).FontWeight(600).TextColor(t.Text).SingleLine()
			faint(c, detail, 10.5)
		})
		ui.Row(c).AlignItems(ui.Center).Gap(4).Children(func() {
			if remove != nil {
				ui.Button(c, i18n.T("Remove")).Children(func() {
					ui.Icon(c, lucide.Trash()).FontSize(15)
					ui.Text(c, i18n.T("Remove"))
				}).OnClick(remove)
			}
			ui.Switch(c, enabled).Label(i18n.T("Enable ") + name)
			onEnabled()
		})
	})
}

// providerSettingsSection manages the environment's configured ACP
// providers: the cards, and the panel that adds one.
func (s *Session) providerSettingsSection(c *ui.Context) {
	t := c.Theme()
	target := s.settingsTarget()
	draft := target.settingsDraft
	ui.Column(c).Gap(7).Children(func() {
		faint(c, i18n.T("Configured providers"), 11)
		ui.Column(c).Gap(6).Children(func() {
			for index := range draft.ACPProviders {
				configured := draft.ACPProviders[index]
				index := index
				detail := providerSettingsSource(configured.Source)
				if configured.Version != "" {
					detail += " · " + configured.Version
				}
				if configured.Distribution != "" && configured.Distribution != "auto" {
					detail += " · " + configured.Distribution
				}
				providerSettingsCard(c, func() {
					// The switch writes through enabled during the build;
					// a change commits to the environment's settings.
					enabled := configured.Enabled
					providerSettingsHeader(c, configured.Name, detail, &enabled, func() {
						if enabled != configured.Enabled {
							target.updateProviderSettings(func(settings *proto.Settings) {
								if index < len(settings.ACPProviders) {
									settings.ACPProviders[index].Enabled = enabled
								}
							})
						}
					}, func() {
						target.updateProviderSettings(func(settings *proto.Settings) {
							if index < len(settings.ACPProviders) {
								settings.ACPProviders = append(settings.ACPProviders[:index], settings.ACPProviders[index+1:]...)
							}
						})
					})
					// The name the agent runs under.
					name := target.providerTextValue(providerTextKeyACP(configured.ID, "name"), configured.Name)
					ui.Field(c, i18n.T("ACP provider name"), func() {
						ui.TextInput(c, &name).Placeholder(i18n.T("Provider name"))
					})
					target.setProviderTextValue(providerTextKeyACP(configured.ID, "name"), name)
					if configured.Source == "local" {
						path := target.providerTextValue(providerTextKeyACP(configured.ID, "path"), configured.CommandPath)
						ui.Field(c, i18n.T("ACP executable"), func() {
							ui.TextInput(c, &path).
								Placeholder(i18n.T("/absolute/path/to/agent or executable on PATH"))
						})
						target.setProviderTextValue(providerTextKeyACP(configured.ID, "path"), path)
					} else {
						ui.Text(c, i18n.T("Registry agent %s · %s distribution", configured.AgentID, configured.Distribution)).
							FontSize(10.5).Font("monospace").TextColor(t.TextMuted).MaxLines(2)
					}
					args := target.providerTextValue(providerTextKeyACP(configured.ID, "args"),
						commandArgsText(configured.CommandArgs))
					ui.Field(c, i18n.T("ACP arguments"), func() {
						ui.TextArea(c, &args).Placeholder(i18n.T("One literal argument per line")).Lines(2, 6)
					})
					target.setProviderTextValue(providerTextKeyACP(configured.ID, "args"), args)
					faint(c, i18n.T("Arguments are passed directly to the process; shell expansion is never applied."), 10.5)
				})
			}
		})
		if target.providerPrepareID != "" {
			ui.Row(c).Gap(8).AlignItems(ui.Center).Children(func() {
				ui.Spinner(c).Size(14, 14).Label(i18n.T("Installing " + target.providerPrepareID))
			})
		}
		if target.providerManageErr != "" {
			ui.Text(c, target.providerManageErr).FontSize(11).TextColor(t.Danger)
		}
		target.providerAddPanel(c)
	})
}

// providerAddPanel is the closed button, or the open panel with its two
// modes: the registry search, and a local command.
func (s *Session) providerAddPanel(c *ui.Context) {
	t := c.Theme()
	if !s.providerAddOpen {
		ui.Button(c, i18n.T("Add ACP provider")).Children(func() {
			ui.Icon(c, lucide.Plus()).FontSize(15)
			ui.Text(c, i18n.T("Add ACP provider"))
		}).OnClick(s.openProviderAdd)
		return
	}
	providerSettingsCard(c, func() {
		ui.Row(c).AlignItems(ui.Center).Gap(5).Children(func() {
			text(c, i18n.T("Add provider"), 13, t.Text, 600).Grow(1)
			ui.Button(c, i18n.T("Close")).OnClick(func() { s.providerAddOpen = false })
		})
		ui.Row(c).Gap(5).Children(func() {
			var registry, local ui.Element
			registry = ui.Button(c, i18n.T("ACP Registry"))
			if s.providerAddMode == "registry" {
				registry = ui.PrimaryButton(c, i18n.T("ACP Registry"))
			}
			registry.OnClick(func() { s.providerAddMode = "registry" })
			local = ui.Button(c, i18n.T("Local command"))
			if s.providerAddMode == "local" {
				local = ui.PrimaryButton(c, i18n.T("Local command"))
			}
			local.OnClick(func() { s.providerAddMode = "local" })
		})
		if s.providerAddMode == "local" {
			s.providerLocalPanel(c)
		} else {
			s.providerRegistryPanel(c)
		}
	})
}

// providerRegistryPanel searches the ACP Registry and offers each agent
// with its distributions.
func (s *Session) providerRegistryPanel(c *ui.Context) {
	t := c.Theme()
	ui.Column(c).Gap(7).Children(func() {
		ui.Row(c).AlignItems(ui.Center).Gap(6).Children(func() {
			ui.Field(c, i18n.T("Search ACP Registry"), func() {
				ui.TextInput(c, &s.providerSearchQuery).Placeholder(i18n.T("Search agents"))
			}).Grow(1)
			search := ui.Button(c, i18n.T("Search")).Children(func() {
				ui.Icon(c, lucide.Search()).FontSize(15)
				ui.Text(c, i18n.T("Search"))
			})
			if s.providerCatalogBusy {
				search.Disabled(true)
			}
			search.OnClick(func() { s.searchProviderCatalog(true) })
		})
		ui.Column(c).Gap(5).Children(func() {
			switch {
			case s.providerCatalogBusy:
				ui.Row(c).Gap(8).AlignItems(ui.Center).Children(func() {
					ui.Spinner(c).Size(14, 14).Label(i18n.T("Loading ACP Registry"))
				})
			case s.providerCatalogErr != "":
				ui.Text(c, s.providerCatalogErr).FontSize(11).TextColor(t.Danger)
			case len(s.providerCatalog) == 0:
				faint(c, i18n.T("No compatible registry agents found."), 11.5)
			default:
				for _, agent := range s.providerCatalog {
					agent := agent
					detail := agent.Description
					if detail == "" {
						detail = agent.ID
					}
					ui.Column(c).Gap(6).Padding(8, 10).Radius(7).Border(1, t.Border).Children(func() {
						ui.Column(c).Gap(1).Grow(1).MinWidth(0).Children(func() {
							ui.Text(c, agent.Name).FontSize(12.5).FontWeight(600).TextColor(t.Text).SingleLine()
							ui.Text(c, agent.Version+" · "+strings.Join(agent.Distributions, ", ")).
								FontSize(10).Font("monospace").TextColor(t.TextMuted).SingleLine()
						})
						ui.Text(c, detail).FontSize(10.5).LineHeight(15).TextColor(t.TextMuted).MaxLines(3)
						ui.Row(c).Wrap().Gap(4).Children(func() {
							for _, distribution := range agent.Distributions {
								if distribution != "binary" && distribution != "npx" && distribution != "uvx" {
									continue
								}
								distribution := distribution
								label, icon := i18n.T("Add via ")+distribution, lucide.Plus()
								if distribution == "binary" {
									label, icon = i18n.T("Install"), lucide.Download()
								}
								b := ui.Button(c, label).Children(func() {
									ui.Icon(c, icon).FontSize(15)
									ui.Text(c, label)
								})
								if s.providerPrepareID != "" {
									b.Disabled(true)
								}
								b.OnClick(func() { s.addRegistryProvider(agent, distribution) })
							}
						})
					})
				}
			}
		})
	})
}

// providerLocalPanel adds an executable on the environment's machine as
// an ACP provider.
func (s *Session) providerLocalPanel(c *ui.Context) {
	ui.Column(c).Gap(7).Children(func() {
		ui.Field(c, i18n.T("Local ACP provider name"), func() {
			ui.TextInput(c, &s.providerLocalName).Placeholder(i18n.T("Provider name"))
		})
		ui.Field(c, i18n.T("Local ACP executable"), func() {
			ui.TextInput(c, &s.providerLocalPath).
				Placeholder(i18n.T("/absolute/path/to/agent or executable on PATH"))
		})
		ui.Field(c, i18n.T("Local ACP arguments"), func() {
			ui.TextArea(c, &s.providerLocalArgs).Placeholder(i18n.T("One literal argument per line")).Lines(2, 6)
		})
		faint(c, i18n.T("The executable is started directly without a shell. Relative executable names are resolved on the environment's PATH."), 10.5)
		ui.PrimaryButton(c, i18n.T("Add local ACP provider")).Children(func() {
			ui.Icon(c, lucide.Plus()).FontSize(15)
			ui.Text(c, i18n.T("Add local ACP provider"))
		}).OnClick(s.addLocalACPProvider)
	})
}
