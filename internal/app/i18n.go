package app

import (
	"github.com/tk/t3go/internal/i18n"
)

// clientLanguage is the language the interface is drawn in, kept on this
// client, so every environment speaks the same language.
var clientLanguage i18n.Lang

// initClientLanguage loads the saved language, or detects it on first
// launch, and makes the translator follow it.
func initClientLanguage(store appStorage) {
	lang := i18n.Detect()
	if raw, ok := store.GetItem("language:v1"); ok && raw != "" {
		lang = i18n.Normalize(raw)
	}
	i18n.Set(lang)
	clientLanguage = lang
	i18n.Track(func() any { return clientLanguage })
	i18n.OnChange(func(next i18n.Lang) {
		clientLanguage = next
		_ = store.SetItem("language:v1", string(next))
	})
}

// setClientLanguage switches the interface language and keeps the choice
// on this client.
func setClientLanguage(lang i18n.Lang) {
	i18n.Set(lang)
}
