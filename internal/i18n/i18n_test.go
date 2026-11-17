package i18n

import "testing"

func TestTranslateFallsBackToEnglish(t *testing.T) {
	Set(English)
	if got := T("Settings"); got != "Settings" {
		t.Fatalf("english translation = %q", got)
	}
}

func TestTranslateUsesChineseCatalog(t *testing.T) {
	Set(Chinese)
	defer Set(English)
	if got := T("Settings"); got != "设置" {
		t.Fatalf("chinese translation = %q", got)
	}
	if got := T("Not in the catalog"); got != "Not in the catalog" {
		t.Fatalf("missing translation = %q", got)
	}
}

func TestTranslateKeepsPlaceholders(t *testing.T) {
	Set(Chinese)
	defer Set(English)
	if got := T("%d files", 3); got != "3 个文件" {
		t.Fatalf("formatted translation = %q", got)
	}
}

func TestNormalize(t *testing.T) {
	for tag, want := range map[string]Lang{
		"zh":      Chinese,
		"zh-CN":   Chinese,
		"ZH_HANS": Chinese,
		"en_US":   English,
		"":        English,
	} {
		if got := Normalize(tag); got != want {
			t.Fatalf("Normalize(%q) = %q, want %q", tag, got, want)
		}
	}
}

func TestSetNotifiesListenersOnce(t *testing.T) {
	changes := 0
	OnChange(func(Lang) { changes++ })
	Set(English)
	Set(Chinese)
	defer Set(English)
	if changes != 1 {
		t.Fatalf("language changes = %d, want 1", changes)
	}
}
