package main

import (
	"github.com/oneo-me/nsgo/core"
	"github.com/tk/t3go/internal/i18n"
)

// clientLanguage holds the language the interface is drawn in. The
// translator reads it on every lookup, so a change redraws the interface.
var clientLanguage *core.Signal[string]

// initClientLanguage loads the saved language and lets the interface
// follow it. The choice is kept on this client, so every environment
// speaks the same language.
func initClientLanguage(cx *core.Scope, store core.Storage) {
	clientLanguage = core.NewSignal(cx, string(i18n.Current()))
	lang := i18n.Detect()
	if raw, ok := store.GetItem("language:v1"); ok && raw != "" {
		lang = i18n.Normalize(raw)
	}
	i18n.Set(lang)
	clientLanguage.Set(string(lang))
	i18n.Track(func() any { return clientLanguage.Get() })
	i18n.OnChange(func(next i18n.Lang) {
		clientLanguage.Set(string(next))
		_ = store.SetItem("language:v1", string(next))
	})
}

// setClientLanguage switches the interface language and keeps the choice
// on this client.
func (a *app) setClientLanguage(lang i18n.Lang) {
	i18n.Set(lang)
}
