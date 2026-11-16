package main

import (
	"context"
	"net"
	"net/url"
	"strings"
	"time"

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

func (a *app) applyRelayAccess(status proto.RelayAccessStatus) {
	a.relayAccess.Set(status)
	if !status.Configured || strings.TrimSpace(status.Route) == "" {
		return
	}
	route := client.Route{URL: strings.TrimRight(status.Route, "/"), Kind: "relay"}
	found := false
	for _, existing := range a.profile.Routes {
		if existing.URL == route.URL {
			found = true
			break
		}
	}
	if !found {
		a.profile.Routes = append(a.profile.Routes, route)
		if a.manager != nil {
			a.manager.update(a.profile)
		}
	}
	a.refreshAccess()
}

func (a *app) refreshRelayAccess() {
	if !a.connected.GetUntracked() || !isLocalEnvironmentProfile(a.profile) {
		return
	}
	c := a.client()
	if c == nil {
		return
	}
	a.relaySeq++
	seq := a.relaySeq
	a.relayErr.Set("")
	go func() {
		ctx, cancel := context.WithTimeout(a.lifetime, 10*time.Second)
		defer cancel()
		status, err := c.RelayAccess(ctx)
		a.cx.Post(func() {
			if seq != a.relaySeq {
				return
			}
			if err != nil {
				a.relayErr.Set(friendlyError(err))
				return
			}
			a.applyRelayAccess(status)
		})
	}()
}

func (a *app) configureRelay() {
	if a.relayBusy.GetUntracked() {
		return
	}
	c := a.client()
	if c == nil {
		a.relayErr.Set("Connect the local environment before configuring Relay.")
		return
	}
	in := proto.RelayAccessConfig{
		Server:    strings.TrimSpace(a.relayServer.GetUntracked()),
		DirectURL: strings.TrimSpace(a.relayDirectURL.GetUntracked()),
		AllowHTTP: a.relayAllowHTTP.GetUntracked(),
	}
	if in.Server == "" {
		a.relayErr.Set("Enter the Relay server URL.")
		return
	}
	email := strings.TrimSpace(a.relayEmail.GetUntracked())
	password := a.relayPassword.GetUntracked()
	if email == "" {
		a.relayErr.Set("Enter your Relay account email.")
		return
	}
	if password == "" {
		a.relayErr.Set("Enter your Relay account password.")
		return
	}
	a.relayBusy.Set(true)
	a.relayErr.Set("")
	authMode := strings.ToLower(strings.TrimSpace(a.relayAuthMode.GetUntracked()))
	account := proto.RelayAccountRequest{
		Server: in.Server, Email: email, Password: password,
		Name: strings.TrimSpace(a.relayName.GetUntracked()), AllowHTTP: in.AllowHTTP,
	}
	a.relaySeq++
	seq := a.relaySeq
	go func() {
		ctx, cancel := context.WithTimeout(a.lifetime, 45*time.Second)
		defer cancel()
		var session proto.RelayUserSession
		var err error
		if authMode == "register" {
			session, err = c.RegisterRelayUser(ctx, account)
		} else {
			session, err = c.LoginRelayUser(ctx, account)
		}
		if err == nil {
			in.UserToken = session.Token
		}
		var status proto.RelayAccessStatus
		if err == nil {
			status, err = c.ConfigureRelay(ctx, in)
		}
		a.cx.Post(func() {
			a.relayBusy.Set(false)
			if seq != a.relaySeq {
				return
			}
			if err != nil {
				a.relayErr.Set(friendlyError(err))
				return
			}
			a.relayPassword.Set("")
			a.relayConfigOpen.Set(false)
			a.settingsDraft.Update(func(settings proto.Settings) proto.Settings {
				settings.AccessRoutes = "both"
				return settings
			})
			a.applyRelayAccess(status)
			a.scheduleRelayStatusRefresh()
		})
	}()
}

// Registration returns before the WebSocket tunnel finishes connecting. Poll a
// few times so Access can move from Connecting to Online without reopening it.
func (a *app) scheduleRelayStatusRefresh() {
	for _, delay := range []time.Duration{750 * time.Millisecond, 2 * time.Second, 5 * time.Second} {
		go func(delay time.Duration) {
			timer := time.NewTimer(delay)
			defer timer.Stop()
			select {
			case <-a.lifetime.Done():
				return
			case <-timer.C:
			}
			a.cx.Post(func() {
				if a.settingsSection.GetUntracked() == "access" {
					a.refreshRelayAccess()
				}
			})
		}(delay)
	}
}

func relayAccessStateLabel(status proto.RelayAccessStatus) string {
	switch strings.ToLower(strings.TrimSpace(status.State)) {
	case "connecting":
		return "Connecting"
	case "retrying":
		return "Reconnecting"
	case "active":
		return "Online"
	case "error":
		return "Connection error"
	default:
		return "Configured"
	}
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

func (a *app) relayConfigurationCard(cx *core.Scope) core.Node {
	colors := ui.ColorsOf(cx)
	open := func() bool { return a.relayConfigOpen.Get() }
	authMode := strings.ToLower(strings.TrimSpace(a.relayAuthMode.Get()))
	if authMode != "register" {
		authMode = "login"
	}
	header := core.View{Style: core.Style{Direction: core.Row, AlignItems: core.AlignCenter, Gap: 8}, Children: []core.Node{
		core.View{Style: core.Style{Direction: core.Column, Gap: 2, FlexGrow: 1, FlexShrink: 1, MinWidth: core.Pt(0)},
			Children: []core.Node{
				core.Text{Content: "Connect Relay", Style: core.Style{FontSize: 12.5, FontWeight: ui.Semibold},
					StyleFunc: func() core.Style { return core.Style{Color: colors.Text()} }},
				core.Text{Content: "Sign in with a Relay account to register this machine outside its local network.",
					MaxLines: 2, Style: core.Style{FontSize: 10.5},
					StyleFunc: func() core.Style { return core.Style{Color: colors.Muted()} }},
			}},
		ui.Button{Text: func() string {
			if open() {
				return "Cancel"
			}
			return "Configure"
		}(), Variant: ui.ButtonSecondary, Size: controlSize(cx), TestID: "settings-relay-config-open",
			OnPress: func() { a.relayConfigOpen.Set(!open()) }},
	}}
	if !open() {
		return core.View{TestID: "settings-relay-config", Style: core.Style{Direction: core.Column, Gap: 7,
			Padding: core.Symmetric(9, 11), Background: colors.Inset(), CornerRadius: 8}, Children: []core.Node{header}}
	}
	body := []core.Node{
		header,
		core.View{TestID: "settings-relay-auth-mode", Style: core.Style{Direction: core.Row, Gap: 6}, Children: []core.Node{
			ui.Button{Text: "Sign in", Variant: func() ui.ButtonVariant {
				if authMode == "login" {
					return ui.ButtonPrimary
				}
				return ui.ButtonSecondary
			}(), Size: controlSize(cx), TestID: "settings-relay-auth-login",
				OnPress: func() {
					a.relayAuthMode.Set("login")
					a.relayErr.Set("")
				}},
			ui.Button{Text: "Create account", Variant: func() ui.ButtonVariant {
				if authMode == "register" {
					return ui.ButtonPrimary
				}
				return ui.ButtonSecondary
			}(), Size: controlSize(cx), TestID: "settings-relay-auth-register",
				OnPress: func() {
					a.relayAuthMode.Set("register")
					a.relayErr.Set("")
				}},
		}},
		mobileInput{Scope: cx, Input: base.Input{Value: a.relayServer.Get, OnChange: func(value string) {
			a.relayServer.Set(value)
		}, Placeholder: "https://relay.example.com", Label: "Relay server URL",
			NoAutoCorrect: true, AutoCapitalize: core.AutoCapitalizeNone},
			TestID: "settings-relay-server"},
		mobileInput{Scope: cx, Input: base.Input{Value: a.relayEmail.Get, OnChange: func(value string) {
			a.relayEmail.Set(value)
		}, Placeholder: "you@example.com", Label: "Relay account email",
			NoAutoCorrect: true, AutoCapitalize: core.AutoCapitalizeNone},
			TestID: "settings-relay-email"},
		mobileInput{Scope: cx, Input: base.Input{Value: a.relayPassword.Get, OnChange: func(value string) {
			a.relayPassword.Set(value)
		}, Placeholder: "At least 8 characters", Label: "Relay account password",
			Masked: true, NoAutoCorrect: true, AutoCapitalize: core.AutoCapitalizeNone},
			TestID: "settings-relay-password"},
	}
	if authMode == "register" {
		body = append(body, mobileInput{Scope: cx, Input: base.Input{Value: a.relayName.Get, OnChange: func(value string) {
			a.relayName.Set(value)
		}, Placeholder: "Optional display name", Label: "Account display name",
			NoAutoCorrect: true, AutoCapitalize: core.AutoCapitalizeWords},
			TestID: "settings-relay-name"})
	}
	body = append(body,
		mobileInput{Scope: cx, Input: base.Input{Value: a.relayDirectURL.Get, OnChange: func(value string) {
			a.relayDirectURL.Set(value)
		}, Placeholder: "Optional LAN URL, for example http://192.168.1.20:4870", Label: "Direct LAN URL",
			NoAutoCorrect: true, AutoCapitalize: core.AutoCapitalizeNone},
			TestID: "settings-relay-direct-url"},
		core.View{Style: core.Style{Direction: core.Row, AlignItems: core.AlignCenter, Gap: 10}, Children: []core.Node{
			core.View{Style: core.Style{Direction: core.Column, Gap: 1, FlexGrow: 1, FlexShrink: 1, MinWidth: core.Pt(0)},
				Children: []core.Node{
					core.Text{Content: "Allow insecure HTTP", Style: core.Style{FontSize: 11.5, FontWeight: ui.Medium},
						StyleFunc: func() core.Style { return core.Style{Color: colors.Text()} }},
					core.Text{Content: "Use only for a trusted private network.", MaxLines: 2,
						Style:     core.Style{FontSize: 10.5},
						StyleFunc: func() core.Style { return core.Style{Color: colors.Faint()} }},
				}},
			settingsSwitch(cx, "Allow insecure HTTP Relay", "settings-relay-allow-http", a.relayAllowHTTP.Get,
				func(value bool) { a.relayAllowHTTP.Set(value) }),
		}},
	)
	if a.relayErr.Get() != "" {
		body = append(body, core.Text{Content: a.relayErr.Get(), TestID: "settings-relay-error", MaxLines: 4,
			Style:     core.Style{FontSize: 10.5},
			StyleFunc: func() core.Style { return core.Style{Color: colors.Danger()} }})
	}
	if a.relayBusy.Get() {
		body = append(body, ui.Spinner{Size: 14, Label: "Authenticating and registering Relay"})
	} else {
		body = append(body, ui.Button{Text: func() string {
			if authMode == "register" {
				return "Create account and connect"
			}
			return "Sign in and connect"
		}(), Icon: lucide.LogIn, Variant: ui.ButtonPrimary,
			Size: controlSize(cx), TestID: "settings-relay-submit", OnPress: a.configureRelay})
	}
	return core.View{TestID: "settings-relay-config", Style: core.Style{Direction: core.Column, Gap: 8,
		Padding: core.Symmetric(9, 11), Background: colors.Inset(), CornerRadius: 8,
		BorderWidth: 1, BorderColor: colors.Border()}, Children: body}
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
		body = append(body, a.backgroundServerCard(cx))
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
		if state := relayAccessStateLabel(a.relayAccess.Get()); state != "" {
			relayDetail += " · " + state
		}
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
	if !hasRelay {
		body = append(body, a.relayConfigurationCard(cx))
	}
	link := a.accessLink.Get()
	if a.accessErr.Get() != "" {
		body = append(body, ui.Text(a.accessErr.Get(), 11.5, ui.Regular, colors.Danger), a.backgroundServerCard(cx))
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
	body = append(body, a.backgroundServerCard(cx))
	return core.View{Style: core.Style{Direction: core.Column, Gap: 10}, Children: body}
}

// backgroundServerCard decides whether paired devices keep reaching this
// machine once the window closes. Only a package that ships the
// environment server has one to keep running.
func (a *app) backgroundServerCard(cx *core.Scope) core.Node {
	if a.manager == nil || bundledServerPath() == "" {
		return core.Group{}
	}
	colors := ui.ColorsOf(cx)
	return core.View{TestID: "settings-access-background", Style: core.Style{Direction: core.Row, AlignItems: core.AlignCenter,
		Gap: 12, Padding: core.Symmetric(10, 12), Background: colors.Inset(), CornerRadius: 9}, Children: []core.Node{
		core.View{Style: core.Style{Direction: core.Column, Gap: 2, FlexGrow: 1, FlexShrink: 1, MinWidth: core.Pt(0)},
			Children: []core.Node{
				core.Text{Content: "Keep running in the background", Style: core.Style{FontSize: 13, FontWeight: ui.Semibold},
					StyleFunc: func() core.Style { return core.Style{Color: colors.Text()} }},
				core.Text{Content: "Paired devices and agent runs reach this machine through its server. Closing T3Go stops that server unless it is kept running here.",
					MaxLines: 4, Style: core.Style{FontSize: 11, LineHeight: 16},
					StyleFunc: func() core.Style { return core.Style{Color: colors.Muted()} }},
			}},
		settingsSwitch(cx, "Keep the local environment server running in the background",
			"settings-background-server", func() bool { return a.manager.background.Get() }, a.manager.setBackgroundServer),
	}}
}
