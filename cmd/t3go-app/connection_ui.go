package main

import (
	"strings"
	"time"

	"github.com/oneo-me/nsgo/base"
	"github.com/oneo-me/nsgo/core"
	"github.com/oneo-me/nsgo/icons/lucide"
	"github.com/oneo-me/nsgo/ui"
	"github.com/tk/t3go/internal/i18n"
)

// Status has its own reactive boundary so transport changes keep input focus.
func (a *app) environmentStatus(cx *core.Scope) core.Node {
	colors := ui.ColorsOf(cx)
	return core.View{Style: core.Style{Direction: core.Row, Gap: 5, MinWidth: core.Pt(0)}, Children: []core.Node{
		core.Text{Content: a.envName(), TestID: "current-environment-name", MaxLines: 1,
			Style:     core.Style{FontSize: 10, FlexShrink: 1, MinWidth: core.Pt(0)},
			StyleFunc: func() core.Style { return core.Style{Color: colors.Muted()} }},
		core.Text{Content: a.connectionLabel(), TestID: "connection-status", MaxLines: 1,
			Style:     core.Style{FontSize: 10, FlexShrink: 0},
			StyleFunc: func() core.Style { return core.Style{Color: colors.Muted()} }},
	}}
}

func (a *app) connectionLabel() string {
	_, label := a.connectionStatus()
	return label
}

// connectionStatus reports the connection's state and the label that
// shows it. The key stays the same in every language, so the interface
// can tell the states apart.
func (a *app) connectionStatus() (string, string) {
	if a.connecting.Get() {
		return "connecting", i18n.T("Connecting…")
	}
	if a.transportBlocked.Get() {
		return "blocked", i18n.T("Authorization required")
	}
	if a.offline.Get() {
		return "reconnecting", i18n.T("Reconnecting…")
	}
	if !a.connected.Get() {
		return "offline", i18n.T("Not connected")
	}
	switch routeKind(a.profile.Routes, a.serverURL.Get()) {
	case "relay":
		return "relay", "Relay"
	case "direct":
		return "direct", i18n.T("Direct")
	default:
		return "connected", i18n.T("Connected")
	}
}

func (a *app) connectionBanner(cx *core.Scope) core.Node {
	colors := ui.ColorsOf(cx)
	var children []core.Node
	testID := "offline"
	style := core.Style{Direction: core.Row, Wrap: core.Wrap, AlignItems: core.AlignCenter, Gap: 8,
		Padding: core.Symmetric(6, 12), Background: colors.Inset(), FlexShrink: 0}
	text := func(message string) core.Node {
		return core.Text{Content: message, Style: core.Style{FontSize: 12, FlexGrow: 1, FlexShrink: 1, MinWidth: core.Pt(0)},
			StyleFunc: func() core.Style { return core.Style{Color: colors.Text()} }}
	}
	switch {
	case a.connecting.Get():
		children = []core.Node{ui.Spinner{Size: 12, Label: i18n.T("Connecting")}, text(i18n.T("Checking the environment connection…"))}
	case a.transportBlocked.Get():
		testID = "authorization-required"
		children = []core.Node{
			ui.Icon(lucide.KeyRound, 16, colors.Danger),
			text(i18n.T("Environment access was rejected. Update the token to reconnect.")),
			ui.Button{Text: i18n.T("Update token"), TestID: "update-credentials", Size: controlSize(cx),
				Variant: ui.ButtonSecondary, OnPress: a.openCredentials},
		}
	case a.offline.Get():
		children = []core.Node{ui.Spinner{Size: 12, Label: i18n.T("Reconnecting")},
			text(i18n.T("Connection lost. Reconnecting…")),
			ui.Button{Text: i18n.T("Retry now"), TestID: "retry-connection", Size: controlSize(cx),
				Variant: ui.ButtonGhost, OnPress: func() { a.connectProfile(a.profile) }},
		}
	case a.connectionRecovered.Get():
		testID = "connection-restored"
		children = []core.Node{ui.Icon(lucide.Check, 16, colors.Accent), text(i18n.T("Connection restored."))}
	default:
		return core.View{}
	}
	return core.View{TestID: testID, Style: style, Children: children}
}

func (a *app) markOnline() {
	wasOffline := a.offline.GetUntracked()
	a.offline.Set(false)
	if wasOffline && a.connected.GetUntracked() {
		a.connectionRecovered.Set(true)
		if a.recoveryNoticeCancel != nil {
			a.recoveryNoticeCancel()
		}
		a.recoveryNoticeCancel = a.cx.AfterFunc(4*time.Second, func() {
			a.connectionRecovered.Set(false)
			a.recoveryNoticeCancel = nil
		})
	}
}

func (a *app) environmentContext(cx *core.Scope, testID string) core.Node {
	return core.Dynamic(func(cx *core.Scope) core.Node {
		return core.Text{Content: i18n.T("Environment: ") + a.envName(), TestID: testID, MaxLines: 1,
			Style:     core.Style{FontSize: 12, FontWeight: ui.Semibold, FlexShrink: 0},
			StyleFunc: func() core.Style { return core.Style{Color: ui.ColorsOf(cx).Muted()} }}
	})
}

func (a *app) openCredentials() {
	a.credentialToken.Set("")
	a.credentialError.Set("")
	a.showCredentials.Set(true)
}

func (a *app) closeCredentials() {
	if a.connecting.GetUntracked() {
		return
	}
	a.showCredentials.Set(false)
	a.credentialToken.Set("")
	a.credentialError.Set("")
}

func (a *app) updateCredentials() {
	if a.connecting.GetUntracked() {
		return
	}
	token := strings.TrimSpace(a.credentialToken.GetUntracked())
	if token == "" {
		a.credentialError.Set(i18n.T("Enter the environment token."))
		return
	}
	a.credentialError.Set("")
	profile := a.profile
	profile.Token = token
	a.connectProfile(profile)
}

func (a *app) credentialsDialog(cx *core.Scope) core.Node {
	return ui.Dialog{Open: a.showCredentials.Get,
		OnOpenChange: func(open bool, _ base.Reason) {
			if !open {
				a.closeCredentials()
			}
		},
		NoCloseOnBackdropPress: compactSettings(cx), Title: i18n.T("Update environment token"),
		Description: i18n.T("Test the new token before saving it. Your drafts and queued messages are kept."),
		TestID:      "credentials-dialog", Style: dialogWidth(cx, 480),
		Children: []core.Node{
			a.environmentContext(cx, "credentials-environment"),
			mobileInput{Scope: cx, Input: base.Input{Value: a.credentialToken.Get, OnChange: a.credentialToken.Set,
				Label: i18n.T("New environment token"), Placeholder: i18n.T("Paste the environment token"), Masked: true,
				SubmitBehavior: core.SubmitBehaviorSubmit, OnSubmit: func(string) { a.updateCredentials() }}},
			core.Show{When: a.connecting.Get, Then: func(*core.Scope) core.Node {
				return ui.Text(i18n.T("Checking connection…"), 12, ui.Regular, ui.ColorsOf(cx).Muted)
			}},
		},
		Footer: []core.Node{
			ui.Button{Text: i18n.T("Cancel"), TestID: "credentials-cancel", Variant: ui.ButtonGhost,
				Size: controlSize(cx), Disabled: a.connecting.Get, OnPress: a.closeCredentials},
			ui.Button{Text: i18n.T("Save and reconnect"), TestID: "credentials-save", Size: controlSize(cx),
				Disabled: a.connecting.Get, OnPress: a.updateCredentials},
		}}
}
