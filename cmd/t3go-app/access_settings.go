package main

import (
	"context"
	"net"
	"net/url"
	"os"
	"strings"
	"time"

	"github.com/oneo-me/nsgo/base"
	"github.com/oneo-me/nsgo/core"
	"github.com/oneo-me/nsgo/icons/lucide"
	"github.com/oneo-me/nsgo/ui"
	qrcode "github.com/skip2/go-qrcode"
	"github.com/tk/t3go/internal/client"
	"github.com/tk/t3go/internal/i18n"
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
	var fallback client.Route
	haveFallback := false
	for _, route := range profile.Routes {
		if route.Kind == kind {
			if kind != "direct" || !isLoopbackURL(route.URL) {
				return route, true
			}
			if !haveFallback {
				fallback, haveFallback = route, true
			}
		}
	}
	return fallback, haveFallback
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

func (a *app) relayAccountSession() (proto.RelayUserSession, string, bool) {
	if a.manager == nil || !a.manager.accountSessionValid() {
		return proto.RelayUserSession{}, "", false
	}
	server := relayAccountServer(a.manager.accountServer.GetUntracked())
	if server == "" {
		return proto.RelayUserSession{}, "", false
	}
	return a.manager.accountSession.GetUntracked(), server, true
}

func (a *app) configureRelay() {
	if a.relayBusy.GetUntracked() {
		return
	}
	c := a.client()
	if c == nil {
		a.relayErr.Set(i18n.T("Connect the local environment before configuring Relay."))
		return
	}
	session, server, ok := a.relayAccountSession()
	if !ok {
		a.relayErr.Set(i18n.T("Sign in from Account before connecting Relay."))
		return
	}
	in := proto.RelayAccessConfig{
		Server: server, UserToken: session.Token, DirectURL: a.localLANURL(),
		AllowHTTP: a.manager.accountAllowHTTP.GetUntracked(),
	}
	a.relayBusy.Set(true)
	a.relayErr.Set("")
	a.relaySeq++
	seq := a.relaySeq
	go func() {
		ctx, cancel := context.WithTimeout(a.lifetime, 45*time.Second)
		defer cancel()
		status, err := c.ConfigureRelay(ctx, in)
		a.cx.Post(func() {
			a.relayBusy.Set(false)
			if seq != a.relaySeq {
				return
			}
			if err != nil {
				a.relayErr.Set(friendlyError(err))
				return
			}
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
		return i18n.T("Connecting")
	case "retrying":
		return i18n.T("Reconnecting")
	case "active":
		return "Online"
	case "error":
		return i18n.T("Connection error")
	default:
		return "Configured"
	}
}

// refreshAccess draws the pairing link for the device just paired, using
// the routes Access allows now. Each device carries its own token, so a
// lost device is revoked here instead of rotating the environment token.
func (a *app) refreshAccess() {
	a.accessLink.Set("")
	a.accessQR.Set(nil)
	a.accessErr.Set("")
	if !a.settingsDraft.GetUntracked().AccessEnabled {
		return
	}
	if !isLocalEnvironmentProfile(a.profile) {
		a.accessErr.Set(i18n.T("Access is only available for the environment running on this machine."))
		return
	}
	if !a.deviceGrantsRead && !a.deviceGrantsBusy.GetUntracked() {
		a.loadDeviceGrants()
	}
	if a.accessToken == "" {
		return
	}
	direct, relay := accessRouteMode(a.settingsDraft.GetUntracked())
	pairing := a.profile
	pairing.Token = a.accessToken
	link, err := client.PairingLinkOptions(pairing, direct, relay)
	if err != nil {
		a.accessErr.Set(err.Error())
		return
	}
	png, err := qrcode.Encode(link, qrcode.Medium, 320)
	if err != nil {
		a.accessErr.Set(i18n.T("Could not draw pairing QR code: ") + err.Error())
		return
	}
	a.accessLink.Set(link)
	a.accessQR.Set(png)
}

// pairAccessDevice mints a token for another device and shows the link
// that pairs it. The token is shown once: revoking the grant later stops
// that device without touching any other.
func (a *app) pairAccessDevice() {
	if !a.settingsConnectionReady() {
		a.deviceGrantsErr.Set(i18n.T("Connect this environment before pairing a device."))
		return
	}
	label := strings.TrimSpace(a.deviceLabel.GetUntracked())
	a.devicePairBusy.Set(true)
	a.deviceGrantsErr.Set("")
	a.deviceLabel.Set("")
	var pairing proto.DevicePairing
	a.call(func(ctx context.Context, c *client.Client) error {
		p, err := c.PairDevice(ctx, label)
		pairing = p
		return err
	}, func(err error) {
		a.devicePairBusy.Set(false)
		if err != nil {
			a.deviceGrantsErr.Set(friendlyError(err))
			a.deviceLabel.Set(label)
			return
		}
		a.accessToken = pairing.Token
		a.accessDeviceID = pairing.Device.ID
		a.refreshAccess()
		a.loadDeviceGrants()
	})
}

// loadDeviceGrants refreshes the paired devices the Access section lists.
func (a *app) loadDeviceGrants() {
	if !a.settingsConnectionReady() {
		return
	}
	a.deviceGrantsBusy.Set(true)
	a.call(func(ctx context.Context, c *client.Client) error {
		grants, err := c.DeviceGrants(ctx)
		if err == nil {
			a.cx.Post(func() { a.deviceGrants.Set(grants) })
		}
		return err
	}, func(err error) {
		a.deviceGrantsBusy.Set(false)
		a.deviceGrantsRead = true
		if err != nil {
			a.deviceGrantsErr.Set(friendlyError(err))
		}
	})
}

// revokeAccessDevice stops one paired device from connecting here.
func (a *app) revokeAccessDevice(id string) {
	if !a.settingsConnectionReady() {
		a.deviceGrantsErr.Set(i18n.T("Connect this environment before revoking a device."))
		return
	}
	a.devicePairBusy.Set(true)
	a.deviceGrantsErr.Set("")
	a.call(func(ctx context.Context, c *client.Client) error {
		return c.RevokeDeviceGrant(ctx, id)
	}, func(err error) {
		a.devicePairBusy.Set(false)
		if err != nil {
			a.deviceGrantsErr.Set(friendlyError(err))
			return
		}
		if a.accessDeviceID == id {
			a.accessToken = ""
			a.accessDeviceID = ""
			a.refreshAccess()
		}
		a.loadDeviceGrants()
	})
}

// pairedDevicesCard pairs another device and lists the grants made so far.
func (a *app) pairedDevicesCard(cx *core.Scope) core.Node {
	colors := ui.ColorsOf(cx)
	children := []core.Node{
		ui.Text(i18n.T("Paired devices"), 12, ui.Semibold, colors.Muted),
		core.Text{Content: i18n.T("Every paired device keeps its own token. Revoking one leaves the environment token and the other devices working."),
			MaxLines: 3, Style: core.Style{FontSize: 11, LineHeight: 16},
			StyleFunc: func() core.Style { return core.Style{Color: colors.Muted()} }},
		core.View{Style: core.Style{Direction: core.Row, AlignItems: core.AlignCenter, Gap: 6}, Children: []core.Node{
			core.View{Style: core.Style{FlexGrow: 1, FlexShrink: 1, MinWidth: core.Pt(0)}, Children: []core.Node{
				mobileInput{Scope: cx, Input: base.Input{Value: a.deviceLabel.Get, OnChange: a.deviceLabel.Set,
					Placeholder: i18n.T("Device name, e.g. Pixel 8"), Label: i18n.T("Device name"),
					TestID: "settings-access-device-label", SubmitBehavior: core.SubmitBehaviorSubmit,
					OnSubmit: func(string) { a.pairAccessDevice() }}},
			}},
			ui.Button{Text: i18n.T("Pair"), Icon: lucide.ScanQrCode, Variant: ui.ButtonPrimary, Size: controlSize(cx),
				TestID: "settings-access-pair", Disabled: func() bool { return a.devicePairBusy.Get() },
				OnPress: a.pairAccessDevice},
		}},
	}
	grants := a.deviceGrants.Get()
	rows := make([]core.Node, 0, len(grants))
	for _, grant := range grants {
		rows = append(rows, a.deviceGrantRow(cx, grant))
	}
	if len(rows) == 0 {
		rows = append(rows, ui.Text(i18n.T("No devices paired yet."), 11.5, ui.Regular, colors.Faint))
	}
	children = append(children, core.View{TestID: "settings-access-devices",
		Style: core.Style{Direction: core.Column, Gap: 4}, Children: rows})
	return core.View{Style: core.Style{Direction: core.Column, Gap: 7, Padding: core.Symmetric(10, 12),
		Background: colors.Inset(), CornerRadius: 9}, Children: children}
}

// deviceGrantRow is one paired device, with the way to revoke it.
func (a *app) deviceGrantRow(cx *core.Scope, grant proto.DeviceGrant) core.Node {
	colors := ui.ColorsOf(cx)
	label := grant.Label
	if label == "" {
		label = i18n.T("Unnamed device")
	}
	detail := i18n.T("Added ") + grant.CreatedAt.Local().Format("2006-01-02")
	if grant.LastSeenAt != nil {
		detail += " · " + i18n.T("last used ") + grant.LastSeenAt.Local().Format("2006-01-02 15:04")
	} else {
		detail += " · " + i18n.T("never used")
	}
	if grant.Current {
		label += " · " + i18n.T("this device")
	}
	var action core.Node = core.Group{}
	if grant.RevokedAt != nil {
		detail += " · " + i18n.T("revoked")
	} else {
		action = ui.Button{Text: i18n.T("Revoke"), Variant: ui.ButtonGhost, Size: controlSize(cx),
			TestID: "settings-access-revoke-" + grant.ID, Disabled: func() bool { return a.devicePairBusy.Get() },
			OnPress: func() { a.revokeAccessDevice(grant.ID) }}
	}
	return core.View{Style: core.Style{Direction: core.Row, AlignItems: core.AlignCenter, Gap: 8,
		Padding: core.Symmetric(3, 2)}, Children: []core.Node{
		core.View{Style: core.Style{FlexGrow: 1, FlexShrink: 1, MinWidth: core.Pt(0), Gap: 1}, Children: []core.Node{
			core.Text{Content: label, TestID: "settings-access-device-" + grant.ID, MaxLines: 1,
				Style: core.Style{FontSize: 12}, StyleFunc: func() core.Style { return core.Style{Color: colors.Text()} }},
			core.Text{Content: detail, MaxLines: 1, Style: core.Style{FontSize: 10.5},
				StyleFunc: func() core.Style { return core.Style{Color: colors.Faint()} }},
		}},
		action,
	}}
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
		return i18n.T("This direct route only works on this machine. Turn on LAN access for devices on the same Wi-Fi, or configure Relay for access across networks.")
	}
	return ""
}

// localLANURL is the address a phone on the same network can use. It is
// deliberately separate from Relay: LAN pairing uses the environment token
// and does not require an account.
func (a *app) localLANURL() string {
	if a.manager == nil || !a.manager.lan.GetUntracked() || a.manager.local == nil ||
		a.manager.local.lanIP == "" {
		return ""
	}
	u, err := url.Parse(a.manager.local.baseURL)
	if err != nil || u.Port() == "" {
		return ""
	}
	return "http://" + net.JoinHostPort(a.manager.local.lanIP, u.Port())
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
		control = accessRouteSwitch(cx, label+i18n.T(" pairing route"), testID, checked, disabled, onChange)
	} else {
		control = core.Text{Content: i18n.T("Not configured"), TestID: testID + "-unavailable",
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
	session, server, signedIn := a.relayAccountSession()
	header := core.View{Style: core.Style{Direction: core.Row, AlignItems: core.AlignCenter, Gap: 8}, Children: []core.Node{
		core.View{Style: core.Style{Direction: core.Column, Gap: 2, FlexGrow: 1, FlexShrink: 1, MinWidth: core.Pt(0)},
			Children: []core.Node{
				core.Text{Content: i18n.T("Relay access"), Style: core.Style{FontSize: 12.5, FontWeight: ui.Semibold},
					StyleFunc: func() core.Style { return core.Style{Color: colors.Text()} }},
				core.Text{Content: i18n.T("Use the Relay account already signed in under Account."),
					MaxLines: 2, Style: core.Style{FontSize: 10.5},
					StyleFunc: func() core.Style { return core.Style{Color: colors.Muted()} }},
			}},
	}}
	body := []core.Node{header}
	if signedIn {
		name := strings.TrimSpace(session.User.Name)
		if name == "" {
			name = session.User.Email
		}
		body = append(body, core.View{TestID: "settings-relay-account",
			Style: core.Style{Direction: core.Row, AlignItems: core.AlignCenter, Gap: 9,
				Padding: core.Symmetric(8, 10), Background: colors.Field(), CornerRadius: 7},
			Children: []core.Node{
				ui.Icon(lucide.UserRound, 18, colors.Accent),
				core.View{Style: core.Style{Direction: core.Column, Gap: 1, FlexGrow: 1, FlexShrink: 1, MinWidth: core.Pt(0)},
					Children: []core.Node{
						core.Text{Content: name, TestID: "settings-relay-account-name", MaxLines: 1,
							Style:     core.Style{FontSize: 11.5, FontWeight: ui.Semibold},
							StyleFunc: func() core.Style { return core.Style{Color: colors.Text()} }},
						core.Text{Content: session.User.Email + " · " + server, TestID: "settings-relay-account-server", MaxLines: 2,
							Style:     core.Style{FontSize: 10.5},
							StyleFunc: func() core.Style { return core.Style{Color: colors.Muted()} }},
					}},
			}})
	} else {
		body = append(body, core.Text{Content: i18n.T("No Relay account is signed in. Sign in once under Account, then return here to connect this machine."),
			TestID: "settings-relay-account-required", MaxLines: 3,
			Style:     core.Style{FontSize: 10.5, LineHeight: 15},
			StyleFunc: func() core.Style { return core.Style{Color: colors.Muted()} }})
	}
	if a.relayBusy.Get() {
		body = append(body, ui.Spinner{Size: 14, Label: i18n.T("Connecting Relay")})
	} else if signedIn {
		body = append(body, ui.Button{Text: i18n.T("Connect Relay"), Icon: lucide.Link,
			Variant: ui.ButtonPrimary, Size: controlSize(cx), TestID: "settings-relay-submit",
			OnPress: a.configureRelay})
	} else {
		body = append(body, ui.Button{Text: i18n.T("Open Account"), Icon: lucide.UserRound,
			Variant: ui.ButtonSecondary, Size: controlSize(cx), TestID: "settings-relay-open-account",
			OnPress: func() { a.setSettingsSection("account") }})
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
			settingsSectionTitle(cx, i18n.T("Access"), i18n.T("Device pairing is available only for the T3Go environment running on this machine.")),
			ui.Text(i18n.T("Select the local environment to manage Access."), 12, ui.Regular, colors.Muted),
		}}
	}
	enabled := func() bool { return a.settingsDraft.Get().AccessEnabled }
	body := []core.Node{
		settingsSectionTitle(cx, i18n.T("Access"), i18n.T("Local desktop use needs no account. Devices on the same Wi-Fi pair directly; Relay is only for access across networks.")),
	}
	hasLANCard := a.hasLANAccessCard()
	if hasLANCard {
		body = append(body, a.lanAccessCard(cx))
	}
	body = append(body,
		core.View{Style: core.Style{Direction: core.Row, AlignItems: core.AlignCenter, Gap: 12,
			Padding: core.Symmetric(10, 12), Background: colors.Inset(), CornerRadius: 9}, Children: []core.Node{
			core.View{Style: core.Style{Direction: core.Column, Gap: 2, FlexGrow: 1, FlexShrink: 1, MinWidth: core.Pt(0)},
				Children: []core.Node{
					core.Text{Content: i18n.T("Allow device pairing"), TestID: "settings-access-title",
						Style:     core.Style{FontSize: 13, FontWeight: ui.Semibold},
						StyleFunc: func() core.Style { return core.Style{Color: colors.Text()} }},
					core.Text{Content: i18n.T("Pairs other devices with their own tokens. Each link is shown once; revoke a device here to stop it."),
						MaxLines: 3, Style: core.Style{FontSize: 11, LineHeight: 16},
						StyleFunc: func() core.Style { return core.Style{Color: colors.Muted()} }},
				}},
			settingsSwitch(cx, i18n.T("Allow device pairing"), "settings-access-enabled", enabled, func(on bool) {
				a.settingsDraft.Update(func(settings proto.Settings) proto.Settings {
					settings.AccessEnabled = on
					return settings
				})
				a.refreshAccess()
			}),
		}},
	)
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
	directDetail := i18n.T("No direct route saved for this environment.")
	if hasDirect {
		directDetail = directRoute.URL
	}
	relayDetail := i18n.T("No relay route saved for this environment.")
	if hasRelay {
		relayDetail = relayRoute.URL
		if state := relayAccessStateLabel(a.relayAccess.Get()); state != "" {
			relayDetail += " · " + state
		}
	}
	routeRows := []core.Node{ui.Text(i18n.T("Pairing routes"), 12, ui.Semibold, colors.Muted)}
	if !hasLANCard {
		routeRows = append(routeRows, accessRouteRow(cx, i18n.T("LAN direct"), directDetail, "settings-access-direct", hasDirect,
			func() bool { return modeDirect && hasDirect },
			func() bool { return modeDirect && hasDirect && (!modeRelay || !hasRelay) },
			func(on bool) { toggleRoute(true, on) }))
	}
	routeRows = append(routeRows, accessRouteRow(cx, "Relay", relayDetail, "settings-access-relay", hasRelay,
		func() bool { return modeRelay && hasRelay },
		func() bool { return modeRelay && hasRelay && (!modeDirect || !hasDirect) },
		func(on bool) { toggleRoute(false, on) }))
	body = append(body, core.View{TestID: "settings-access-routes",
		Style: core.Style{Direction: core.Column, Gap: 7}, Children: routeRows})
	if !hasRelay {
		body = append(body, ui.Text(i18n.T("Remote access (optional)"), 12, ui.Semibold, colors.Muted))
		body = append(body, a.relayConfigurationCard(cx))
	}
	if a.accessErr.Get() != "" {
		body = append(body, a.backgroundServerCard(cx))
		return core.View{Style: core.Style{Direction: core.Column, Gap: 10}, Children: body}
	}
	body = append(body, a.pairedDevicesCard(cx))
	link := a.accessLink.Get()
	if link == "" {
		body = append(body, a.backgroundServerCard(cx))
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
				core.Text{Content: i18n.T("Pairing link"), Style: core.Style{FontSize: 12, FontWeight: ui.Semibold},
					StyleFunc: func() core.Style { return core.Style{Color: colors.Text()} }},
				ui.Spacer{},
				ui.Button{Text: i18n.T("Copy link"), Icon: lucide.Copy, Variant: ui.ButtonSecondary,
					Size: controlSize(cx), TestID: "settings-access-copy", OnPress: func() { a.cx.SetClipboard(link) }},
			}},
			core.Text{Content: link, TestID: "settings-access-link", Selectable: core.SelectableOn, MaxLines: 4,
				Style:     core.Style{FontSize: 10.5, FontFamily: "Menlo, monospace", LineHeight: 15},
				StyleFunc: func() core.Style { return core.Style{Color: colors.Muted()} }},
			core.Text{Content: i18n.T("Open this link on the new device, or let it scan the QR code. Revoke the device below if it is lost."),
				MaxLines: 3, Style: core.Style{FontSize: 10.5, LineHeight: 15},
				StyleFunc: func() core.Style { return core.Style{Color: colors.Muted()} }},
		}},
	)
	if len(a.accessQR.Get()) > 0 {
		body = append(body, core.View{Style: core.Style{AlignItems: core.AlignCenter, Padding: core.All(8)},
			Children: []core.Node{ui.Image{Source: core.ImageSource{
				Data: a.accessQR.Get(), Size: core.Size{Width: 220, Height: 220},
			}, Label: i18n.T("Environment pairing QR code"), ResizeMode: core.Contain, TestID: "settings-access-qr",
				Style: core.Style{Width: core.Pt(220), Height: core.Pt(220), CornerRadius: 8}}}},
		)
	}
	body = append(body, a.backgroundServerCard(cx))
	return core.View{Style: core.Style{Direction: core.Column, Gap: 10}, Children: body}
}

// lanAccessCard exposes the built-in server on the local network. This is
// the no-account path used by a phone on the same Wi-Fi.
func (a *app) lanAccessCard(cx *core.Scope) core.Node {
	if !a.hasLANAccessCard() {
		return core.Group{}
	}
	colors := ui.ColorsOf(cx)
	address := a.localLANURL()
	direct, _ := accessRouteMode(a.settingsDraft.Get())
	on := a.manager.lan.Get() && direct
	detail := i18n.T("Turn this on to pair a phone over the same Wi-Fi. The pairing link still requires this environment's token.")
	if on {
		if address == "" {
			detail = i18n.T("LAN access is on, but no private network address was detected. Reconnect this machine to Wi-Fi.")
		} else {
			detail = address + i18n.T(" · Same Wi-Fi only; no account required.")
		}
	}
	return core.View{TestID: "settings-access-lan", Style: core.Style{Direction: core.Row, AlignItems: core.AlignCenter,
		Gap: 12, Padding: core.Symmetric(10, 12), Background: colors.Inset(), CornerRadius: 9}, Children: []core.Node{
		core.View{Style: core.Style{Direction: core.Column, Gap: 2, FlexGrow: 1, FlexShrink: 1, MinWidth: core.Pt(0)},
			Children: []core.Node{
				core.Text{Content: i18n.T("LAN direct"), TestID: "settings-access-lan-title",
					Style:     core.Style{FontSize: 13, FontWeight: ui.Semibold},
					StyleFunc: func() core.Style { return core.Style{Color: colors.Text()} }},
				core.Text{Content: detail, TestID: "settings-access-lan-detail", MaxLines: 3,
					Style:     core.Style{FontSize: 11, LineHeight: 16},
					StyleFunc: func() core.Style { return core.Style{Color: colors.Muted()} }},
			}},
		settingsSwitch(cx, i18n.T("Allow direct access from devices on this LAN"), "settings-access-lan-toggle",
			func() bool {
				direct, _ := accessRouteMode(a.settingsDraft.Get())
				return a.manager.lan.Get() && direct
			}, func(on bool) {
				a.settingsDraft.Update(func(settings proto.Settings) proto.Settings {
					_, relay := accessRouteMode(settings)
					setAccessRouteMode(&settings, on, relay)
					return settings
				})
				a.manager.setLANAccess(on)
				a.refreshAccess()
			}),
	}}
}

func (a *app) hasLANAccessCard() bool {
	return a.manager != nil && bundledServerPath() != "" && strings.TrimSpace(os.Getenv("T3GO_SERVER")) == ""
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
				core.Text{Content: i18n.T("Keep running in the background"), Style: core.Style{FontSize: 13, FontWeight: ui.Semibold},
					StyleFunc: func() core.Style { return core.Style{Color: colors.Text()} }},
				core.Text{Content: i18n.T("Paired devices and agent runs reach this machine through its server. Closing T3Go stops that server unless it is kept running here."),
					MaxLines: 4, Style: core.Style{FontSize: 11, LineHeight: 16},
					StyleFunc: func() core.Style { return core.Style{Color: colors.Muted()} }},
			}},
		settingsSwitch(cx, i18n.T("Keep the local environment server running in the background"),
			"settings-background-server", func() bool { return a.manager.background.Get() }, a.manager.setBackgroundServer),
	}}
}
