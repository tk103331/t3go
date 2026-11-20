package app

import (
	"fmt"
	"runtime"
	"runtime/debug"
	"strings"

	"github.com/egoist/mygo/ui"

	"github.com/tk/t3go/internal/app/icons/lucide"
	"github.com/tk/t3go/internal/i18n"
	"github.com/tk/t3go/internal/proto"
)

const (
	appVersion     = "0.1.0"
	appBuildNumber = "114"
)

func mygoVersion() string {
	info, ok := debug.ReadBuildInfo()
	if !ok {
		return "unknown"
	}
	for _, dependency := range info.Deps {
		if dependency.Path == "github.com/egoist/mygo" {
			version := dependency.Version
			if dependency.Replace != nil && dependency.Replace.Version != "" {
				version = dependency.Replace.Version
			}
			if strings.TrimSpace(version) != "" {
				return version
			}
		}
	}
	return "unknown"
}

func goRuntimeVersion() string {
	return runtime.Version() + " · " + runtime.GOOS + "/" + runtime.GOARCH
}

// settingsProviders shows each agent CLI's state and models.
func (s *Session) settingsProviders(c *ui.Context) {
	t := c.Theme()
	target := s.settingsTarget()
	providers := target.providers
	models := target.allModels()
	ui.Column(c).Gap(8).Children(func() {
		settingsSectionTitle(c, i18n.T("Providers"), i18n.T("Configure ACP agents for this environment, then detect their installed models."))
		if target.providersBusy {
			ui.Row(c).Gap(8).AlignItems(ui.Center).Children(func() {
				ui.Spinner(c).Size(14, 14).Label(i18n.T("Detecting providers on ") + target.envName())
			})
		}
		if target.providersErr != "" {
			ui.Text(c, target.providersErr).FontSize(12).TextColor(t.Danger).MaxLines(3)
		}
		ui.Row(c).AlignItems(ui.Center).Gap(8).Children(func() {
			ui.Text(c, i18n.T("%d providers · %d models · %s", len(providers), len(models), target.envName())).
				FontSize(11.5).TextColor(t.TextMuted).MaxLines(2).Grow(1).MinWidth(0)
			refresh := ui.Button(c, i18n.T("Refresh")).Children(func() {
				ui.Icon(c, lucide.RefreshCw()).FontSize(15)
				ui.Text(c, i18n.T("Refresh"))
			})
			if target.providersBusy {
				refresh.Disabled(true)
			}
			refresh.OnClick(target.refreshProviders)
		})
		// TODO(view): the ACP agent registry's editor port lands with the
		// provider catalog's views.
		faint(c, i18n.T("Detected providers"), 11)
		ui.Column(c).Gap(6).Children(func() {
			if len(providers) == 0 {
				faint(c, i18n.T("No providers detected yet."), 11.5)
			}
			for _, provider := range providers {
				provider := provider
				status := "Installed"
				color := t.Success
				switch {
				case !provider.Installed:
					status = i18n.T("Not found on PATH")
					color = t.Danger
				case provider.AuthStatus == "authenticated":
					status = i18n.T("Installed · signed in")
				case provider.AuthStatus == "unauthenticated":
					status = i18n.T("Installed · signed out")
					color = t.TextMuted
				}
				ui.Column(c).Gap(5).Padding(8, 10).Radius(8).Background(t.Surface).Children(func() {
					ui.Row(c).AlignItems(ui.Center).Gap(6).Children(func() {
						ui.Column(c).Gap(1).MinWidth(0).Children(func() {
							ui.Text(c, provider.Name).FontSize(12.5).FontWeight(600).TextColor(t.Text).SingleLine()
							faint(c, provider.ID, 10.5)
						})
						ui.Spacer(c)
						ui.Column(c).Padding(2, 7).Radius(5).Background(color.Alpha(0.12)).Children(func() {
							ui.Text(c, status).FontSize(10).FontWeight(600).TextColor(color).SingleLine()
						})
					})
					if provider.Version != "" {
						faint(c, provider.Version, 10.5)
					}
					if provider.Path != "" {
						ui.Text(c, provider.Path).FontSize(10.5).Font("monospace").TextColor(t.TextMuted).MaxLines(2)
					}
					if provider.Error != "" && !strings.EqualFold(provider.Error, i18n.T("not installed")) {
						ui.Text(c, provider.Error).FontSize(10.5).TextColor(t.Danger).MaxLines(2)
					}
					models := 0
					ui.Row(c).Wrap().Gap(3).Padding(0, 0, 2, 0).Children(func() {
						for _, model := range models2list(target.allModels()) {
							if model.Provider != provider.ID {
								continue
							}
							models++
							model := model
							label := model.Name
							if model.Default {
								label += i18n.T(" · default")
							}
							if model.ContextWindow > 0 {
								label += " · " + humanTokens(int64(model.ContextWindow))
							}
							ui.Row(c).AlignItems(ui.Center).Gap(2).MaxWidthPercent(100).MinWidth(0).
								Padding(2, 7).Radius(5).Background(t.Background).Children(func() {
								ui.Text(c, label).FontSize(10.5).TextColor(t.TextMuted).SingleLine().MinWidth(0)
								if model.Custom {
									ui.Button(c, "×").Label(i18n.T("Remove " + model.Name)).Children(func() {
										ui.Icon(c, lucide.X()).FontSize(9)
									}).Tooltip(i18n.T("Remove " + model.Name)).OnClick(func() {
										target.removeCustomModel(provider.ID, model.ID)
									})
								}
							})
						}
						if models == 0 && provider.Installed {
							ui.Text(c, i18n.T("No models reported yet.")).FontSize(10.5).TextColor(t.TextMuted)
						}
					})
					if provider.AuthStatus == "unauthenticated" || provider.AuthStatus == "unknown" {
						if provider.LoginCommand != "" {
							ui.Text(c, provider.LoginCommand).FontSize(10.5).Font("monospace").
								TextColor(t.TextMuted).MaxLines(2)
						}
					}
					if !provider.Installed && provider.CanInstall && provider.InstallCommand != "" {
						ui.Text(c, provider.InstallCommand).FontSize(10.5).Font("monospace").
							TextColor(t.TextMuted).MaxLines(2)
					}
					ui.Row(c).Wrap().Gap(4).Padding(0, 0, 2, 0).Children(func() {
						if !provider.Installed && provider.CanInstall && provider.InstallCommand != "" {
							ui.Button(c, i18n.T("Install")).Children(func() {
								ui.Icon(c, lucide.Download()).FontSize(15)
								ui.Text(c, i18n.T("Install"))
							}).OnClick(func() {
								s.runSettingsProviderCommand(target, provider.InstallCommand)
							})
						}
						if provider.Installed && provider.CanAuthenticate && provider.AuthStatus != "authenticated" && provider.LoginCommand != "" {
							ui.Button(c, i18n.T("Sign in")).Children(func() {
								ui.Icon(c, lucide.LogIn()).FontSize(15)
								ui.Text(c, i18n.T("Sign in"))
							}).OnClick(func() {
								s.runSettingsProviderCommand(target, provider.LoginCommand)
							})
						}
						ui.Button(c, i18n.T("Add model")).Children(func() {
							ui.Icon(c, lucide.Plus()).FontSize(15)
							ui.Text(c, i18n.T("Add model"))
						}).OnClick(func() {
							target.customModelProvider = provider.ID
							target.showCustomModel = true
						})
					})
				})
			}
		})
	})
}

// models2list flattens the models slice; a named copy of the range
// variable for the closure above.
func models2list(models []proto.Model) []proto.Model { return models }

// settingsUsage summarizes token and cost usage.
func (s *Session) settingsUsage(c *ui.Context) {
	t := c.Theme()
	target := s.settingsTarget()
	report := target.usageReport
	ui.Column(c).Gap(8).Children(func() {
		settingsSectionTitle(c, i18n.T("Usage"), i18n.T("Tokens and cost of the runs this environment recorded."))
		ui.Row(c).AlignItems(ui.Center).Gap(4).Children(func() {
			ui.Text(c, report.Since+i18n.T(" – today")).FontSize(11).TextColor(t.TextMuted)
			ui.Spacer(c)
			if target.usageBusy {
				ui.Spinner(c).Size(12, 12).Label(i18n.T("Loading usage"))
			}
			for _, option := range []int{7, 30, 90} {
				option := option
				label := fmt.Sprintf("%dd", option)
				var b ui.Element
				if option == target.usageDays {
					b = ui.PrimaryButton(c, label)
				} else {
					b = ui.Button(c, label)
				}
				b.OnClick(func() { target.loadUsage(option) })
			}
		})
		ui.Row(c).Gap(6).Children(func() {
			usageCard(c, "Runs", fmt.Sprint(report.Totals.Runs))
			usageCard(c, "Tokens", humanTokens(report.Totals.Tokens))
			usageCard(c, "Cost", fmt.Sprintf("$%.2f", report.Totals.Cost))
		})
		if report.Totals.Runs > 0 {
			var max int64
			for _, day := range report.Days {
				if day.Tokens > max {
					max = day.Tokens
				}
			}
			ui.Row(c).AlignItems(ui.End).Gap(2).Height(104).Padding(4, 0, 0, 0).Children(func() {
				for _, day := range report.Days {
					day := day
					height := float32(2)
					if max > 0 && day.Tokens > 0 {
						height = 4 + 96*float32(day.Tokens)/float32(max)
					}
					ui.Box(c).Grow(1).MinWidth(2).Height(height).Radius(2).
						Background(t.Accent.Alpha(0.55))
				}
			})
		} else {
			faint(c, i18n.T("No usage recorded in this range."), 11.5)
		}
		if target.usageErr != "" {
			ui.Text(c, target.usageErr).FontSize(11).TextColor(t.Danger)
		}
		if len(report.Providers) > 0 {
			faint(c, i18n.T("Providers"), 11)
			ui.Column(c).Gap(4).Children(func() {
				for _, provider := range report.Providers {
					provider := provider
					ui.Row(c).AlignItems(ui.Center).Gap(8).Padding(5, 6).Radius(6).
						Background(t.Surface).Children(func() {
						ui.Text(c, provider.Provider).FontSize(11.5).FontWeight(500).
							TextColor(t.Text).SingleLine().Grow(1).MinWidth(0)
						ui.Text(c, i18n.T("%d runs", provider.Runs)).FontSize(10.5).TextColor(t.TextMuted)
						ui.Text(c, humanTokens(provider.Tokens)+i18n.T(" tokens")).FontSize(10.5).TextColor(t.TextMuted)
						ui.Text(c, fmt.Sprintf("$%.2f", provider.Cost)).FontSize(10.5).TextColor(t.TextMuted)
					})
				}
			})
		}
	})
}

// usageCard is one total in the usage page.
func usageCard(c *ui.Context, label, value string) {
	t := c.Theme()
	ui.Column(c).Gap(1).Padding(8, 10).Radius(7).Background(t.Surface).Children(func() {
		ui.Text(c, value).FontSize(16).FontWeight(600).TextColor(t.Text)
		faint(c, label, 10.5)
	})
}

// settingsWorktrees lists threads that run in their own checkouts.
func (s *Session) settingsWorktrees(c *ui.Context) {
	t := c.Theme()
	ui.Column(c).Gap(8).Children(func() {
		settingsSectionTitle(c, i18n.T("Worktrees"),
			i18n.T("New threads can run in the project directory or in their own worktree. T3Go keeps managed worktrees until their thread is deleted."))
		ui.Column(c).Gap(6).Children(func() {
			shown := 0
			for _, thread := range s.threads {
				if thread.WorktreePath == "" {
					continue
				}
				shown++
				thread := thread
				meta := thread.WorktreePath
				if thread.Branch != "" {
					meta = thread.Branch + " · " + meta
				}
				ui.Column(c).Gap(2).Padding(7, 9).Radius(7).Background(t.Surface).Children(func() {
					ui.Text(c, thread.Title).FontSize(12).FontWeight(600).TextColor(t.Text).SingleLine()
					ui.Text(c, meta).FontSize(10.5).Font("monospace").TextColor(t.TextMuted).SingleLine()
				})
			}
			if shown == 0 {
				faint(c, i18n.T("No thread is using a worktree."), 11.5)
			}
		})
	})
}

// settingsShortcuts lists the app's keyboard shortcuts.
func (s *Session) settingsShortcuts(c *ui.Context) {
	t := c.Theme()
	shortcuts := [][2]string{
		{"⌘K", i18n.T("Search threads and actions")},
		{"⌘N", i18n.T("New thread")},
		{"⌘⇧N", i18n.T("New project")},
		{"⌘,", i18n.T("Settings")},
		{"⌘B", i18n.T("Toggle sidebar")},
		{"↑ / ↓", i18n.T("Move through lists")},
		{"Return", i18n.T("Open the selected item")},
		{"Esc", i18n.T("Close a dialog")},
	}
	ui.Column(c).Gap(4).Children(func() {
		settingsSectionTitle(c, i18n.T("Shortcuts"),
			i18n.T("Keyboard shortcuts on this platform; Ctrl replaces ⌘ outside Apple platforms."))
		ui.Column(c).Gap(1).Children(func() {
			for _, shortcut := range shortcuts {
				shortcut := shortcut
				ui.Row(c).AlignItems(ui.Center).Gap(10).Padding(6, 4).Children(func() {
					ui.Column(c).Width(64).Padding(2, 6).Radius(5).Background(t.Surface).Children(func() {
						ui.Text(c, shortcut[0]).FontSize(10.5).FontWeight(500).TextColor(t.Text)
					})
					ui.Text(c, shortcut[1]).FontSize(12).TextColor(t.TextMuted).SingleLine().Grow(1).MinWidth(0)
				})
			}
		})
	})
}

// settingsAbout describes this client and the server running on this
// machine. It intentionally ignores the selected settings environment.
func (s *Session) settingsAbout(c *ui.Context) {
	t := c.Theme()
	target := s.localSettingsTarget()
	ui.Column(c).Gap(8).Children(func() {
		settingsSectionTitle(c, i18n.T("About T3Go"), i18n.T("Client and local environment build information."))
		ui.Column(c).Gap(4).Children(func() {
			aboutRow(c, i18n.T("Client"), i18n.T("T3Go ")+appVersion)
			aboutRow(c, i18n.T("Build"), appBuildNumber)
			aboutRow(c, i18n.T("Go"), goRuntimeVersion())
			aboutRow(c, "MyGo", mygoVersion())
			aboutRow(c, i18n.T("Protocol"), i18n.T("API v%d", proto.ProtocolVersion))
			if target != nil {
				server := target.serverVersion
				if server == "" {
					server = i18n.T("Unknown")
				}
				aboutRow(c, i18n.T("Server"), server)
				aboutRow(c, i18n.T("Local URL"), target.serverURL)
				aboutRow(c, i18n.T("Machine"), target.envName())
				if target.profile.Hostname != "" {
					aboutRow(c, i18n.T("Hostname"), target.profile.Hostname)
				}
				if target.profile.System != nil {
					system := strings.TrimSpace(target.profile.System.OS + " " + target.profile.System.OSVersion)
					if target.profile.System.Arch != "" {
						system = strings.TrimSpace(system + " · " + target.profile.System.Arch)
					}
					aboutRow(c, i18n.T("System"), system)
				}
				if target.offline {
					ui.Text(c, i18n.T("The local event stream is reconnecting.")).
						FontSize(11.5).TextColor(t.Danger)
				}
			} else {
				ui.Text(c, i18n.T("No local T3Go environment is connected on this machine.")).
					FontSize(11.5).TextColor(t.TextMuted)
			}
		})
		ui.Text(c, i18n.T("Remote environments are managed under Environments. This page reports only the client and the T3Go server running on this machine.")).
			FontSize(11).LineHeight(16).TextColor(t.TextMuted).MaxLines(4)
		if s.manager != nil {
			ui.Button(c, i18n.T("Manage environments")).OnClick(func() {
				s.setSettingsSection("environments")
			})
		}
	})
}

// aboutRow is one label-value line of the About page.
func aboutRow(c *ui.Context, label, value string) {
	t := c.Theme()
	ui.Row(c).AlignItems(ui.Center).Gap(8).Padding(4, 0).Children(func() {
		ui.Text(c, label).FontSize(12).TextColor(t.TextMuted).Width(90)
		ui.Text(c, value).FontSize(12).TextColor(t.Text).MaxLines(2).Selectable().Grow(1).MinWidth(0)
	})
}

// settingsEnvironments manages the saved environments inside settings.
func (s *Session) settingsEnvironments(c *ui.Context) {
	t := c.Theme()
	ui.Column(c).Gap(8).Children(func() {
		settingsSectionTitle(c, i18n.T("Environments"),
			i18n.T("The workspaces this client keeps connections to. One is active at a time."))
		ui.Column(c).Gap(6).Children(func() {
			if s.manager == nil {
				faint(c, i18n.T("This client runs one environment."), 11.5)
				return
			}
			names := s.manager.environmentNames()
			for _, p := range s.manager.profiles {
				p := p
				selected := p.ID == s.manager.current
				row := ui.Row(c).AlignItems(ui.Center).Gap(8).Padding(8, 10).Radius(8).
					Background(t.Surface).Children(func() {
					ui.Column(c).Gap(2).Grow(1).MinWidth(0).Children(func() {
						ui.Text(c, names[p.ID]).FontSize(12.5).FontWeight(500).TextColor(t.Text).SingleLine()
						faint(c, p.Routes[0].URL, 10.5)
					})
					if selected {
						text(c, i18n.T("Active"), 10.5, t.Accent)
					} else {
						ui.Button(c, i18n.T("Open")).OnClick(func() {
							s.manager.activate(p.ID)
							s.showSettings = false
						})
					}
					ui.Button(c, "").Label(i18n.T("Forget environment")).Children(func() {
						ui.Icon(c, lucide.Trash()).FontSize(14).TextColor(t.Danger)
					}).Tooltip(i18n.T("Forget environment")).OnClick(func() {
						s.manager.forget(p.ID)
					})
				})
				if selected {
					row.Border(1, t.Accent)
				}
			}
		})
		ui.Row(c).Gap(8).Children(func() {
			ui.PrimaryButton(c, i18n.T("Add environment")).Children(func() {
				ui.Icon(c, lucide.Plus()).FontSize(15)
				ui.Text(c, i18n.T("Add environment"))
			}).OnClick(func() {
				s.showSettings = false
				s.manager.addEnvironment()
			})
			if s.manager != nil && s.manager.accountSessionValid() {
				ui.Button(c, i18n.T("Sign out of Relay")).OnClick(func() {
					s.manager.signOutRelayAccount()
				})
			} else {
				ui.Button(c, i18n.T("Sign in to Relay")).OnClick(func() {
					s.showSettings = false
					s.manager.accountOpen = true
				})
			}
		})
	})
}

// settingsBackground manages this machine's environment server and its
// reach on the network.
func (s *Session) settingsBackground(c *ui.Context) {
	ui.Column(c).Gap(8).Children(func() {
		settingsSectionTitle(c, i18n.T("Background server"),
			i18n.T("The environment server this client ships, and whether it keeps running after the client exits."))
		if s.manager == nil {
			faint(c, i18n.T("This client runs one environment."), 11.5)
			return
		}
		background := s.manager.background
		ui.Row(c).AlignItems(ui.Center).Gap(8).Children(func() {
			ui.Text(c, i18n.T("Keep the environment server running after T3Go quits")).
				FontSize(12.5).TextColor(c.Theme().Text).Grow(1)
			ui.Switch(c, &background).OnChange(func() {
				s.manager.setBackgroundServer(background)
			})
		})
		lan := s.manager.lan
		ui.Row(c).AlignItems(ui.Center).Gap(8).Children(func() {
			ui.Text(c, i18n.T("Let devices on this network pair directly")).
				FontSize(12.5).TextColor(c.Theme().Text).Grow(1)
			ui.Switch(c, &lan).OnChange(func() {
				s.manager.setLANAccess(lan)
			})
		})
		if s.manager.localStarting {
			ui.Row(c).Gap(8).AlignItems(ui.Center).Children(func() {
				ui.Spinner(c).Size(12, 12).Label(i18n.T("Starting the local environment"))
				faint(c, i18n.T("Starting the local environment…"), 11.5)
			})
		}
		if s.manager.localErr != "" {
			errorNote(c, s.manager.localErr)
		}
		if s.manager.local != nil && s.manager.local.baseURL != "" {
			aboutRow(c, i18n.T("Local URL"), s.manager.local.baseURL)
		}
	})
}

// settingsAccess configures pairing only for the local environment: the
// pairing link and QR code, the routes that carry it, and whether the
// environment server keeps running for paired devices.
func (s *Session) settingsAccess(c *ui.Context) {
	t := c.Theme()
	if !isLocalEnvironmentProfile(s.profile) {
		ui.Column(c).Gap(8).Children(func() {
			settingsSectionTitle(c, i18n.T("Access"),
				i18n.T("Device pairing is available only for the T3Go environment running on this machine."))
			ui.Text(c, i18n.T("Select the local environment to manage Access.")).
				FontSize(12).TextColor(t.TextMuted)
		})
		return
	}
	target := s.localSettingsTarget()
	if target == nil {
		ui.Column(c).Gap(8).Children(func() {
			settingsSectionTitle(c, i18n.T("Access"),
				i18n.T("Device pairing is available only for the T3Go environment running on this machine."))
			ui.Text(c, i18n.T("No local environment is connected.")).
				FontSize(12).TextColor(t.TextMuted)
		})
		return
	}
	on := target.settingsDraft.AccessEnabled
	hasLANCard := target.hasLANAccessCard()
	ui.Column(c).Gap(10).Children(func() {
		settingsSectionTitle(c, i18n.T("Access"),
			i18n.T("Local desktop use needs no account. Devices on the same Wi-Fi pair directly; Relay is only for access across networks."))
		if hasLANCard {
			s.lanAccessCard(c)
		}
		// The pairing switch writes through the draft and rebuilds the
		// link from the environment's routes.
		ui.Row(c).AlignItems(ui.Center).Gap(12).Padding(10, 12).Radius(9).
			Background(t.Surface).Children(func() {
			ui.Column(c).Gap(2).Grow(1).MinWidth(0).Children(func() {
				ui.Text(c, i18n.T("Allow device pairing")).FontSize(13).FontWeight(600).TextColor(t.Text)
				ui.Text(c, i18n.T("Shows a pairing link and QR code for this machine. Credentials stay in local environment storage.")).
					FontSize(11).LineHeight(16).TextColor(t.TextMuted).MaxLines(3)
			})
			ui.Switch(c, &on).Label(i18n.T("Allow device pairing")).OnChange(func() {
				draft := target.settingsDraft
				draft.AccessEnabled = on
				target.settingsDraft = draft
				target.refreshAccess()
			})
		})
		if !on {
			s.backgroundServerCard(c)
			return
		}
		directRoute, hasDirect := firstAccessRoute(target.profile, "direct")
		relayRoute, hasRelay := firstAccessRoute(target.profile, "relay")
		modeDirect, modeRelay := accessRouteMode(target.settingsDraft)
		toggleRoute := func(direct, value bool) {
			draft := target.settingsDraft
			currentDirect, currentRelay := accessRouteMode(draft)
			if direct {
				currentDirect = value
			} else {
				currentRelay = value
			}
			if currentDirect || currentRelay {
				setAccessRouteMode(&draft, currentDirect, currentRelay)
			}
			target.settingsDraft = draft
			target.refreshAccess()
		}
		faint(c, i18n.T("Pairing routes"), 12)
		if !hasLANCard {
			directDetail := i18n.T("No direct route saved for this environment.")
			if hasDirect {
				directDetail = directRoute.URL
			}
			s.accessRouteRow(c, i18n.T("LAN direct"), directDetail, hasDirect,
				modeDirect && hasDirect, modeDirect && hasDirect && (!modeRelay || !hasRelay),
				func(value bool) { toggleRoute(true, value) })
		}
		relayDetail := i18n.T("No relay route saved for this environment.")
		if hasRelay {
			relayDetail = relayRoute.URL + " · " + relayAccessStateLabel(target.relayAccess)
		}
		s.accessRouteRow(c, "Relay", relayDetail, hasRelay,
			modeRelay && hasRelay, modeRelay && hasRelay && (!modeDirect || !hasDirect),
			func(value bool) { toggleRoute(false, value) })
		if !hasRelay {
			faint(c, i18n.T("Remote access (optional)"), 12)
			s.relayConfigurationCard(c)
		}
		link := target.accessLink
		if target.accessErr != "" {
			ui.Text(c, target.accessErr).FontSize(11.5).TextColor(t.Danger)
			s.backgroundServerCard(c)
			return
		}
		if warning := accessRouteWarning(link); warning != "" {
			ui.Text(c, warning).FontSize(11).TextColor(t.Danger)
		}
		ui.Column(c).Gap(8).Padding(10, 12).Radius(9).Background(t.Surface).Children(func() {
			ui.Row(c).AlignItems(ui.Center).Gap(8).Children(func() {
				ui.Icon(c, lucide.KeyRound()).FontSize(16).TextColor(t.TextMuted)
				text(c, i18n.T("Pairing link"), 12, t.Text, 600).Grow(1)
				ui.Button(c, i18n.T("Copy link")).Children(func() {
					ui.Icon(c, lucide.Copy()).FontSize(15)
					ui.Text(c, i18n.T("Copy link"))
				}).OnClick(func() { s.setClipboard(link) })
			})
			ui.Text(c, link).FontSize(10.5).LineHeight(15).Font("monospace").
				TextColor(t.TextMuted).MaxLines(4).Selectable()
		})
		if len(target.accessQR) > 0 {
			if bitmap, err := ui.DecodeBitmap(target.accessQR); err == nil {
				ui.Column(c).AlignItems(ui.Center).Padding(8).Children(func() {
					ui.Image(c, bitmap).Size(220, 220).Radius(8)
				})
			}
		}
		s.backgroundServerCard(c)
	})
}

// accessRouteRow is one pairing route with its switch.
func (s *Session) accessRouteRow(c *ui.Context, label, detail string, available, checked, disabled bool, onChange func(bool)) {
	t := c.Theme()
	on := checked
	ui.Row(c).AlignItems(ui.Center).Gap(12).Padding(8, 10).Radius(8).
		Background(t.Surface).Children(func() {
		ui.Column(c).Gap(2).Grow(1).MinWidth(0).Children(func() {
			ui.Text(c, label).FontSize(12.5).FontWeight(600).TextColor(t.Text)
			ui.Text(c, detail).FontSize(10.5).TextColor(t.TextMuted).MaxLines(2)
		})
		if available {
			sw := ui.Switch(c, &on).Label(label + i18n.T(" pairing route"))
			if disabled {
				sw.Disabled(true)
			}
			sw.OnChange(func() { onChange(on) })
		} else {
			faint(c, i18n.T("Not configured"), 11)
		}
	})
}

// relayConfigurationCard connects this machine to the Relay account the
// client is signed into.
func (s *Session) relayConfigurationCard(c *ui.Context) {
	t := c.Theme()
	target := s.localSettingsTarget()
	if target == nil {
		return
	}
	session, server, signedIn := target.relayAccountSession()
	ui.Column(c).Gap(8).Padding(9, 11).Radius(8).Border(1, t.Border).
		Background(t.Surface).Children(func() {
		ui.Column(c).Gap(2).Grow(1).MinWidth(0).Children(func() {
			text(c, i18n.T("Relay access"), 12.5, t.Text, 600)
			ui.Text(c, i18n.T("Use the Relay account already signed in under Account.")).
				FontSize(10.5).TextColor(t.TextMuted).MaxLines(2)
		})
		if signedIn {
			name := strings.TrimSpace(session.User.Name)
			if name == "" {
				name = session.User.Email
			}
			ui.Row(c).AlignItems(ui.Center).Gap(9).Padding(8, 10).Radius(7).
				Background(t.Background).Children(func() {
				ui.Icon(c, lucide.UserRound()).FontSize(18).TextColor(t.Accent)
				ui.Column(c).Gap(1).Grow(1).MinWidth(0).Children(func() {
					ui.Text(c, name).FontSize(11.5).FontWeight(600).TextColor(t.Text).SingleLine()
					ui.Text(c, session.User.Email+" · "+server).FontSize(10.5).TextColor(t.TextMuted).MaxLines(2)
				})
			})
		} else {
			ui.Text(c, i18n.T("No Relay account is signed in. Sign in once under Account, then return here to connect this machine.")).
				FontSize(10.5).LineHeight(15).TextColor(t.TextMuted).MaxLines(3)
		}
		if target.relayErr != "" {
			ui.Text(c, target.relayErr).FontSize(10.5).TextColor(t.Danger).MaxLines(4)
		}
		if target.relayBusy {
			ui.Row(c).Gap(8).AlignItems(ui.Center).Children(func() {
				ui.Spinner(c).Size(14, 14).Label(i18n.T("Connecting Relay"))
			})
		} else if signedIn {
			ui.PrimaryButton(c, i18n.T("Connect Relay")).Children(func() {
				ui.Icon(c, lucide.Link()).FontSize(15)
				ui.Text(c, i18n.T("Connect Relay"))
			}).OnClick(target.configureRelay)
		} else {
			ui.Button(c, i18n.T("Open Account")).Children(func() {
				ui.Icon(c, lucide.UserRound()).FontSize(15)
				ui.Text(c, i18n.T("Open Account"))
			}).OnClick(func() {
				target.setSettingsSection("environments")
				s.manager.accountOpen = true
			})
		}
	})
}

// lanAccessCard exposes the built-in server on the local network. This is
// the no-account path used by a phone on the same Wi-Fi.
func (s *Session) lanAccessCard(c *ui.Context) {
	t := c.Theme()
	if !s.hasLANAccessCard() {
		return
	}
	address := s.localLANURL()
	direct, _ := accessRouteMode(s.settingsDraft)
	value := s.manager.lan && direct
	detail := i18n.T("Turn this on to pair a phone over the same Wi-Fi. The pairing link still requires this environment's token.")
	if value {
		if address == "" {
			detail = i18n.T("LAN access is on, but no private network address was detected. Reconnect this machine to Wi-Fi.")
		} else {
			detail = address + i18n.T(" · Same Wi-Fi only; no account required.")
		}
	}
	ui.Row(c).AlignItems(ui.Center).Gap(12).Padding(10, 12).Radius(9).
		Background(t.Surface).Children(func() {
		ui.Column(c).Gap(2).Grow(1).MinWidth(0).Children(func() {
			ui.Text(c, i18n.T("LAN direct")).FontSize(13).FontWeight(600).TextColor(t.Text)
			ui.Text(c, detail).FontSize(11).LineHeight(16).TextColor(t.TextMuted).MaxLines(3)
		})
		ui.Switch(c, &value).Label(i18n.T("Allow direct access from devices on this LAN")).OnChange(func() {
			draft := s.settingsDraft
			_, relay := accessRouteMode(draft)
			setAccessRouteMode(&draft, value, relay)
			s.settingsDraft = draft
			s.manager.setLANAccess(value)
			s.refreshAccess()
		})
	})
}

// backgroundServerCard decides whether paired devices keep reaching this
// machine once the window closes. Only a package that ships the
// environment server has one to keep running.
func (s *Session) backgroundServerCard(c *ui.Context) {
	t := c.Theme()
	if s.manager == nil || bundledServerPath() == "" {
		return
	}
	value := s.manager.background
	ui.Row(c).AlignItems(ui.Center).Gap(12).Padding(10, 12).Radius(9).
		Background(t.Surface).Children(func() {
		ui.Column(c).Gap(2).Grow(1).MinWidth(0).Children(func() {
			ui.Text(c, i18n.T("Keep running in the background")).FontSize(13).FontWeight(600).TextColor(t.Text)
			ui.Text(c, i18n.T("Paired devices and agent runs reach this machine through its server. Closing T3Go stops that server unless it is kept running here.")).
				FontSize(11).LineHeight(16).TextColor(t.TextMuted).MaxLines(4)
		})
		ui.Switch(c, &value).Label(i18n.T("Keep the local environment server running in the background")).OnChange(func() {
			s.manager.setBackgroundServer(value)
		})
	})
}

// settingsAccess configures pairing only for the local environment: the
// pairing link and QR code, the routes that carry it, and whether the
// environment server keeps running for paired devices.
func (s *Session) settingsAccess(c *ui.Context) {
	t := c.Theme()
	if !isLocalEnvironmentProfile(s.profile) {
		ui.Column(c).Gap(8).Children(func() {
			settingsSectionTitle(c, i18n.T("Access"),
				i18n.T("Device pairing is available only for the T3Go environment running on this machine."))
			ui.Text(c, i18n.T("Select the local environment to manage Access.")).
				FontSize(12).TextColor(t.TextMuted)
		})
		return
	}
	target := s.localSettingsTarget()
	if target == nil {
		ui.Column(c).Gap(8).Children(func() {
			settingsSectionTitle(c, i18n.T("Access"), i18n.T("Device pairing is available only for the T3Go environment running on this machine."))
			ui.Text(c, i18n.T("No local environment is connected.")).
				FontSize(12).TextColor(t.TextMuted)
		})
		return
	}
	on := target.settingsDraft.AccessEnabled
	hasLANCard := target.hasLANAccessCard()
	ui.Column(c).Gap(10).Children(func() {
		settingsSectionTitle(c, i18n.T("Access"),
			i18n.T("Local desktop use needs no account. Devices on the same Wi-Fi pair directly; Relay is only for access across networks."))
		if hasLANCard {
			s.lanAccessCard(c)
		}
		// The pairing switch writes through the draft and rebuilds the
		// link from the environment's routes.
		ui.Row(c).AlignItems(ui.Center).Gap(12).Padding(10, 12).Radius(9).
			Background(t.Surface).Children(func() {
			ui.Column(c).Gap(2).Grow(1).MinWidth(0).Children(func() {
				ui.Text(c, i18n.T("Allow device pairing")).FontSize(13).FontWeight(600).TextColor(t.Text)
				ui.Text(c, i18n.T("Shows a pairing link and QR code for this machine. Credentials stay in local environment storage.")).
					FontSize(11).LineHeight(16).TextColor(t.TextMuted).MaxLines(3)
			})
			ui.Switch(c, &on).Label(i18n.T("Allow device pairing")).OnChange(func() {
				draft := target.settingsDraft
				draft.AccessEnabled = on
				target.settingsDraft = draft
				target.refreshAccess()
			})
		})
		if !on {
			s.backgroundServerCard(c)
			return
		}
		directRoute, hasDirect := firstAccessRoute(target.profile, "direct")
		relayRoute, hasRelay := firstAccessRoute(target.profile, "relay")
		modeDirect, modeRelay := accessRouteMode(target.settingsDraft)
		toggleRoute := func(direct, value bool) {
			draft := target.settingsDraft
			currentDirect, currentRelay := accessRouteMode(draft)
			if direct {
				currentDirect = value
			} else {
				currentRelay = value
			}
			if currentDirect || currentRelay {
				setAccessRouteMode(&draft, currentDirect, currentRelay)
			}
			target.settingsDraft = draft
			target.refreshAccess()
		}
		faint(c, i18n.T("Pairing routes"), 12)
		directDetail := i18n.T("No direct route saved for this environment.")
		if hasDirect {
			directDetail = directRoute.URL
		}
		if !hasLANCard {
			s.accessRouteRow(c, i18n.T("LAN direct"), directDetail, hasDirect,
				modeDirect && hasDirect, modeDirect && hasDirect && (!modeRelay || !hasRelay),
				func(value bool) { toggleRoute(true, value) })
		}
		relayDetail := i18n.T("No relay route saved for this environment.")
		if hasRelay {
			relayDetail = relayRoute.URL + " · " + relayAccessStateLabel(target.relayAccess)
		}
		s.accessRouteRow(c, "Relay", relayDetail, hasRelay,
			modeRelay && hasRelay, modeRelay && hasRelay && (!modeDirect || !hasDirect),
			func(value bool) { toggleRoute(false, value) })
		if !hasRelay {
			faint(c, i18n.T("Remote access (optional)"), 12)
			s.relayConfigurationCard(c)
		}
		link := target.accessLink
		if target.accessErr != "" {
			ui.Text(c, target.accessErr).FontSize(11.5).TextColor(t.Danger)
			s.backgroundServerCard(c)
			return
		}
		if warning := accessRouteWarning(link); warning != "" {
			ui.Text(c, warning).FontSize(11).TextColor(t.Danger)
		}
		ui.Column(c).Gap(8).Padding(10, 12).Radius(9).Background(t.Surface).Children(func() {
			ui.Row(c).AlignItems(ui.Center).Gap(8).Children(func() {
				ui.Icon(c, lucide.KeyRound()).FontSize(16).TextColor(t.TextMuted)
				text(c, i18n.T("Pairing link"), 12, t.Text, 600).Grow(1)
				ui.Button(c, i18n.T("Copy link")).Children(func() {
					ui.Icon(c, lucide.Copy()).FontSize(15)
					ui.Text(c, i18n.T("Copy link"))
				}).OnClick(func() { s.setClipboard(link) })
			})
			ui.Text(c, link).FontSize(10.5).LineHeight(15).Font("monospace").
				TextColor(t.TextMuted).MaxLines(4).Selectable()
		})
		if len(target.accessQR) > 0 {
			if bitmap, err := ui.DecodeBitmap(target.accessQR); err == nil {
				ui.Column(c).AlignItems(ui.Center).Padding(8).Children(func() {
					ui.Image(c, bitmap).Size(220, 220).Radius(8)
				})
			}
		}
		s.backgroundServerCard(c)
	})
}

// accessRouteRow is one pairing route with its switch.
func (s *Session) accessRouteRow(c *ui.Context, label, detail string, available, checked, disabled bool, onChange func(bool)) {
	t := c.Theme()
	on := checked
	ui.Row(c).AlignItems(ui.Center).Gap(12).Padding(8, 10).Radius(8).
		Background(t.Surface).Children(func() {
		ui.Column(c).Gap(2).Grow(1).MinWidth(0).Children(func() {
			ui.Text(c, label).FontSize(12.5).FontWeight(600).TextColor(t.Text)
			ui.Text(c, detail).FontSize(10.5).TextColor(t.TextMuted).MaxLines(2)
		})
		if available {
			sw := ui.Switch(c, &on).Label(label + i18n.T(" pairing route"))
			if disabled {
				sw.Disabled(true)
			}
			sw.OnChange(func() { onChange(on) })
		} else {
			faint(c, i18n.T("Not configured"), 11)
		}
	})
}

// relayConfigurationCard connects this machine to the Relay account the
// client is signed into.
func (s *Session) relayConfigurationCard(c *ui.Context) {
	t := c.Theme()
	target := s.localSettingsTarget()
	if target == nil {
		return
	}
	session, server, signedIn := target.relayAccountSession()
	ui.Column(c).Gap(8).Padding(9, 11).Radius(8).Border(1, t.Border).
		Background(t.Surface).Children(func() {
		ui.Column(c).Gap(2).Grow(1).MinWidth(0).Children(func() {
			text(c, i18n.T("Relay access"), 12.5, t.Text, 600)
			ui.Text(c, i18n.T("Use the Relay account already signed in under Account.")).
				FontSize(10.5).TextColor(t.TextMuted).MaxLines(2)
		})
		if signedIn {
			name := strings.TrimSpace(session.User.Name)
			if name == "" {
				name = session.User.Email
			}
			ui.Row(c).AlignItems(ui.Center).Gap(9).Padding(8, 10).Radius(7).
				Background(t.Background).Children(func() {
				ui.Icon(c, lucide.UserRound()).FontSize(18).TextColor(t.Accent)
				ui.Column(c).Gap(1).Grow(1).MinWidth(0).Children(func() {
					ui.Text(c, name).FontSize(11.5).FontWeight(600).TextColor(t.Text).SingleLine()
					ui.Text(c, session.User.Email+" · "+server).FontSize(10.5).TextColor(t.TextMuted).MaxLines(2)
				})
			})
		} else {
			ui.Text(c, i18n.T("No Relay account is signed in. Sign in once under Account, then return here to connect this machine.")).
				FontSize(10.5).LineHeight(15).TextColor(t.TextMuted).MaxLines(3)
		}
		if target.relayErr != "" {
			ui.Text(c, target.relayErr).FontSize(10.5).TextColor(t.Danger).MaxLines(4)
		}
		if target.relayBusy {
			ui.Row(c).Gap(8).AlignItems(ui.Center).Children(func() {
				ui.Spinner(c).Size(14, 14).Label(i18n.T("Connecting Relay"))
			})
		} else if signedIn {
			ui.PrimaryButton(c, i18n.T("Connect Relay")).Children(func() {
				ui.Icon(c, lucide.Link()).FontSize(15)
				ui.Text(c, i18n.T("Connect Relay"))
			}).OnClick(target.configureRelay)
		} else {
			ui.Button(c, i18n.T("Open Account")).Children(func() {
				ui.Icon(c, lucide.UserRound()).FontSize(15)
				ui.Text(c, i18n.T("Open Account"))
			}).OnClick(func() {
				target.setSettingsSection("environments")
				s.manager.accountOpen = true
			})
		}
	})
}

// lanAccessCard exposes the built-in server on the local network. This is
// the no-account path used by a phone on the same Wi-Fi.
func (s *Session) lanAccessCard(c *ui.Context) {
	t := c.Theme()
	if !s.hasLANAccessCard() {
		return
	}
	address := s.localLANURL()
	direct, _ := accessRouteMode(s.settingsDraft)
	detail := i18n.T("Turn this on to pair a phone over the same Wi-Fi. The pairing link still requires this environment's token.")
	if s.manager.lan && direct {
		if address == "" {
			detail = i18n.T("LAN access is on, but no private network address was detected. Reconnect this machine to Wi-Fi.")
		} else {
			detail = address + i18n.T(" · Same Wi-Fi only; no account required.")
		}
	}
	value := s.manager.lan && direct
	ui.Row(c).AlignItems(ui.Center).Gap(12).Padding(10, 12).Radius(9).
		Background(t.Surface).Children(func() {
		ui.Column(c).Gap(2).Grow(1).MinWidth(0).Children(func() {
			ui.Text(c, i18n.T("LAN direct")).FontSize(13).FontWeight(600).TextColor(t.Text)
			ui.Text(c, detail).FontSize(11).LineHeight(16).TextColor(t.TextMuted).MaxLines(3)
		})
		ui.Switch(c, &value).Label(i18n.T("Allow direct access from devices on this LAN")).OnChange(func() {
			draft := s.settingsDraft
			_, relay := accessRouteMode(draft)
			setAccessRouteMode(&draft, value, relay)
			s.settingsDraft = draft
			s.manager.setLANAccess(value)
			s.refreshAccess()
		})
	})
}

// backgroundServerCard decides whether paired devices keep reaching this
// machine once the window closes. Only a package that ships the
// environment server has one to keep running.
func (s *Session) backgroundServerCard(c *ui.Context) {
	t := c.Theme()
	if s.manager == nil || bundledServerPath() == "" {
		return
	}
	value := s.manager.background
	ui.Row(c).AlignItems(ui.Center).Gap(12).Padding(10, 12).Radius(9).
		Background(t.Surface).Children(func() {
		ui.Column(c).Gap(2).Grow(1).MinWidth(0).Children(func() {
			ui.Text(c, i18n.T("Keep running in the background")).FontSize(13).FontWeight(600).TextColor(t.Text)
			ui.Text(c, i18n.T("Paired devices and agent runs reach this machine through its server. Closing T3Go stops that server unless it is kept running here.")).
				FontSize(11).LineHeight(16).TextColor(t.TextMuted).MaxLines(4)
		})
		ui.Switch(c, &value).Label(i18n.T("Keep the local environment server running in the background")).OnChange(func() {
			s.manager.setBackgroundServer(value)
		})
	})
}
