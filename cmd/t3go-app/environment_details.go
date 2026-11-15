package main

import (
	"fmt"
	"strings"

	"github.com/oneo-me/nsgo/base"
	"github.com/oneo-me/nsgo/core"
	"github.com/oneo-me/nsgo/ui"
	"github.com/tk/t3go/internal/client"
	"github.com/tk/t3go/internal/environment"
)

func memoryLabel(bytes uint64) string {
	const gib = 1024 * 1024 * 1024
	if bytes < gib {
		return fmt.Sprintf("%.0f MiB", float64(bytes)/(1024*1024))
	}
	return fmt.Sprintf("%.1f GiB", float64(bytes)/gib)
}

func environmentHardware(cx *core.Scope, p client.EnvironmentProfile) core.Node {
	colors := ui.ColorsOf(cx)
	var rows []core.Node
	row := func(label, value, id string) {
		if value == "" {
			return
		}
		rows = append(rows, core.View{Style: core.Style{Direction: core.Row, Gap: 8, MinWidth: core.Pt(0)},
			Children: []core.Node{
				ui.Text(label, 11.5, ui.Regular, colors.Faint),
				core.Text{Content: value, TestID: id + p.ID, MaxLines: 2, Selectable: core.SelectableOn,
					Style:     core.Style{FontSize: 11.5, FlexGrow: 1, FlexShrink: 1, MinWidth: core.Pt(0)},
					StyleFunc: func() core.Style { return core.Style{Color: colors.Muted()} }},
			}})
	}
	if environment.UsableName(p.Hostname) && p.Hostname != environmentBaseName(p) {
		row("Host", p.Hostname, "environment-host-")
	}
	if info := p.System; info != nil {
		row("System", strings.TrimSpace(info.OS+" "+info.OSVersion)+" · "+info.Arch, "environment-system-")
		cpu := info.CPUModel
		if info.LogicalCPUs > 0 {
			if cpu != "" {
				cpu += " · "
			}
			cpu += fmt.Sprintf("%d logical CPUs", info.LogicalCPUs)
		}
		row("CPU", cpu, "environment-cpu-")
		if info.MemoryTotalBytes > 0 {
			row("Memory", memoryLabel(info.MemoryTotalBytes), "environment-memory-")
		}
	} else {
		rows = append(rows, ui.Text("System information unavailable", 11.5, ui.Regular, colors.Faint))
	}
	return core.View{TestID: "environment-hardware-" + p.ID, Style: core.Style{Gap: 4}, Children: rows}
}

func (a *app) environmentAliasEditor(cx *core.Scope, id string) core.Node {
	return core.Show{When: func() bool { return a.renameEnvironment.Get() == id }, Then: func(cx *core.Scope) core.Node {
		invalid := func() bool {
			value := strings.TrimSpace(a.environmentAlias.Get())
			return value != "" && !environment.UsableName(value)
		}
		return core.View{TestID: "environment-alias-editor-" + id, Style: core.Style{Gap: 8}, Children: []core.Node{
			mobileInput{Scope: cx, Input: base.Input{Label: "Environment alias", TestID: "environment-alias-" + id,
				Placeholder: "Alias (blank uses hostname)", Value: a.environmentAlias.Get, OnChange: a.environmentAlias.Set}},
			core.Dynamic(func(cx *core.Scope) core.Node {
				if invalid() {
					return ui.Text("Use a device name instead of localhost or a loopback address.", 12, ui.Regular, ui.ColorsOf(cx).Danger)
				}
				return ui.Text("Saved on this client. Leave blank to use the hostname.", 12, ui.Regular, ui.ColorsOf(cx).Muted)
			}),
			core.View{Style: core.Style{Direction: core.Row, Gap: 8, Wrap: core.Wrap}, Children: []core.Node{
				ui.Button{Text: "Save alias", TestID: "environment-alias-save-" + id, Size: controlSize(cx), Disabled: invalid,
					OnPress: func() {
						if invalid() {
							return
						}
						p := a.manager.find(id)
						p.Alias = strings.TrimSpace(a.environmentAlias.GetUntracked())
						a.manager.update(p)
						a.renameEnvironment.Set("")
					}},
				ui.Button{Text: "Cancel", TestID: "environment-alias-cancel-" + id, Variant: ui.ButtonGhost, Size: controlSize(cx),
					OnPress: func() { a.renameEnvironment.Set("") }},
			}},
		}}
	}}
}
