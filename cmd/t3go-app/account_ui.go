package main

import (
	"net/url"
	"slices"
	"strings"

	"github.com/oneo-me/nsgo/base"
	"github.com/oneo-me/nsgo/core"
	"github.com/oneo-me/nsgo/icons/lucide"
	"github.com/oneo-me/nsgo/ui"
	"github.com/tk/t3go/internal/client"
	"github.com/tk/t3go/internal/i18n"
)

func (m *environmentManager) relayAccountDialog(cx *core.Scope) core.Node {
	return ui.Dialog{
		Open: m.accountOpen.Get,
		OnOpenChange: func(open bool, _ base.Reason) {
			if !open && !m.accountBusy.Get() {
				m.accountOpen.Set(false)
			}
		},
		NoCloseOnBackdropPress: compactSettings(cx),
		Title:                  i18n.T("Relay account"),
		Description:            i18n.T("Sign in once, then discover and open every environment owned by this account."),
		TestID:                 "relay-account-dialog",
		Style:                  dialogWidth(cx, 560),
		Children: []core.Node{core.Scroll{StyleFunc: func() core.Style {
			height := (cx.Env().Window().Height - cx.Env().KeyboardHeight()) * 0.72
			return core.Style{MaxHeight: core.Pt(height)}
		}, Children: []core.Node{core.Dynamic(m.relayAccountPanel)}}},
		Footer: []core.Node{ui.Button{Text: i18n.T("Close"), Variant: ui.ButtonGhost, Size: controlSize(cx),
			Disabled: m.accountBusy.Get, OnPress: func() { m.accountOpen.Set(false) }}},
	}
}

func (m *environmentManager) relayAccountPanel(cx *core.Scope) core.Node {
	colors := ui.ColorsOf(cx)
	session := m.accountSession.Get()
	if session.Token != "" && m.accountSessionValid() {
		return m.signedInRelayAccount(cx, colors)
	}
	mode := strings.ToLower(strings.TrimSpace(m.accountMode.Get()))
	if mode != "register" {
		mode = "login"
	}
	body := []core.Node{
		core.Text{Content: i18n.T("Use one Relay account to find your computers across networks."),
			Style:     core.Style{FontSize: 13, FontWeight: ui.Semibold},
			StyleFunc: func() core.Style { return core.Style{Color: colors.Text()} }},
		ui.Text(i18n.T("Environments are added automatically after sign-in. Direct LAN pairing remains available without an account."), 12, ui.Regular, colors.Muted),
		core.View{Style: core.Style{Direction: core.Row, Gap: 6}, Children: []core.Node{
			ui.Button{Text: i18n.T("Sign in"), Variant: func() ui.ButtonVariant {
				if mode == "login" {
					return ui.ButtonPrimary
				}
				return ui.ButtonSecondary
			}(), Size: controlSize(cx), TestID: "relay-account-mode-login",
				OnPress: func() { m.accountMode.Set("login"); m.accountErr.Set("") }},
			ui.Button{Text: i18n.T("Create account"), Variant: func() ui.ButtonVariant {
				if mode == "register" {
					return ui.ButtonPrimary
				}
				return ui.ButtonSecondary
			}(), Size: controlSize(cx), TestID: "relay-account-mode-register",
				OnPress: func() { m.accountMode.Set("register"); m.accountErr.Set("") }},
		}},
		mobileInput{Scope: cx, Input: base.Input{Value: m.accountServer.Get, OnChange: m.accountServer.Set,
			Placeholder: "https://relay.example.com", Label: i18n.T("Relay server URL"),
			NoAutoCorrect: true, AutoCapitalize: core.AutoCapitalizeNone},
			TestID: "relay-account-server"},
		mobileInput{Scope: cx, Input: base.Input{Value: m.accountEmail.Get, OnChange: m.accountEmail.Set,
			Placeholder: "you@example.com", Label: i18n.T("Email"),
			NoAutoCorrect: true, AutoCapitalize: core.AutoCapitalizeNone},
			TestID: "relay-account-email"},
		mobileInput{Scope: cx, Input: base.Input{Value: m.accountPassword.Get, OnChange: m.accountPassword.Set,
			Placeholder: i18n.T("At least 8 characters"), Label: i18n.T("Password"), Masked: true,
			NoAutoCorrect: true, AutoCapitalize: core.AutoCapitalizeNone,
			SubmitBehavior: core.SubmitBehaviorSubmit, OnSubmit: func(string) { m.signInRelayAccount() }},
			TestID: "relay-account-password"},
	}
	if mode == "register" {
		body = append(body, mobileInput{Scope: cx, Input: base.Input{Value: m.accountName.Get,
			OnChange: m.accountName.Set, Placeholder: i18n.T("Optional display name"), Label: i18n.T("Display name"),
			AutoCapitalize: core.AutoCapitalizeWords}, TestID: "relay-account-name"})
	}
	body = append(body,
		core.View{Style: core.Style{Direction: core.Row, AlignItems: core.AlignCenter, Gap: 10}, Children: []core.Node{
			core.View{Style: core.Style{Direction: core.Column, Gap: 1, FlexGrow: 1, FlexShrink: 1, MinWidth: core.Pt(0)},
				Children: []core.Node{
					core.Text{Content: i18n.T("Allow insecure HTTP"), Style: core.Style{FontSize: 11.5, FontWeight: ui.Medium},
						StyleFunc: func() core.Style { return core.Style{Color: colors.Text()} }},
					ui.Text(i18n.T("Use only for a trusted private Relay."), 10.5, ui.Regular, colors.Faint),
				}},
			settingsSwitch(cx, i18n.T("Allow insecure HTTP Relay"), "relay-account-allow-http",
				m.accountAllowHTTP.Get, m.accountAllowHTTP.Set),
		}},
	)
	if m.accountErr.Get() != "" {
		body = append(body, core.Text{Content: m.accountErr.Get(), TestID: "relay-account-error",
			MaxLines: 5, Style: core.Style{FontSize: 11.5},
			StyleFunc: func() core.Style { return core.Style{Color: colors.Danger()} }})
	}
	if m.accountBusy.Get() {
		body = append(body, ui.Spinner{Size: 16, Label: i18n.T("Signing in and discovering environments")})
	} else {
		body = append(body, ui.Button{Text: func() string {
			if mode == "register" {
				return i18n.T("Create account and discover")
			}
			return i18n.T("Sign in and discover")
		}(), Icon: lucide.LogIn, Variant: ui.ButtonPrimary, Size: controlSize(cx),
			TestID: "relay-account-submit", OnPress: m.signInRelayAccount})
	}
	return core.View{Style: core.Style{Gap: 10}, Children: body}
}

// relayEnvironmentDetail is what tells two discoveries apart: where the
// machine answers, or, for a relay-only registration, its short id.
func relayEnvironmentDetail(environment client.RelayEnvironment) string {
	for _, raw := range environment.DirectURLs {
		u, err := url.Parse(strings.TrimSpace(raw))
		if err != nil || u.Host == "" {
			continue
		}
		return u.Host
	}
	return shortID(environment.EnvironmentID)
}

func (m *environmentManager) signedInRelayAccount(cx *core.Scope, colors ui.Colors) core.Node {
	session := m.accountSession.Get()
	environments := m.accountEnvs.Get()
	// The environment that answers now comes first: the stale registrations
	// a machine left behind when its data was reset sink below it.
	ordered := append([]client.RelayEnvironment(nil), environments...)
	slices.SortStableFunc(ordered, func(a, b client.RelayEnvironment) int {
		if a.Online != b.Online {
			if a.Online {
				return -1
			}
			return 1
		}
		if a.Name != b.Name {
			return strings.Compare(a.Name, b.Name)
		}
		return strings.Compare(a.EnvironmentID, b.EnvironmentID)
	})
	var rows []core.Node
	for _, environment := range ordered {
		environment := environment
		status := i18n.T("Offline")
		statusColor := colors.Faint()
		if environment.Online {
			status, statusColor = i18n.T("Online"), colors.Up()
		}
		detail := status + " · " + relayEnvironmentDetail(environment)
		if environment.EnvironmentID == m.localID {
			detail += " · " + i18n.T("This machine")
		}
		rows = append(rows, core.View{TestID: "relay-account-environment-" + environment.EnvironmentID,
			Style: core.Style{Direction: core.Row, AlignItems: core.AlignCenter, Gap: 10,
				Padding: core.Symmetric(9, 11), Background: colors.Inset(), CornerRadius: 8},
			Children: []core.Node{
				ui.Icon(lucide.Monitor, 20, colors.Muted),
				core.View{Style: core.Style{Direction: core.Column, Gap: 2, FlexGrow: 1, FlexShrink: 1, MinWidth: core.Pt(0)},
					Children: []core.Node{
						core.Text{Content: environment.Name, MaxLines: 1,
							Style:     core.Style{FontSize: 13, FontWeight: ui.Semibold},
							StyleFunc: func() core.Style { return core.Style{Color: colors.Text()} }},
						core.Text{Content: detail, TestID: "relay-account-environment-detail-" + environment.EnvironmentID,
							MaxLines: 1, Style: core.Style{FontSize: 10.5},
							StyleFunc: func() core.Style { return core.Style{Color: statusColor} }},
					}},
				ui.Button{Text: i18n.T("Open"), Variant: ui.ButtonSecondary, Size: controlSize(cx),
					TestID: "relay-account-open-environment-" + environment.EnvironmentID,
					OnPress: func() {
						m.activate(environment.EnvironmentID)
						m.accountOpen.Set(false)
					}},
				ui.IconButton{Icon: lucide.Trash, Label: i18n.T("Remove environment"), Variant: ui.IconButtonGhost,
					Size: iconButtonSize(cx), TestID: "relay-account-remove-environment-" + environment.EnvironmentID,
					Disabled: m.accountBusy.Get,
					OnPress:  func() { m.accountRemove.Set(environment.EnvironmentID) }},
			}})
	}
	if len(rows) == 0 {
		rows = append(rows, core.View{Style: core.Style{Gap: 4, Padding: core.All(14),
			Background: colors.Inset(), CornerRadius: 8}, Children: []core.Node{
			ui.Text(i18n.T("No environments found"), 13, ui.Semibold, colors.Text),
			ui.Text(i18n.T("Open Access on a desktop with this account to register that computer."), 12, ui.Regular, colors.Muted),
		}})
	}
	return core.View{TestID: "relay-account-signed-in", Style: core.Style{Gap: 10}, Children: []core.Node{
		core.View{Style: core.Style{Direction: core.Row, AlignItems: core.AlignCenter, Gap: 10,
			Padding: core.Symmetric(10, 12), Background: colors.Inset(), CornerRadius: 9}, Children: []core.Node{
			ui.Icon(lucide.UserRound, 22, colors.Accent),
			core.View{Style: core.Style{Direction: core.Column, Gap: 2, FlexGrow: 1, FlexShrink: 1, MinWidth: core.Pt(0)},
				Children: []core.Node{
					core.Text{Content: session.User.Name, TestID: "relay-account-name-label", MaxLines: 1,
						Style:     core.Style{FontSize: 13, FontWeight: ui.Semibold},
						StyleFunc: func() core.Style { return core.Style{Color: colors.Text()} }},
					core.Text{Content: session.User.Email + " · " + m.accountServer.Get(), MaxLines: 2,
						Style:     core.Style{FontSize: 10.5},
						StyleFunc: func() core.Style { return core.Style{Color: colors.Muted()} }},
				}},
		}},
		core.View{Style: core.Style{Direction: core.Row, Gap: 6, Wrap: core.Wrap}, Children: []core.Node{
			ui.Button{Text: i18n.T("Refresh"), Icon: lucide.RefreshCw, Variant: ui.ButtonSecondary,
				Size: controlSize(cx), Disabled: m.accountBusy.Get, TestID: "relay-account-refresh",
				OnPress: func() { m.refreshRelayAccount(false) }},
			ui.Button{Text: i18n.T("Sign out"), Icon: lucide.LogOut, Variant: ui.ButtonGhost,
				Size: controlSize(cx), Disabled: m.accountBusy.Get, TestID: "relay-account-sign-out",
				OnPress: m.signOutRelayAccount},
		}},
		ui.Text(i18n.T("Discovered environments"), 12, ui.Semibold, colors.Muted),
		core.View{Style: core.Style{Gap: 6}, Children: rows},
		ui.Text(i18n.T("Account access is signed by Relay for each request. The account token is not the environment token used by LAN pairing."), 10.5, ui.Regular, colors.Faint),
		m.removeRelayEnvironmentDialog(cx),
	}}
}

// removeRelayEnvironmentDialog confirms forgetting one discovered
// environment, so a list of stale registrations can be cleared.
func (m *environmentManager) removeRelayEnvironmentDialog(cx *core.Scope) core.Node {
	colors := ui.ColorsOf(cx)
	id := m.accountRemove.Get()
	return ui.AlertDialog{Open: func() bool { return id != "" }, OnOpenChange: func(open bool, _ base.Reason) {
		if !open {
			m.accountRemove.Set("")
		}
	}, Title: i18n.T("Remove this environment?"), TestID: "relay-account-remove-dialog",
		Description: i18n.T("The account will no longer find this computer over Relay until it registers again. Its projects, threads and this device's direct access stay as they are."),
		Actions: []ui.AlertAction{
			{Text: i18n.T("Cancel"), Variant: ui.ButtonGhost, TestID: "relay-account-remove-cancel",
				OnPress: func() { m.accountRemove.Set("") }},
			{Text: i18n.T("Remove"), Variant: ui.ButtonPrimary, Color: func() core.Color { return colors.Danger() },
				TestID: "relay-account-remove-confirm", OnPress: func() {
					m.accountRemove.Set("")
					m.removeRelayEnvironment(id)
				}},
		}}
}
