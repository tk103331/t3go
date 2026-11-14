package main

import (
	"strings"
	"time"

	"github.com/oneo-me/nsgo/base"
	"github.com/oneo-me/nsgo/core"
	"github.com/oneo-me/nsgo/icons/lucide"
	"github.com/oneo-me/nsgo/ui"
)

// The header owns its subscriptions, so connection changes keep the composer
// mounted and focused. An environment name always identifies the target machine.
func (a *app) environmentHeader(cx *core.Scope) core.Node {
	colors := ui.ColorsOf(cx)
	height := core.Points(32)
	if ui.Touch(cx) {
		height = 44
	}
	return core.View{Style: core.Style{Padding: core.Symmetric(4, 12), FlexShrink: 0,
		BorderWidth: 1, BorderColor: colors.Border()}, Children: []core.Node{
		base.Button{Label: "Switch environment", TestID: "environment-switcher",
			OnPress: func(core.PressEvent) { a.showEnvironments.Set(true) },
			Style: core.Style{Direction: core.Row, AlignItems: core.AlignCenter, Gap: 8,
				MinHeight: core.Pt(height), Width: core.Pct(100), MaxWidth: core.Pt(460), MinWidth: core.Pt(0),
				Padding: core.Symmetric(0, 8), CornerRadius: 6},
			StyleFunc: func() core.Style {
				return core.Style{Hover: &core.Style{Background: colors.Inset()}, Focus: ui.FocusRing(cx, -2)}
			},
			Children: []core.Node{
				ui.Icon(lucide.Network, 16, colors.Muted),
				core.Text{Content: a.envName(), MaxLines: 1, TestID: "current-environment-name",
					Style:     core.Style{FontSize: 13, FontWeight: ui.Semibold, FlexGrow: 1, FlexShrink: 1, MinWidth: core.Pt(0)},
					StyleFunc: func() core.Style { return core.Style{Color: colors.Text()} }},
				core.Text{Content: a.connectionLabel(), TestID: "connection-status", MaxLines: 1,
					Style:     core.Style{FontSize: 11, FlexShrink: 0},
					StyleFunc: func() core.Style { return core.Style{Color: colors.Muted()} }},
				ui.Icon(lucide.ChevronDown, 14, colors.Muted),
			}},
	}}
}

func (a *app) connectionLabel() string {
	if a.connecting.Get() {
		return "Connecting…"
	}
	if a.transportBlocked.Get() {
		return "Authorization required"
	}
	if a.offline.Get() {
		return "Reconnecting…"
	}
	switch routeKind(a.profile.Routes, a.serverURL.Get()) {
	case "relay":
		return "Relay"
	case "direct":
		return "Direct"
	default:
		return "Connected"
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
		children = []core.Node{ui.Spinner{Size: 12, Label: "Connecting"}, text("Checking the environment connection…")}
	case a.transportBlocked.Get():
		testID = "authorization-required"
		children = []core.Node{
			ui.Icon(lucide.KeyRound, 16, colors.Danger),
			text("Environment access was rejected. Update the token to reconnect."),
			ui.Button{Text: "Update token", TestID: "update-credentials", Size: controlSize(cx),
				Variant: ui.ButtonSecondary, OnPress: a.openCredentials},
		}
	case a.offline.Get():
		children = []core.Node{ui.Spinner{Size: 12, Label: "Reconnecting"},
			text("Connection lost. Reconnecting…"),
			ui.Button{Text: "Retry now", TestID: "retry-connection", Size: controlSize(cx),
				Variant: ui.ButtonGhost, OnPress: func() { a.connectProfile(a.profile) }},
		}
	case a.connectionRecovered.Get():
		testID = "connection-restored"
		children = []core.Node{ui.Icon(lucide.Check, 16, colors.Accent), text("Connection restored.")}
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
		return core.Text{Content: "Environment: " + a.envName(), TestID: testID, MaxLines: 1,
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
		a.credentialError.Set("Enter the environment token.")
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
		NoCloseOnBackdropPress: compactSettings(cx), Title: "Update environment token",
		Description: "Test the new token before saving it. Your drafts and queued messages are kept.",
		TestID:      "credentials-dialog", Style: dialogWidth(cx, 480),
		Children: []core.Node{
			a.environmentContext(cx, "credentials-environment"),
			mobileInput{Scope: cx, Input: base.Input{Value: a.credentialToken.Get, OnChange: a.credentialToken.Set,
				Label: "New environment token", Placeholder: "Paste the environment token", Masked: true,
				SubmitBehavior: core.SubmitBehaviorSubmit, OnSubmit: func(string) { a.updateCredentials() }}},
			core.Dynamic(func(cx *core.Scope) core.Node {
				if a.connecting.Get() {
					return ui.Text("Checking connection…", 12, ui.Regular, ui.ColorsOf(cx).Muted)
				}
				return ui.Text(a.credentialError.Get(), 12, ui.Regular, ui.ColorsOf(cx).Danger)
			}),
		},
		Footer: []core.Node{
			ui.Button{Text: "Cancel", TestID: "credentials-cancel", Variant: ui.ButtonGhost,
				Size: controlSize(cx), Disabled: a.connecting.Get, OnPress: a.closeCredentials},
			ui.Button{Text: "Save and reconnect", TestID: "credentials-save", Size: controlSize(cx),
				Disabled: a.connecting.Get, OnPress: a.updateCredentials},
		}}
}
