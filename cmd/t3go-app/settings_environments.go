package main

import (
	"fmt"
	"strings"

	"github.com/egoist/asuka/colors"
	"github.com/egoist/asuka/fonts"
	"github.com/egoist/asuka/signal"
	"github.com/egoist/asuka/ui"
	"github.com/tk/t3go/internal/client"
)

func settingsEnvironmentsView(a *app) ui.Node {
	rows := ui.For(a.profilesV, func(p client.EnvironmentProfile) string { return p.ID },
		func(item signal.Accessor[client.EnvironmentProfile], _ signal.Accessor[int]) ui.Node {
			return ui.Dynamic(func() ui.Node {
				return settingsEnvironmentCard(a, item.Get())
			})
		})
	count := len(a.profilesV.Get())
	summary := "No saved environments"
	if count > 0 {
		summary = fmt.Sprintf("%d saved · active: %s", count, a.environmentName(a.activeV.Get()))
	}
	return ui.Column(ui.Style{Gap: 12},
		sectionTitle("Environments", "Saved machines, routes and credentials. Other settings tabs follow the active environment."),
		ui.Row(ui.Style{Wrap: true, Align: ui.AlignCenter, Gap: 6},
			ui.Text(ui.Val(summary), ui.Style{Font: fonts.Caption, Color: colors.SecondaryLabel, Grow: 1}),
			ui.Button(ui.ButtonProps{Title: ui.Val("Refresh"), Icon: ui.IconRefresh,
				Label: ui.Val("Refresh environment status"), OnPress: a.refreshEnvironmentSnapshots}),
			ui.Button(ui.ButtonProps{Title: ui.Val("Add environment"), Icon: ui.IconPlus,
				Variant: ui.ButtonFilled, OnPress: a.openAddEnvironment}),
		),
		ui.Show(signal.Func(func() bool { return len(a.profilesV.Get()) == 0 }), func() ui.Node {
			return card(
				ui.Text(ui.Val("No environments yet"), ui.Style{Font: fonts.Subheadline.Weighted(fonts.Semibold)}),
				ui.Text(ui.Val("Add a direct URL or relay link to start managing environments."),
					ui.Style{Font: fonts.Caption, Color: colors.SecondaryLabel, Lines: 3}),
				ui.Button(ui.ButtonProps{Title: ui.Val("Add environment"), Icon: ui.IconPlus,
					Variant: ui.ButtonFilled, OnPress: a.openAddEnvironment}),
			)
		}),
		ui.Column(ui.Style{Gap: 8}, rows),
	)
}

func settingsEnvironmentCard(a *app, p client.EnvironmentProfile) ui.Node {
	_ = a.envSnapV.Get()
	current := p.ID == a.activeID
	isDefault := p.ID == a.defaultID
	snapshot, loaded := a.environmentSnapshotFor(p.ID)
	status, statusColor, statusDetail := environmentStatus(a, p, snapshot, loaded)
	host := environmentHost(p)
	if host == "" {
		host = "Saved environment"
	}
	routeLabel := environmentRouteLabel(p)
	tokenLabel := "Token saved"
	if strings.TrimSpace(p.Token) == "" {
		tokenLabel = "No token"
	}

	heading := []ui.Node{
		ui.Text(ui.Val(environmentBaseName(p)), ui.Style{Font: fonts.Subheadline.Weighted(fonts.Semibold),
			Grow: 1, Lines: 1}),
	}
	if current {
		heading = append(heading, environmentBadge("Active", colors.Accent))
	}
	if isDefault {
		heading = append(heading, environmentBadge("Default", colors.Green))
	}

	var switchButton ui.Node
	if current {
		switchButton = ui.Button(ui.ButtonProps{Title: ui.Val("Reconnect"), Icon: ui.IconRefresh,
			Disabled: a.connecting, OnPress: func() { a.reconnectProfile(p.ID) }})
	} else {
		switchButton = ui.Button(ui.ButtonProps{Title: ui.Val("Switch"), Icon: ui.IconGlobe,
			Disabled: a.connecting, OnPress: func() { a.switchProfile(p.ID) }})
	}

	return card(
		ui.Row(ui.Style{Align: ui.AlignCenter, Gap: 5}, heading...),
		ui.Text(ui.Val(host), ui.Style{Font: fonts.Caption, Color: colors.SecondaryLabel, Lines: 1}),
		ui.Row(ui.Style{Wrap: true, Align: ui.AlignCenter, Gap: 5},
			environmentBadge(status, statusColor),
			environmentBadge(profilePreferenceLabel(p.Preference), colors.SecondaryLabel),
			environmentBadge(routeLabel, colors.SecondaryLabel),
			environmentBadge(tokenLabel, colors.SecondaryLabel),
		),
		ui.Text(ui.Val(environmentSnapshotLabel(snapshot, loaded)), ui.Style{Font: fonts.Caption2,
			Color: colors.SecondaryLabel, Lines: 2}),
		ui.Show(signal.Func(func() bool { return statusDetail != "" }), func() ui.Node {
			return ui.Text(ui.Val(statusDetail), ui.Style{Font: fonts.Caption2, Color: colors.Red, Lines: 3})
		}),
		ui.Row(ui.Style{Wrap: true, Align: ui.AlignCenter, Gap: 5},
			switchButton,
			ui.Button(ui.ButtonProps{Title: ui.Val("Set as default"), Icon: ui.IconCheck,
				Disabled: ui.Val(isDefault), OnPress: func() { a.setDefaultProfile(p.ID) }}),
			ui.Button(ui.ButtonProps{Icon: ui.IconMore, Label: ui.Val("Manage " + environmentBaseName(p)),
				Menu: environmentActions(a, p)}),
		),
	)
}

func environmentBadge(label string, color ui.Color) ui.Node {
	return ui.Text(ui.Val(label), ui.Style{Font: fonts.Caption2.Weighted(fonts.Medium), Color: color,
		Padding: ui.Symmetric(1, 6), CornerRadius: 6, Background: color.Opacity(0.1)})
}

func environmentRouteLabel(p client.EnvironmentProfile) string {
	direct, relay := false, false
	for _, route := range p.Routes {
		switch route.Kind {
		case "direct":
			direct = true
		case "relay":
			relay = true
		}
	}
	switch {
	case direct && relay:
		return "Direct + Relay"
	case relay:
		return "Relay route"
	case direct:
		return "Direct route"
	default:
		return "No route"
	}
}

func environmentStatus(a *app, p client.EnvironmentProfile, snapshot environmentSnapshot, loaded bool) (string, ui.Color, string) {
	if p.ID == a.activeID {
		switch {
		case a.connecting.Get():
			return "Connecting…", colors.Orange, ""
		case a.offline.Get():
			return "Reconnecting…", colors.Orange, a.connErr.Get()
		case !a.connected.Get():
			return "Disconnected", colors.Red, a.connErr.Get()
		}
		switch routeKind(p.Routes, a.serverURL.Get()) {
		case "relay":
			return "Connected via Relay", colors.Green, ""
		case "direct":
			return "Connected via Direct", colors.Green, ""
		default:
			return "Connected", colors.Green, ""
		}
	}
	if !loaded {
		return "Not refreshed", colors.SecondaryLabel, ""
	}
	if snapshot.Err != "" {
		return "Unavailable", colors.Red, snapshot.Err
	}
	return "Available", colors.Green, ""
}

func environmentSnapshotLabel(snapshot environmentSnapshot, loaded bool) string {
	if !loaded {
		return "Snapshot not loaded yet."
	}
	if snapshot.Err != "" {
		return "The last snapshot refresh failed."
	}
	return fmt.Sprintf("%d projects · %d threads", len(snapshot.Projects), len(snapshot.Threads))
}
