package main

import (
	"net"
	"net/url"
	"strings"

	"github.com/oneo-me/nsgo/base"
	"github.com/oneo-me/nsgo/core"
	"github.com/oneo-me/nsgo/icons/lucide"
	"github.com/oneo-me/nsgo/ui"
	qrcode "github.com/skip2/go-qrcode"
	"github.com/tk/t3go/internal/client"
	"github.com/tk/t3go/internal/proto"
)

// Access belongs to the server running on this machine. A loopback direct
// route is the durable local profile signal; relay-only environments are
// always remote from the client's point of view.
func isLocalEnvironmentProfile(profile client.EnvironmentProfile) bool {
	for _, route := range profile.Routes {
		if route.Kind == "relay" {
			continue
		}
		raw := strings.TrimRight(strings.TrimSpace(route.URL), "/")
		u, err := url.Parse(raw)
		if err != nil {
			continue
		}
		host := strings.ToLower(u.Hostname())
		if host == "localhost" || strings.HasSuffix(host, ".localhost") {
			return true
		}
		if ip := net.ParseIP(host); ip != nil && ip.IsLoopback() {
			return true
		}
	}
	return false
}

// localSettingsTarget never follows the environment chips. Access and About
// stay bound to the active local machine even while a remote thread is open.
func (a *app) localSettingsTarget() *app {
	if a.manager == nil {
		if isLocalEnvironmentProfile(a.profile) {
			return a
		}
		return nil
	}
	for _, candidate := range []*app{a.manager.app, a.manager.sessions[a.manager.defaultID]} {
		if candidate != nil && isLocalEnvironmentProfile(candidate.profile) {
			return candidate
		}
	}
	for _, profile := range a.manager.profiles.GetUntracked() {
		if !isLocalEnvironmentProfile(profile) {
			continue
		}
		if target := a.manager.sessions[profile.ID]; target != nil {
			return target
		}
	}
	return nil
}

func accessRouteMode(settings proto.Settings) (direct, relay bool) {
	switch strings.ToLower(strings.TrimSpace(settings.AccessRoutes)) {
	case "direct":
		return true, false
	case "relay":
		return false, true
	default:
		return true, true
	}
}

func setAccessRouteMode(settings *proto.Settings, direct, relay bool) {
	switch {
	case direct && relay:
		settings.AccessRoutes = "both"
	case direct:
		settings.AccessRoutes = "direct"
	case relay:
		settings.AccessRoutes = "relay"
	}
}

func firstAccessRoute(profile client.EnvironmentProfile, kind string) (client.Route, bool) {
	for _, route := range profile.Routes {
		if route.Kind == kind {
			return route, true
		}
	}
	return client.Route{}, false
}

// refreshAccess builds the pairing link from the current environment's saved
// identity and routes. The link is only rendered after the environment's
// Access switch has been enabled.
func (a *app) refreshAccess() {
	a.accessLink.Set("")
	a.accessQR.Set(nil)
	a.accessErr.Set("")
	if !a.settingsDraft.GetUntracked().AccessEnabled {
		return
	}
	if !isLocalEnvironmentProfile(a.profile) {
		a.accessErr.Set("Access is only available for the environment running on this machine.")
		return
	}
	direct, relay := accessRouteMode(a.settingsDraft.GetUntracked())
	link, err := client.PairingLinkOptions(a.profile, direct, relay)
	if err != nil {
		a.accessErr.Set(err.Error())
		return
	}
	png, err := qrcode.Encode(link, qrcode.Medium, 320)
	if err != nil {
		a.accessErr.Set("Could not draw pairing QR code: " + err.Error())
		return
	}
	a.accessLink.Set(link)
	a.accessQR.Set(png)
}

func accessRouteWarning(link string) string {
	u, err := url.Parse(link)
	if err != nil {
		return ""
	}
	host := strings.ToLower(u.Query().Get("url"))
	parsed, err := url.Parse(host)
	if err != nil {
		return ""
	}
	name := strings.ToLower(parsed.Hostname())
	if name == "localhost" || name == "127.0.0.1" || name == "::1" {
		return "This direct route only works on this device. Enable Relay for pairing outside this machine."
	}
	return ""
}

func accessRouteSwitch(cx *core.Scope, label, testID string, checked func() bool, disabled func() bool,
	onChange func(bool)) core.Node {
	look := ui.NewSwitchLook(cx, checked, ui.SizeSmall)
	return base.Switch{Checked: checked, Disabled: disabled, OnChange: onChange, Label: label, TestID: testID,
		Style:          core.Style{Margin: core.All(-8), Padding: core.All(8)},
		TrackStyleFunc: look.Track, ThumbStyleFunc: look.Thumb}
}

func accessRouteRow(cx *core.Scope, label, detail, testID string, available bool, checked func() bool,
	disabled func() bool, onChange func(bool)) core.Node {
	colors := ui.ColorsOf(cx)
	var control core.Node
	if available {
		control = accessRouteSwitch(cx, label+" pairing route", testID, checked, disabled, onChange)
	} else {
		control = core.Text{Content: "Not configured", TestID: testID + "-unavailable",
			Style:     core.Style{FontSize: 11, FontWeight: ui.Medium},
			StyleFunc: func() core.Style { return core.Style{Color: colors.Faint()} }}
	}
	return core.View{TestID: testID + "-row", Style: core.Style{Direction: core.Row, AlignItems: core.AlignCenter,
		Gap: 12, Padding: core.Symmetric(8, 10), Background: colors.Inset(), CornerRadius: 8}, Children: []core.Node{
		core.View{Style: core.Style{Direction: core.Column, Gap: 2, FlexGrow: 1, FlexShrink: 1, MinWidth: core.Pt(0)},
			Children: []core.Node{
				core.Text{Content: label, Style: core.Style{FontSize: 12.5, FontWeight: ui.Semibold},
					StyleFunc: func() core.Style { return core.Style{Color: colors.Text()} }},
				core.Text{Content: detail, MaxLines: 2, Style: core.Style{FontSize: 10.5},
					StyleFunc: func() core.Style { return core.Style{Color: colors.Muted()} }},
			}},
		control,
	}}
}

// settingsAccess configures pairing only for the local environment.
func (a *app) settingsAccess(cx *core.Scope) core.Node {
	colors := ui.ColorsOf(cx)
	if !isLocalEnvironmentProfile(a.profile) {
		return core.View{TestID: "settings-access-local-only", Style: core.Style{Gap: 8}, Children: []core.Node{
			settingsSectionTitle(cx, "Access", "Device pairing is available only for the T3Go environment running on this machine."),
			ui.Text("Select the local environment to manage Access.", 12, ui.Regular, colors.Muted),
		}}
	}
	enabled := func() bool { return a.settingsDraft.Get().AccessEnabled }
	body := []core.Node{
		settingsSectionTitle(cx, "Access", "Pair another device with the local environment using the routes you choose."),
		core.View{Style: core.Style{Direction: core.Row, AlignItems: core.AlignCenter, Gap: 12,
			Padding: core.Symmetric(10, 12), Background: colors.Inset(), CornerRadius: 9}, Children: []core.Node{
			core.View{Style: core.Style{Direction: core.Column, Gap: 2, FlexGrow: 1, FlexShrink: 1, MinWidth: core.Pt(0)},
				Children: []core.Node{
					core.Text{Content: "Allow device pairing", TestID: "settings-access-title",
						Style:     core.Style{FontSize: 13, FontWeight: ui.Semibold},
						StyleFunc: func() core.Style { return core.Style{Color: colors.Text()} }},
					core.Text{Content: "Shows a pairing link and QR code for this machine. Credentials stay in local environment storage.",
						MaxLines: 3, Style: core.Style{FontSize: 11, LineHeight: 16},
						StyleFunc: func() core.Style { return core.Style{Color: colors.Muted()} }},
				}},
			settingsSwitch(cx, "Allow device pairing", "settings-access-enabled", enabled, func(on bool) {
				a.settingsDraft.Update(func(settings proto.Settings) proto.Settings {
					settings.AccessEnabled = on
					return settings
				})
				a.refreshAccess()
			}),
		}},
	}
	if !enabled() {
		return core.View{Style: core.Style{Direction: core.Column, Gap: 10}, Children: body}
	}
	directRoute, hasDirect := firstAccessRoute(a.profile, "direct")
	relayRoute, hasRelay := firstAccessRoute(a.profile, "relay")
	modeDirect, modeRelay := accessRouteMode(a.settingsDraft.Get())
	toggleRoute := func(direct bool, on bool) {
		a.settingsDraft.Update(func(settings proto.Settings) proto.Settings {
			currentDirect, currentRelay := accessRouteMode(settings)
			if direct {
				currentDirect = on
			} else {
				currentRelay = on
			}
			if currentDirect || currentRelay {
				setAccessRouteMode(&settings, currentDirect, currentRelay)
			}
			return settings
		})
		a.refreshAccess()
	}
	directDetail := "No direct route saved for this environment."
	if hasDirect {
		directDetail = directRoute.URL
	}
	relayDetail := "No relay route saved for this environment."
	if hasRelay {
		relayDetail = relayRoute.URL
	}
	body = append(body, core.View{TestID: "settings-access-routes",
		Style: core.Style{Direction: core.Column, Gap: 7}, Children: []core.Node{
			ui.Text("Pairing routes", 12, ui.Semibold, colors.Muted),
			accessRouteRow(cx, "Direct / LAN", directDetail, "settings-access-direct", hasDirect,
				func() bool { return modeDirect && hasDirect },
				func() bool { return modeDirect && hasDirect && (!modeRelay || !hasRelay) },
				func(on bool) { toggleRoute(true, on) }),
			accessRouteRow(cx, "Relay", relayDetail, "settings-access-relay", hasRelay,
				func() bool { return modeRelay && hasRelay },
				func() bool { return modeRelay && hasRelay && (!modeDirect || !hasDirect) },
				func(on bool) { toggleRoute(false, on) }),
		}})
	link := a.accessLink.Get()
	if a.accessErr.Get() != "" {
		body = append(body, ui.Text(a.accessErr.Get(), 11.5, ui.Regular, colors.Danger))
		return core.View{Style: core.Style{Direction: core.Column, Gap: 10}, Children: body}
	}
	if warning := accessRouteWarning(link); warning != "" {
		body = append(body, ui.Text(warning, 11, ui.Regular, colors.Danger))
	}
	body = append(body,
		core.View{TestID: "settings-access-pairing", Style: core.Style{Direction: core.Column, Gap: 8,
			Padding: core.Symmetric(10, 12), Background: colors.Inset(), CornerRadius: 9}, Children: []core.Node{
			core.View{Style: core.Style{Direction: core.Row, AlignItems: core.AlignCenter, Gap: 8}, Children: []core.Node{
				ui.Icon(lucide.KeyRound, 16, colors.Muted),
				core.Text{Content: "Pairing link", Style: core.Style{FontSize: 12, FontWeight: ui.Semibold},
					StyleFunc: func() core.Style { return core.Style{Color: colors.Text()} }},
				ui.Spacer{},
				ui.Button{Text: "Copy link", Icon: lucide.Copy, Variant: ui.ButtonSecondary,
					Size: controlSize(cx), TestID: "settings-access-copy", OnPress: func() { a.cx.SetClipboard(link) }},
			}},
			core.Text{Content: link, TestID: "settings-access-link", Selectable: core.SelectableOn, MaxLines: 4,
				Style:     core.Style{FontSize: 10.5, FontFamily: "Menlo, monospace", LineHeight: 15},
				StyleFunc: func() core.Style { return core.Style{Color: colors.Muted()} }},
		}},
	)
	if len(a.accessQR.Get()) > 0 {
		body = append(body, core.View{Style: core.Style{AlignItems: core.AlignCenter, Padding: core.All(8)},
			Children: []core.Node{ui.Image{Source: core.ImageSource{
				Data: a.accessQR.Get(), Size: core.Size{Width: 220, Height: 220},
			}, Label: "Environment pairing QR code", ResizeMode: core.Contain, TestID: "settings-access-qr",
				Style: core.Style{Width: core.Pt(220), Height: core.Pt(220), CornerRadius: 8}}}},
		)
	}
	return core.View{Style: core.Style{Direction: core.Column, Gap: 10}, Children: body}
}
