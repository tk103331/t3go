package app

import (
	"strings"

	"github.com/egoist/mygo/ui"

	"github.com/tk/t3go/internal/app/icons/lucide"
	"github.com/tk/t3go/internal/i18n"
	"github.com/tk/t3go/internal/proto"
)

const modelSearchThreshold = 10

type modelPickerState struct {
	open   bool
	query  string
	active int
	reveal bool
}

func modelChoiceLabel(model proto.Model, provider string) string {
	label := model.Name
	if label == "" {
		label = model.ID
	}
	if model.Provider != provider {
		label += " · " + model.Provider
	}
	return label
}

func filterModelChoices(models []proto.Model, query string) []proto.Model {
	terms := strings.Fields(strings.ToLower(query))
	if len(terms) == 0 {
		return models
	}
	var matches []proto.Model
	for _, model := range models {
		text := strings.ToLower(model.Name + " " + model.ID + " " + model.Provider)
		match := true
		for _, term := range terms {
			if !strings.Contains(text, term) {
				match = false
				break
			}
		}
		if match {
			matches = append(matches, model)
		}
	}
	return matches
}

// modelPicker retains a small menu for short catalogs. Larger catalogs
// get a search field and a scrolling list, without dropping any models.
func (s *Session) modelPicker(c *ui.Context, anchor ui.Element, provider, modelID string, choose func(proto.Model)) {
	models := s.allModels()
	if len(models) <= modelSearchThreshold {
		anchor.PopoverMenuButton(func(m *ui.Menu) {
			for _, model := range models {
				model := model
				if m.Item(modelChoiceLabel(model, provider)).
					Checked(model.ID == modelID && model.Provider == provider).
					Disabled(!s.providerAvailable(model.Provider)).Chosen() {
					choose(model)
				}
			}
		})
		return
	}
	state := ui.Local(anchor, "model-picker", func() modelPickerState { return modelPickerState{active: -1} })
	anchor.OnClick(func() {
		state.open = !state.open
		state.query, state.active, state.reveal = "", -1, false
	})
	ui.PopoverBase(c, anchor, &state.open, func(panel ui.Element) {
		t := c.Theme()
		width, height := c.Size()
		top, right, bottom, left := c.SafeArea()
		visibleHeight := max(float32(0), height-top-bottom)
		bounds := anchor.Bounds()
		above := max(float32(0), bounds.Y-top-8)
		below := max(float32(0), height-bottom-bounds.Y-bounds.H-8)
		// AttachTo fits against the full window. Prefer the side with
		// room inside the safe area so a short result list stays above
		// the phone keyboard as the search narrows.
		if above > below {
			panel.AttachTo(anchor, ui.AnchorTopStart, ui.AnchorBottomStart)
		}
		panel.Width(min(360, width-left-right-24)).MaxWidth(width-left-right-24).Padding(6).Gap(4).
			Radius(10).Border(1, t.Border).Background(t.Background).
			Shadow(0, 4, 16, 0, t.Text.Alpha(0.12))
		search := ui.TextInput(c, &state.query).Label(i18n.T("Search models")).
			Placeholder(i18n.T("Search name, model ID or provider")).FillWidth().MinHeight(controlMinHeight(c)).Autocorrect(false)
		if !touch(c) {
			search.AutoFocus()
		}
		if search.Changed() {
			state.active = -1
		}
		matches := filterModelChoices(models, state.query)
		selectModel := func(model proto.Model) {
			if s.providerAvailable(model.Provider) {
				state.open = false
				choose(model)
			}
		}
		search.OnTextKey(func(k ui.TextKey) bool {
			switch k.Key {
			case ui.KeyDown, ui.KeyUp:
				step := 1
				if k.Key == ui.KeyUp {
					step = -1
				}
				for i := state.active + step; i >= 0 && i < len(matches); i += step {
					if s.providerAvailable(matches[i].Provider) {
						state.active, state.reveal = i, true
						break
					}
				}
				return true
			case ui.KeyEnter:
				if state.active >= 0 && state.active < len(matches) {
					selectModel(matches[state.active])
				} else {
					for _, model := range matches {
						if s.providerAvailable(model.Provider) {
							selectModel(model)
							break
						}
					}
				}
				return true
			}
			return false
		})
		if len(matches) == 0 {
			faint(c, i18n.T("No matching models"), 12).Padding(12, 8)
			return
		}
		rowHeight := max(float32(34), controlMinHeight(c))
		availableHeight := min(visibleHeight*0.55, max(above, below))
		listHeight := min(float32(min(len(matches), 10))*rowHeight, max(rowHeight, availableHeight-controlMinHeight(c)-16))
		ui.Scroll(c).FillWidth().MaxHeight(listHeight).Gap(0).Children(func() {
			for i, model := range matches {
				i, model := i, model
				selected := model.Provider == provider && model.ID == modelID
				row := clickableRow(c, modelChoiceLabel(model, provider), func() {
					ui.Box(c).Width(16).Children(func() {
						if selected {
							ui.Icon(c, lucide.Check()).FontSize(13).TextColor(t.Accent)
						}
					})
					ui.Text(c, modelChoiceLabel(model, provider)).FontSize(12).SingleLine().MinWidth(0).Grow(1)
				}).Height(rowHeight).Shrink(0).Padding(4, 8).Gap(6).Radius(6).KeepFocus().
					Disabled(!s.providerAvailable(model.Provider)).Tooltip(model.ID + " · " + model.Provider)
				if row.Hovered() || i == state.active {
					row.Background(t.SurfaceHover)
				}
				if i == state.active && state.reveal {
					row.ScrollIntoView()
					state.reveal = false
				}
				row.OnClick(func() { selectModel(model) })
			}
		})
	})
}
