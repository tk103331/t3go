package app

import (
	"github.com/egoist/mygo/ui"

	"github.com/tk/t3go/internal/app/icons/lucide"
	"github.com/tk/t3go/internal/i18n"
)

// connectScreen is what an environment that has not answered shows: the
// address and token fields, the saved environments, and the Relay
// account.
func connectScreen(c *ui.Context, s *Session, m *Manager) {
	t := c.Theme()
	ui.Column(c).Fill().Center().Padding(24).Children(func() {
		ui.Column(c).WidthPercent(100).MaxWidth(420).Gap(14).
			Padding(22).Radius(14).Border(1, t.Border).Background(t.Surface).Children(func() {
			ui.Row(c).AlignItems(ui.Center).Gap(12).Children(func() {
				brandWordmark(c, 21)
				ui.Column(c).Gap(3).Children(func() {
					ui.Text(c, "T3Go").FontSize(21).Bold().TextColor(t.Text)
					faint(c, i18n.T("Your coding workspace"), 12)
				})
			})
			text(c, i18n.T("Connect to an environment"), 15, t.Text, 600)
			ui.Text(c, i18n.T("Enter the server address and access token to open your projects and threads.")).
				FontSize(12.5).LineHeight(18).TextColor(t.TextMuted)
			if m != nil {
				if ui.Button(c, "").Label(i18n.T("Sign in to Relay")).Label(i18n.T("Sign in to Relay")).Children(func() {
					ui.Icon(c, lucide.UserRound()).FontSize(15)
					ui.Text(c, i18n.T("Sign in to Relay"))
				}).Clicked() {
					m.accountOpen = true
				}
				if m.localStarting {
					faint(c, i18n.T("Starting the local environment…"), 12)
				}
				if m.localErr != "" {
					errorNote(c, m.localErr)
				}
			}
			if s != nil {
				ui.Field(c, i18n.T("Server address"), func() {
					ui.TextInput(c, &s.serverURL).
						Placeholder(i18n.T("Server, e.g. http://127.0.0.1:4870"))
				})
				ui.Field(c, i18n.T("Token"), func() {
					ui.TextInput(c, &s.token).
						Placeholder(i18n.T("Token, or paste a t3go:// pair link")).Password()
				})
				if s.connErr != "" {
					ui.Column(c).Padding(8, 10).Radius(7).
						Background(s.dangerWash(c)).Children(func() {
						ui.Text(c, s.connErr).FontSize(12).TextColor(t.Danger)
					})
				}
				var connectLabel = i18n.T("Connect")
				if s.connecting {
					connectLabel = i18n.T("Connecting…")
				}
				ui.PrimaryButton(c, "").Label(connectLabel).Children(func() {
					ui.Icon(c, lucide.LogIn()).FontSize(15)
					ui.Text(c, connectLabel)
				}).OnClick(func() {
					s.connect(s.serverURL, s.token)
				}).Grow(1)
				faint(c, i18n.T("Find the token in the environment data directory, or run `t3go pair`."), 11)
				if m != nil && (m.pending != s || len(m.profiles) == 0) {
					ui.Button(c, "").Label(i18n.T("Saved environments")).Children(func() {
						ui.Icon(c, lucide.Network()).FontSize(15)
						ui.Text(c, i18n.T("Saved environments"))
					}).OnClick(func() {
						m.showEnvironments = true
					})
				}
			}
		})
	})
	// The saved-environments list and the Relay account open above the
	// connection screen.
	if m != nil {
		m.dialogs(c)
	}
}

// dangerWash is the translucent wash an error card sits on.
func (s *Session) dangerWash(c *ui.Context) ui.Color {
	return c.Theme().Danger.Alpha(0.1)
}

// dialogs draws the manager's own dialogs: the list of saved
// environments, and the Relay account.
func (m *Manager) dialogs(c *ui.Context) {
	environmentsDialog(c, m)
	relayAccountDialog(c, m)
}

// environmentsDialog lists the saved environments, adds one, and forgets
// one. The workspace opens it from the connection screen and the
// settings.
func environmentsDialog(c *ui.Context, m *Manager) {
	if !m.showEnvironments {
		return
	}
	ui.Modal(c, &m.showEnvironments, func() {
		t := c.Theme()
		ui.Column(c).Width(420).MaxWidthPercent(92).Gap(12).Padding(20).
			Radius(14).Background(t.Background).Children(func() {
			text(c, i18n.T("Environments"), 16, t.Text, 600)
			if len(m.profiles) == 0 {
				faint(c, i18n.T("No environments saved yet. Connect to one to add it."), 12)
			}
			ui.Scroll(c).MaxHeight(300).Gap(6).Children(func() {
				for _, p := range m.profiles {
					p := p
					row := ui.Row(c).AlignItems(ui.Center).Gap(8).Padding(8, 10).
						Radius(8).Background(t.Surface)
					if p.ID == m.current {
						row.Border(1, t.Accent)
					} else {
						row.Border(1, t.Border)
					}
					row.Children(func() {
						ui.Column(c).Gap(2).Grow(1).MinWidth(0).Children(func() {
							ui.Text(c, m.environmentNames()[p.ID]).FontSize(13).FontWeight(500).TextColor(t.Text).SingleLine()
							faint(c, p.Routes[0].URL, 11)
						})
						if p.ID != m.current {
							ui.Button(c, i18n.T("Open")).OnClick(func() {
								m.activate(p.ID)
								m.showEnvironments = false
							})
						}
						ui.Button(c, "").Label("").Label(i18n.T("Forget environment")).Children(func() {
							ui.Icon(c, lucide.Trash()).FontSize(14).TextColor(t.Danger)
						}).Tooltip(i18n.T("Forget environment")).OnClick(func() {
							m.forget(p.ID)
						})
					})
				}
			})
			ui.Row(c).Gap(8).Justify(ui.End).Children(func() {
				if ui.Button(c, i18n.T("Close")).Clicked() {
					m.showEnvironments = false
				}
				ui.PrimaryButton(c, "").Label(i18n.T("Add environment")).Children(func() {
					ui.Icon(c, lucide.Plus()).FontSize(15)
					ui.Text(c, i18n.T("Add environment"))
				}).OnClick(func() {
					m.showEnvironments = false
					m.addEnvironment()
				})
			})
		})
	})
}

// relayAccountDialog signs in, registers, and signs out of a Relay
// account, which discovers the environments it owns.
func relayAccountDialog(c *ui.Context, m *Manager) {
	ui.Modal(c, &m.accountOpen, func() {
		t := c.Theme()
		ui.Column(c).Width(400).MaxWidthPercent(92).Gap(10).Padding(20).
			Radius(14).Background(t.Background).Children(func() {
			ui.Row(c).AlignItems(ui.Center).Gap(8).Children(func() {
				ui.Icon(c, lucide.UserRound()).FontSize(16).TextColor(t.TextMuted)
				text(c, i18n.T("Relay account"), 16, t.Text, 600)
				ui.Spacer(c)
			})
			if m.accountSessionValid() {
				text(c, m.accountSession.User.Email, 13, t.Text)
				faint(c, m.accountSession.User.Name, 12)
				if len(m.accountEnvs) > 0 {
					faint(c, i18n.T("%d environments on this account", len(m.accountEnvs)), 11.5)
				}
				if m.accountErr != "" {
					errorNote(c, m.accountErr)
				}
				ui.Row(c).Gap(8).Justify(ui.End).Children(func() {
					if ui.Button(c, i18n.T("Close")).Clicked() {
						m.accountOpen = false
					}
					if ui.Button(c, i18n.T("Sign out")).Clicked() {
						m.signOutRelayAccount()
					}
				})
				return
			}
			segmentChoice(c, []string{i18n.T("Sign in"), i18n.T("Register")}, i18n.T("Sign in"), func(choice string) {
				if choice == i18n.T("Register") {
					m.accountMode = "register"
				} else {
					m.accountMode = "login"
				}
			})
			ui.Field(c, i18n.T("Relay server"), func() {
				ui.TextInput(c, &m.accountServer).Placeholder("https://relay.example.com")
			})
			ui.Field(c, i18n.T("Email"), func() {
				ui.TextInput(c, &m.accountEmail).Placeholder("you@example.com")
			})
			ui.Field(c, i18n.T("Password"), func() {
				ui.TextInput(c, &m.accountPassword).Password()
			})
			if m.accountMode == "register" {
				ui.Field(c, i18n.T("Name"), func() {
					ui.TextInput(c, &m.accountName)
				})
			}
			allow := m.accountAllowHTTP
			ui.Row(c).Gap(8).AlignItems(ui.Center).Children(func() {
				ui.Checkbox(c, &allow, i18n.T("Allow plain HTTP (insecure)")).OnChange(func() {
					m.accountAllowHTTP = allow
				})
			})
			if m.accountErr != "" {
				errorNote(c, m.accountErr)
			}
			ui.Row(c).Gap(8).Justify(ui.End).Children(func() {
				if ui.Button(c, i18n.T("Cancel")).Clicked() {
					m.accountOpen = false
				}
				var label = i18n.T("Sign in")
				if m.accountMode == "register" {
					label = i18n.T("Register")
				}
				b := ui.PrimaryButton(c, label)
				if m.accountBusy {
					b.Disabled(true)
				}
				b.OnClick(m.signInRelayAccount)
			})
		})
	})
}
