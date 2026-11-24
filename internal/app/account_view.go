package app

import (
	"strings"

	"github.com/egoist/mygo/ui"

	"github.com/tk/t3go/internal/app/icons/lucide"
	"github.com/tk/t3go/internal/client"
	"github.com/tk/t3go/internal/i18n"
)

func (s *Session) settingsAccount(c *ui.Context) {
	settingsSectionTitle(c, i18n.T("Relay account"),
		i18n.T("Use one Relay account to find your computers across networks."))
	if s.manager == nil {
		faint(c, i18n.T("Relay account sign-in is unavailable in this client."), 12)
		return
	}
	relayAccountView(c, s.manager)
}

// Account settings and the connection screen share the same form and
// session state, including registration, discovery and error feedback.
func relayAccountView(c *ui.Context, m *Manager) {
	ui.Column(c).FillWidth().Gap(12).Children(func() {
		if m.accountSessionValid() {
			aboutRow(c, i18n.T("Email"), m.accountSession.User.Email)
			if m.accountSession.User.Name != "" {
				aboutRow(c, i18n.T("Display name"), m.accountSession.User.Name)
			}
			aboutRow(c, i18n.T("Relay server URL"), m.accountServer)
			text(c, i18n.T("Discovered environments"), 13, c.Theme().Text, 600)
			if len(m.accountEnvs) == 0 {
				faint(c, i18n.T("No environments found"), 12)
				faint(c, i18n.T("Open Access on a desktop with this account to register that computer."), 12)
			}
			for _, environment := range m.accountEnvs {
				accountEnvironmentCard(c, m, environment)
			}
			if m.accountErr != "" {
				errorNote(c, m.accountErr)
			}
			ui.Row(c).Wrap().Gap(8).Children(func() {
				ui.Button(c, i18n.T("Refresh")).Disabled(m.accountBusy).
					OnClick(func() { m.refreshRelayAccount(false) })
				ui.Button(c, i18n.T("Sign out")).Disabled(m.accountBusy).OnClick(m.signOutRelayAccount)
			})
			if m.accountBusy {
				ui.Spinner(c).Label(i18n.T("Loading environments"))
			}
			return
		}
		selected := i18n.T("Sign in")
		if m.accountMode == "register" {
			selected = i18n.T("Register")
		}
		segmentChoice(c, []string{i18n.T("Sign in"), i18n.T("Register")}, selected, func(choice string) {
			if m.accountBusy {
				return
			}
			m.accountErr = ""
			m.accountMode = "login"
			if choice == i18n.T("Register") {
				m.accountMode = "register"
			}
		})
		ui.Field(c, i18n.T("Relay server URL"), func() {
			ui.TextInput(c, &m.accountServer).Placeholder("https://relay.example.com").Disabled(m.accountBusy)
		})
		ui.Field(c, i18n.T("Email"), func() {
			ui.TextInput(c, &m.accountEmail).Placeholder("you@example.com").Disabled(m.accountBusy)
		})
		ui.Field(c, i18n.T("Password"), func() {
			ui.TextInput(c, &m.accountPassword).Password().Disabled(m.accountBusy)
		})
		if m.accountMode == "register" {
			ui.Field(c, i18n.T("Display name"), func() {
				ui.TextInput(c, &m.accountName).Placeholder(i18n.T("Optional display name")).Disabled(m.accountBusy)
			})
		}
		ui.Checkbox(c, &m.accountAllowHTTP, i18n.T("Allow insecure HTTP")).Disabled(m.accountBusy)
		if m.accountErr != "" {
			errorNote(c, m.accountErr)
		}
		label := i18n.T("Sign in and discover")
		if m.accountMode == "register" {
			label = i18n.T("Create account")
		}
		ui.PrimaryButton(c, label).Disabled(m.accountBusy).OnClick(m.signInRelayAccount)
		if m.accountBusy {
			ui.Spinner(c).Label(i18n.T("Signing in and discovering environments"))
		}
		faint(c, i18n.T("Environments are added automatically after sign-in. Direct LAN pairing remains available without an account."), 11.5)
	})
}

func accountEnvironmentCard(c *ui.Context, m *Manager, environment client.RelayEnvironment) {
	t := c.Theme()
	profile := m.find(environment.EnvironmentID)
	session := m.sessions[environment.EnvironmentID]
	if session != nil {
		profile = session.profile
	}
	name := m.environmentNames()[environment.EnvironmentID]
	if name == "" {
		name = strings.TrimSpace(environment.Name)
	}
	if name == "" {
		name = environment.EnvironmentID
	}
	ui.Column(c).FillWidth().Gap(10).Padding(14).Radius(10).
		Border(1, t.Border).Background(t.Surface).Children(func() {
		ui.Row(c).FillWidth().AlignItems(ui.Center).Gap(8).Children(func() {
			ui.Icon(c, lucide.Monitor()).FontSize(18).TextColor(t.TextMuted)
			ui.Text(c, name).FontSize(13).FontWeight(600).TextColor(t.Text).
				Grow(1).MinWidth(0).SingleLine().Tooltip(name)
			if m.current == environment.EnvironmentID {
				text(c, i18n.T("Active"), 10.5, t.Accent)
			}
		})
		ui.Row(c).Wrap().AlignItems(ui.Center).Gap(8).Children(func() {
			online, color := i18n.T("Relay offline"), t.TextMuted
			if environment.Online {
				online, color = i18n.T("Relay online"), t.Success
			}
			ui.Row(c).AlignItems(ui.Center).Gap(5).Children(func() {
				ui.Box(c).Size(6, 6).Radius(3).Background(color)
				text(c, online, 11, color)
			})
			connection := i18n.T("Not connected")
			if session != nil {
				connection = session.connectionLabel()
				if session.settingsConnectionReady() {
					connection = i18n.T("Connected") + " · " + connection
				}
			}
			text(c, connection, 11, t.TextMuted)
		})
		if profile.Hostname != "" {
			accountEnvironmentInfo(c, i18n.T("Hostname"), profile.Hostname)
		}
		if system := profile.System; system != nil {
			value := strings.TrimSpace(system.OS + " " + system.OSVersion)
			if system.Arch != "" {
				value = strings.TrimSpace(value + " · " + system.Arch)
			}
			if value != "" {
				accountEnvironmentInfo(c, i18n.T("System"), value)
			}
		}
		relayProfile := client.RelayAccountProfile(relayAccountServer(m.accountServer), "", environment)
		accountEnvironmentInfo(c, i18n.T("Relay address"), relayProfile.Routes[0].URL)
		seen := map[string]bool{}
		direct := append([]string(nil), environment.DirectURLs...)
		for _, route := range profile.Routes {
			if route.Kind == "direct" {
				direct = append(direct, route.URL)
			}
		}
		for _, address := range direct {
			if address != "" && !seen[address] {
				seen[address] = true
				accountEnvironmentInfo(c, i18n.T("Direct address"), address)
			}
		}
		accountEnvironmentInfo(c, i18n.T("Environment ID"), environment.EnvironmentID)
		if session != nil && session.connErr != "" {
			errorNote(c, session.connErr)
		}
		label := i18n.T("Open")
		if session != nil && !session.settingsConnectionReady() {
			label = i18n.T("Reconnect")
		}
		ui.Button(c, label).Label(i18n.T("Open environment") + " " + name).
			Disabled(session == nil || session.connecting).OnClick(func() {
			m.openAccountEnvironment(environment.EnvironmentID)
		})
	})
}

func accountEnvironmentInfo(c *ui.Context, label, value string) {
	ui.Column(c).FillWidth().Gap(3).Children(func() {
		faint(c, label, 10.5)
		ui.Text(c, value).FontSize(11.5).TextColor(c.Theme().Text).
			FillWidth().MinWidth(0).MaxLines(3).Selectable()
	})
}

func (m *Manager) openAccountEnvironment(id string) {
	next := m.sessions[id]
	if next == nil || next.connecting {
		return
	}
	if m.active != nil {
		m.active.showSettings = false
	}
	if !next.settingsConnectionReady() {
		next.connectProfile(next.profile)
	}
	m.activate(id)
	m.accountOpen = false
	next.showSettings = false
	next.mobileSettingsDetail = ""
	next.mobilePage = "threads"
}
