package app

import (
	"strconv"

	"github.com/egoist/mygo/ui"

	"github.com/tk/t3go/internal/app/icons/lucide"
	"github.com/tk/t3go/internal/i18n"
	"github.com/tk/t3go/internal/proto"
)

// configChoice is one selectable value of a session config option.
type configChoice struct {
	value string
	label string
}

// configOption is a per-session setting. Its popover follows the
// "Thinking" design: a title, a short explanation and a checked list.
// Model selection is deliberately not one of these options; it keeps its
// own picker and its own button.
type configOption struct {
	key         string
	label       string
	title       string
	description string
	icon        *ui.SVG
	value       string
	choices     []configChoice
	choose      func(string)
}

func (o configOption) currentLabel() string {
	for _, choice := range o.choices {
		if choice.value == o.value {
			return choice.label
		}
	}
	return o.value
}

func (o configOption) pillLabel() string {
	return o.label + ": " + o.currentLabel()
}

func boolConfigValue(on bool) string {
	return strconv.FormatBool(on)
}

func permissionConfigChoices() []configChoice {
	modes := []proto.PermissionMode{proto.PermissionSupervised, proto.PermissionAcceptEdits,
		proto.PermissionAuto, proto.PermissionFullAccess}
	choices := make([]configChoice, 0, len(modes))
	for _, mode := range modes {
		choices = append(choices, configChoice{value: string(mode), label: string(mode)})
	}
	return choices
}

// threadConfigOptions are the settings that can share one popover when
// the composer's middle pane has no room for individual pills.
func (s *Session) threadConfigOptions(thread proto.Thread) []configOption {
	return []configOption{
		{
			key:         "permission",
			label:       i18n.T("Permission"),
			title:       i18n.T("Permission"),
			description: i18n.T("Set what the agent may change in this thread."),
			icon:        lucide.KeyRound(),
			value:       string(thread.Permission),
			choices:     permissionConfigChoices(),
			choose:      func(value string) { s.setThreadPermission(proto.PermissionMode(value)) },
		},
		{
			key:         "plan",
			label:       i18n.T("Plan"),
			title:       i18n.T("Plan mode"),
			description: i18n.T("Plan the work before implementing it."),
			icon:        lucide.ClipboardCheck(),
			value:       boolConfigValue(thread.Plan),
			choices: []configChoice{
				{value: boolConfigValue(false), label: i18n.T("Off")},
				{value: boolConfigValue(true), label: i18n.T("On")},
			},
			choose: func(value string) { s.setThreadPlan(value == boolConfigValue(true)) },
		},
	}
}

// newThreadPermissionOption is the new thread's only config setting.
func (s *Session) newThreadPermissionOption() configOption {
	return configOption{
		key:         "permission",
		label:       i18n.T("Permission"),
		title:       i18n.T("Permission"),
		description: i18n.T("Set what the agent may change in this thread."),
		icon:        lucide.KeyRound(),
		value:       string(s.newThreadPerm),
		choices:     permissionConfigChoices(),
		choose:      func(value string) { s.newThreadPerm = proto.PermissionMode(value) },
	}
}

// sideChatPermissionOption is the side chat's only config setting.
func (s *Session) sideChatPermissionOption(thread proto.Thread) configOption {
	return configOption{
		key:         "permission",
		label:       i18n.T("Side chat permission"),
		title:       i18n.T("Permission"),
		description: i18n.T("Set what the agent may change in this thread."),
		icon:        lucide.KeyRound(),
		value:       string(thread.Permission),
		choices:     permissionConfigChoices(),
		choose:      func(value string) { s.setSidePermission(proto.PermissionMode(value)) },
	}
}

// configOptionsCollapsed is true when the middle pane cannot fit several
// config pills next to the model picker. A phone always gathers them,
// and a desktop pane does so when a side panel squeezes it.
func (s *Session) configOptionsCollapsed(c *ui.Context) bool {
	width, _ := c.Size()
	if compact(c) {
		return true
	}
	_, room, _ := s.desktopPaneWidths(width)
	return room < 700
}

// configOptions draws the session's config settings. Several settings
// gather into one popover in a narrow middle pane; the model picker is
// always drawn by the caller, outside this group.
func (s *Session) configOptions(c *ui.Context, opts []configOption) {
	if len(opts) == 0 {
		return
	}
	if len(opts) > 1 && s.configOptionsCollapsed(c) {
		s.configPopover(c, opts)
		return
	}
	for _, opt := range opts {
		s.configPill(c, opt)
	}
}

// configPopoverPanel styles a config popover, keeps it inside the safe
// area, and returns the height its content may use. On a phone the panel
// opens on the side with more room, above the keyboard when necessary.
func configPopoverPanel(c *ui.Context, anchor, panel ui.Element) float32 {
	t := c.Theme()
	width, height := c.Size()
	top, right, bottom, left := c.SafeArea()
	bounds := anchor.Bounds()
	above := max(float32(0), bounds.Y-top-8)
	below := max(float32(0), height-bottom-bounds.Y-bounds.H-8)
	room := below
	if above > below {
		room = above
		panel.AttachTo(anchor, ui.AnchorTopStart, ui.AnchorBottomStart)
	}
	panel.Width(min(360, width-left-right-24)).MaxWidth(width-left-right-24).
		Padding(14).Gap(12).Radius(12).Border(1, t.Border).Background(t.Surface).
		Shadow(0, 6, 24, 0, t.Text.Alpha(0.16))
	return max(96, min(room, height-top-bottom-24))
}

// configOptionContent renders one setting the way the reference design
// does: a heading, a description and a list whose current value is
// checked. Choosing a value closes the popover that holds it.
func configOptionContent(c *ui.Context, opt configOption, close func()) {
	t := c.Theme()
	ui.Column(c).FillWidth().Gap(6).Children(func() {
		ui.Text(c, opt.title).FontSize(13).FontWeight(600).TextColor(t.Text)
		faint(c, opt.description, 11)
		ui.Column(c).FillWidth().Gap(1).Children(func() {
			for _, choice := range opt.choices {
				choice := choice
				selected := choice.value == opt.value
				row := clickableRow(c, choice.label, func() {
					ui.Box(c).Width(18).Shrink(0)
					ui.Text(c, choice.label).FontSize(12).
						TextColor(t.Text).SingleLine().MinWidth(0).Grow(1)
					if selected {
						ui.Icon(c, lucide.Check()).FontSize(13).TextColor(t.Accent).Shrink(0)
					}
				}).Padding(7, 6).Radius(8).KeepFocus()
				if selected || row.Hovered() {
					row.Background(t.SurfaceHover)
				}
				row.OnClick(func() {
					close()
					opt.choose(choice.value)
				})
			}
		})
	})
}

// configPill is one config option shown on its own when the middle pane
// has room for it.
func (s *Session) configPill(c *ui.Context, opt configOption) {
	icon := opt.icon
	if icon == nil {
		icon = lucide.SlidersHorizontal()
	}
	anchor := toolbarButton(c, "").Label(opt.pillLabel()).MaxWidthPercent(100).MinWidth(0).Children(func() {
		ui.Icon(c, icon).FontSize(13)
		ui.Text(c, opt.pillLabel()).FontSize(11.5).SingleLine().MinWidth(0)
		ui.Icon(c, lucide.ChevronDown()).FontSize(10)
	})
	open := ui.Local(anchor, "config-"+opt.key, func() bool { return false })
	anchor.OnClick(func() { *open = !*open })
	ui.PopoverBase(c, anchor, open, func(panel ui.Element) {
		room := configPopoverPanel(c, anchor, panel)
		ui.Scroll(c).FillWidth().MaxHeight(room).Children(func() {
			configOptionContent(c, opt, func() { *open = false })
		})
	})
}

// configPopover gathers every config option into a single pill and panel.
func (s *Session) configPopover(c *ui.Context, opts []configOption) {
	anchor := toolbarButton(c, "").Label(i18n.T("Config")).MaxWidthPercent(100).MinWidth(0).Children(func() {
		ui.Icon(c, lucide.SlidersHorizontal()).FontSize(13)
		ui.Text(c, i18n.T("Config")).FontSize(11.5).SingleLine().MinWidth(0)
		ui.Icon(c, lucide.ChevronDown()).FontSize(10)
	})
	open := ui.Local(anchor, "session-config", func() bool { return false })
	anchor.OnClick(func() { *open = !*open })
	ui.PopoverBase(c, anchor, open, func(panel ui.Element) {
		room := configPopoverPanel(c, anchor, panel)
		t := c.Theme()
		ui.Scroll(c).FillWidth().MaxHeight(room).Children(func() {
			for i, opt := range opts {
				if i > 0 {
					ui.Box(c).FillWidth().Height(1).Background(t.Border).Shrink(0).Margin(5, 0)
				}
				configOptionContent(c, opt, func() { *open = false })
			}
		})
	})
}
