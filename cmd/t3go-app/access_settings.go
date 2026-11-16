package main

import (
	"net/url"
	"strings"

	"github.com/oneo-me/nsgo/core"
	"github.com/oneo-me/nsgo/icons/lucide"
	"github.com/oneo-me/nsgo/ui"
	qrcode "github.com/skip2/go-qrcode"
	"github.com/tk/t3go/internal/client"
	"github.com/tk/t3go/internal/proto"
)

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
	link, err := client.PairingLink(a.profile)
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
		return "The saved direct route is loopback-only. On another device, use the environment's LAN or relay route."
	}
	return ""
}

// settingsAccess configures whether this environment exposes its pairing
// link in the desktop settings window.
func (a *app) settingsAccess(cx *core.Scope) core.Node {
	colors := ui.ColorsOf(cx)
	enabled := func() bool { return a.settingsDraft.Get().AccessEnabled }
	body := []core.Node{
		settingsSectionTitle(cx, "Access", "Pair another device with this environment using its saved direct and relay routes."),
		core.View{Style: core.Style{Direction: core.Row, AlignItems: core.AlignCenter, Gap: 12,
			Padding: core.Symmetric(10, 12), Background: colors.Inset(), CornerRadius: 9}, Children: []core.Node{
			core.View{Style: core.Style{Direction: core.Column, Gap: 2, FlexGrow: 1, FlexShrink: 1, MinWidth: core.Pt(0)},
				Children: []core.Node{
					core.Text{Content: "Allow device pairing", TestID: "settings-access-title",
						Style:     core.Style{FontSize: 13, FontWeight: ui.Semibold},
						StyleFunc: func() core.Style { return core.Style{Color: colors.Text()} }},
					core.Text{Content: "Shows a pairing link and QR code for this environment. Credentials stay in local environment storage.",
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
