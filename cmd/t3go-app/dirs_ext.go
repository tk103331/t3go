package main

import (
	"context"
	"strings"
	"time"

	"github.com/egoist/asuka/colors"
	"github.com/egoist/asuka/fonts"
	"github.com/egoist/asuka/signal"
	"github.com/egoist/asuka/ui"
)

func (a *app) openDirPicker() {
	a.dirsErr.Set("")
	a.showDirPicker.Set(true)
	a.loadDirs("")
}

func (a *app) loadDirs(path string) {
	c := a.client()
	if c == nil {
		a.dirsErr.Set("Connect an environment first.")
		return
	}
	a.dirsBusy.Set(true)
	a.dirsErr.Set("")
	go func() {
		ctx, cancel := context.WithTimeout(a.lifetime, 20*time.Second)
		defer cancel()
		dirs, err := c.Dirs(ctx, path)
		ui.Post(func() {
			if a.client() != c {
				return
			}
			a.dirsBusy.Set(false)
			if err != nil {
				a.dirsErr.Set(friendlyError(err))
				return
			}
			a.dirs.Set(dirs)
		})
	}()
}

func (a *app) chooseDir(path string) {
	path = strings.TrimSpace(path)
	if path == "" {
		return
	}
	a.newProjectPath.Set(path)
	if strings.TrimSpace(a.newProjectName.Peek()) == "" {
		name := path[strings.LastIndexAny(path, `/\`)+1:]
		if name == "" {
			name = path
		}
		a.newProjectName.Set(name)
	}
	a.showDirPicker.Set(false)
}

func (a *app) createDir(name string) {
	current := a.dirs.Peek().Path
	name = strings.TrimSpace(name)
	c := a.client()
	if c == nil || name == "" {
		return
	}
	a.dirsBusy.Set(true)
	a.dirsErr.Set("")
	go func() {
		ctx, cancel := context.WithTimeout(a.lifetime, 20*time.Second)
		defer cancel()
		dirs, err := c.CreateDir(ctx, current, name)
		ui.Post(func() {
			if a.client() != c {
				return
			}
			a.dirsBusy.Set(false)
			if err != nil {
				a.dirsErr.Set(friendlyError(err))
				return
			}
			a.dirs.Set(dirs)
			a.newDirName.Set("")
		})
	}()
}

func dirsView(a *app) ui.Node {
	return ui.Dynamic(func() ui.Node { return dirsContentView(a) })
}

func dirsContentView(a *app) ui.Node {
	dirs := a.dirs.Get()
	path := dirs.Path
	if path == "" {
		path = dirs.Home
	}
	var rows []ui.Node
	for _, entry := range dirs.Entries {
		entry := entry
		rows = append(rows, ui.View(ui.ViewProps{
			OnPress: func() { a.loadDirs(entry.Path) },
			Style:   ui.Style{Padding: ui.Symmetric(7, 4)},
		}, ui.Row(ui.Style{Align: ui.AlignCenter, Gap: 7},
			ui.Symbol(ui.IconFolder, ui.Style{Color: colors.Accent}),
			ui.Text(ui.Val(entry.Name), ui.Style{Font: fonts.Footnote, Grow: 1, Lines: 1}),
			ui.Symbol(ui.IconForward, ui.Style{Color: colors.SecondaryLabel}),
		)))
	}
	if len(rows) == 0 {
		rows = append(rows, ui.Text(ui.Val("No folders here."), ui.Style{Font: fonts.Caption, Color: colors.SecondaryLabel}))
	}
	return ui.Column(ui.Style{Gap: 6, Padding: ui.All(8), CornerRadius: 10,
		Background: colors.Background, BorderWidth: 1, BorderColor: colors.Separator},
		ui.Row(ui.Style{Align: ui.AlignCenter, Gap: 5},
			ui.Button(ui.ButtonProps{Icon: ui.IconUp, Label: ui.Val("Parent folder"),
				Disabled: signal.Func(func() bool { return dirs.Parent == "" }),
				OnPress:  func() { a.loadDirs(dirs.Parent) }}),
			ui.Text(ui.Val(path), ui.Style{Font: fonts.Custom("monospace", fonts.Caption), Grow: 1, Lines: 1}),
			ui.Button(ui.ButtonProps{Title: ui.Val("Use folder"), Variant: ui.ButtonFilled,
				OnPress: func() { a.chooseDir(path) }}),
		),
		ui.Row(ui.Style{Align: ui.AlignCenter, Gap: 5},
			ui.TextField(ui.TextFieldProps{Value: a.newDirName, Placeholder: ui.Val("New folder name"),
				Style: ui.Style{Grow: 1}, ReturnKey: ui.ReturnGo,
				OnSubmit: func() { a.createDir(a.newDirName.Peek()) }}),
			ui.Button(ui.ButtonProps{Icon: ui.IconPlus, Label: ui.Val("Create folder"),
				OnPress: func() { a.createDir(a.newDirName.Peek()) }}),
		),
		ui.Show(signal.Func(func() bool { return a.dirsBusy.Get() }), func() ui.Node {
			return ui.Row(ui.Style{Align: ui.AlignCenter, Gap: 6}, ui.Activity(),
				ui.Text(ui.Val("Reading folders…"), ui.Style{Font: fonts.Caption, Color: colors.SecondaryLabel}))
		}),
		ui.Show(signal.Func(func() bool { return a.dirsErr.Get() != "" }), func() ui.Node {
			return ui.Text(a.dirsErr, ui.Style{Font: fonts.Caption, Color: colors.Red, Lines: 3})
		}),
		ui.Scroll(ui.ScrollProps{Style: ui.Style{MaxHeight: ui.Pt(260)}},
			ui.Column(ui.Style{Gap: 1}, rows...)),
		ui.Button(ui.ButtonProps{Title: ui.Val("Close folder browser"), OnPress: func() { a.showDirPicker.Set(false) }}),
	)
}
