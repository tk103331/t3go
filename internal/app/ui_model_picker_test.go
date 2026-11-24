package app

import (
	"fmt"
	"testing"

	"github.com/egoist/mygo/ui"

	"github.com/tk/t3go/internal/i18n"
	"github.com/tk/t3go/internal/proto"
)

func modelPickerTestSession(count int) *Session {
	s := &Session{providers: []proto.Provider{
		{ID: "alpha", Installed: true}, {ID: "beta", Installed: true}, {ID: "offline"},
	}}
	for i := 0; i < count; i++ {
		provider := "alpha"
		if i >= 10 {
			provider = "beta"
		}
		s.models = append(s.models, proto.Model{ID: fmt.Sprintf("id-%02d", i), Name: fmt.Sprintf("Model %02d", i), Provider: provider})
	}
	return s
}

func TestUIModelPickerSearch(t *testing.T) {
	for _, width := range []int{320, 1000} {
		t.Run(fmt.Sprint(width), func(t *testing.T) {
			s := modelPickerTestSession(24)
			var picked proto.Model
			tt := ui.NewTester(func(c *ui.Context) {
				ui.Column(c).Fill().Justify(ui.End).Padding(16).Children(func() {
					button := ui.Button(c, "Models")
					s.modelPicker(c, button, "alpha", "id-01", func(model proto.Model) { picked = model })
				})
			}, width, 640)
			if width < 760 {
				tt.SetPreferences(ui.Preferences{Pointer: ui.PointerCoarse, NoHover: true})
			}
			if err := tt.Click("Models"); err != nil {
				t.Fatal(err)
			}
			search, ok := tt.Access(i18n.T("Search models"))
			if !ok || search.Bounds.X < 0 || search.Bounds.X+search.Bounds.W > float32(width) || search.Bounds.H < 28 {
				t.Fatalf("search is missing or clipped: %+v", search.Bounds)
			}
			if err := tt.Click(i18n.T("Search models")); err != nil {
				t.Fatal(err)
			}
			tt.Type("model 19")
			if _, ok := tt.Access("Model 19 · beta"); !ok {
				t.Fatal("search did not match the display name")
			}
			tt.Key(ui.Cmd, ui.KeyA)
			tt.Type("BETA id-19")
			if _, ok := tt.Access("Model 19 · beta"); !ok {
				t.Fatalf("search did not find a model outside the first ten: %v", tt.Texts())
			}
			if tt.HasText("Model 00") {
				t.Fatal("search left nonmatching models visible")
			}
			tt.Key(0, ui.KeyEnter)
			if picked.ID != "id-19" || picked.Provider != "beta" {
				t.Fatalf("search did not preserve model/provider identity: %+v", picked)
			}
			if _, ok := tt.Access(i18n.T("Search models")); ok {
				t.Fatal("picker did not close after selection")
			}
			if err := tt.Click("Models"); err != nil {
				t.Fatal(err)
			}
			if !tt.HasText("Model 00") {
				t.Fatal("opening the picker did not reset the search")
			}
			if err := tt.Click(i18n.T("Search models")); err != nil {
				t.Fatal(err)
			}
			tt.Type("no-such-model")
			if !tt.HasText(i18n.T("No matching models")) {
				t.Fatal("empty search results have no explanation")
			}
			tt.Key(0, ui.KeyEscape)
			if _, ok := tt.Access(i18n.T("Search models")); ok {
				t.Fatal("Escape did not dismiss the search picker")
			}
		})
	}
}

func TestUIModelPickerShortMenu(t *testing.T) {
	s := modelPickerTestSession(10)
	var picked proto.Model
	tt := ui.NewTester(func(c *ui.Context) {
		button := ui.Button(c, "Models")
		s.modelPicker(c, button, "alpha", "id-01", func(model proto.Model) { picked = model })
	}, 800, 600)
	if err := tt.Click("Models"); err != nil {
		t.Fatal(err)
	}
	if _, ok := tt.Access(i18n.T("Search models")); ok {
		t.Fatal("ten models should retain the short menu")
	}
	if err := tt.ChooseMenuItem("Model 09"); err != nil {
		t.Fatal(err)
	}
	if picked.ID != "id-09" {
		t.Fatalf("short menu selected the wrong model: %+v", picked)
	}
}

func TestUIModelPickerNavigateLongList(t *testing.T) {
	s := modelPickerTestSession(24)
	var picked proto.Model
	tt := ui.NewTester(func(c *ui.Context) {
		ui.Column(c).Fill().Justify(ui.End).Padding(16).Children(func() {
			button := ui.Button(c, "Models")
			s.modelPicker(c, button, "alpha", "id-01", func(model proto.Model) { picked = model })
		})
	}, 320, 568)
	if err := tt.Click("Models"); err != nil {
		t.Fatal(err)
	}
	for range 24 {
		tt.Key(0, ui.KeyDown)
	}
	last, ok := tt.Access("Model 23 · beta")
	if !ok || last.Bounds.Y < 0 || last.Bounds.Y+last.Bounds.H > 568 {
		t.Fatalf("keyboard navigation did not scroll the last model into view: %+v", last.Bounds)
	}
	if err := tt.Click("Model 23 · beta"); err != nil {
		t.Fatal(err)
	}
	if picked.ID != "id-23" {
		t.Fatalf("clicking the last row selected the wrong model: %+v", picked)
	}
}

func TestUIModelPickerCatalogGrowth(t *testing.T) {
	s := modelPickerTestSession(1)
	tt := ui.NewTester(func(c *ui.Context) {
		button := ui.Button(c, "Models")
		s.modelPicker(c, button, "alpha", "id-01", func(proto.Model) {})
	}, 390, 844)
	s.models = modelPickerTestSession(24).models
	tt.Frame()
	if err := tt.Click("Models"); err != nil {
		t.Fatal(err)
	}
	if _, ok := tt.Access(i18n.T("Search models")); !ok {
		t.Fatalf("growing the catalog did not enable search: %v", tt.Texts())
	}
}

func TestUIModelPickerStaysAboveKeyboard(t *testing.T) {
	s := modelPickerTestSession(24)
	var picked proto.Model
	tt := ui.NewTester(func(c *ui.Context) {
		top, right, bottom, left := c.SafeArea()
		ui.Column(c).Fill().Padding(top+16, right+16, bottom+16, left+16).Justify(ui.End).Children(func() {
			button := ui.Button(c, "Models")
			s.modelPicker(c, button, "alpha", "id-01", func(model proto.Model) { picked = model })
		})
	}, 390, 844)
	tt.SetPreferences(ui.Preferences{Pointer: ui.PointerCoarse, NoHover: true})
	if err := tt.Click("Models"); err != nil {
		t.Fatal(err)
	}
	if err := tt.Click(i18n.T("Search models")); err != nil {
		t.Fatal(err)
	}
	tt.SetInsets(24, 0, 300, 0)
	tt.Frame()
	tt.Type("id-23")
	tt.Frame()
	for _, label := range []string{i18n.T("Search models"), "Model 23 · beta"} {
		item, ok := tt.Access(label)
		if !ok || item.Bounds.Y < 24 || item.Bounds.Y+item.Bounds.H > 544 {
			t.Fatalf("%s is hidden by the keyboard: %+v", label, item.Bounds)
		}
	}
	if err := tt.Click("Model 23 · beta"); err != nil {
		t.Fatal(err)
	}
	if picked.ID != "id-23" {
		t.Fatal("tapping a result with the keyboard open did not select it")
	}
}

func TestUIModelPickerDisablesUnavailableProvider(t *testing.T) {
	s := modelPickerTestSession(12)
	s.models[11].Provider = "offline"
	var picked proto.Model
	tt := ui.NewTester(func(c *ui.Context) {
		button := ui.Button(c, "Models")
		s.modelPicker(c, button, "alpha", "id-01", func(model proto.Model) { picked = model })
	}, 800, 600)
	if err := tt.Click("Models"); err != nil {
		t.Fatal(err)
	}
	tt.Type("id-11")
	if _, ok := tt.Access("Model 11 · offline"); !ok {
		t.Fatal("search did not include the unavailable model")
	}
	tt.Key(0, ui.KeyEnter)
	if picked.ID != "" {
		t.Fatal("Enter selected a model whose provider is unavailable")
	}
}
