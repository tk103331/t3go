// Package i18n translates the client's interface strings.
//
// The catalog is keyed by the English source text, so a string without a
// translation keeps its English wording instead of showing an empty label.
package i18n

import (
	"fmt"
	"os"
	"strings"
	"sync"
)

// Lang is a supported interface language.
type Lang string

const (
	// English is the default language.
	English Lang = "en"
	// Chinese is the simplified Chinese translation.
	Chinese Lang = "zh"
)

var (
	mu        sync.RWMutex
	current   = English
	tracker   func() any
	listeners []func(Lang)
)

// Languages lists the supported languages in menu order.
func Languages() []Lang {
	return []Lang{English, Chinese}
}

// Name returns the language's own name, as its menu entry shows it.
func (l Lang) Name() string {
	switch l {
	case Chinese:
		return "中文"
	default:
		return "English"
	}
}

// Normalize maps a language tag onto a supported language.
func Normalize(tag string) Lang {
	tag = strings.ToLower(strings.TrimSpace(tag))
	tag = strings.ReplaceAll(tag, "_", "-")
	switch tag {
	case "zh", "zh-cn", "zh-hans", "zh-hans-cn", "cn", "chinese", "zh-tw", "zh-hant":
		return Chinese
	default:
		if strings.HasPrefix(tag, "zh") {
			return Chinese
		}
		return English
	}
}

// Detect returns the language the operating system asks for.
func Detect() Lang {
	for _, key := range []string{"LC_ALL", "LC_MESSAGES", "LANG"} {
		if value := strings.TrimSpace(os.Getenv(key)); value != "" {
			return Normalize(value)
		}
	}
	return English
}

// Current returns the active language.
func Current() Lang {
	mu.RLock()
	defer mu.RUnlock()
	return current
}

// Set changes the interface language and tells the listeners about it.
func Set(lang Lang) {
	lang = Normalize(string(lang))
	mu.Lock()
	if current == lang {
		mu.Unlock()
		return
	}
	current = lang
	callbacks := append([]func(Lang){}, listeners...)
	mu.Unlock()
	for _, callback := range callbacks {
		callback(lang)
	}
}

// OnChange registers a callback run when the language changes.
func OnChange(callback func(Lang)) {
	mu.Lock()
	defer mu.Unlock()
	listeners = append(listeners, callback)
}

// Track registers a function the translator reads on every lookup. The
// client uses it to rebuild the interface when the language changes.
func Track(fn func() any) {
	mu.Lock()
	defer mu.Unlock()
	tracker = fn
}

// T translates key into the active language. Arguments format the
// translation, so a translated template keeps its placeholders.
func T(key string, args ...any) string {
	mu.RLock()
	lang := current
	read := tracker
	mu.RUnlock()
	if read != nil {
		read()
	}
	text := key
	if lang == Chinese {
		if translated, ok := zh[key]; ok {
			text = translated
		}
	}
	if len(args) == 0 {
		return text
	}
	return fmt.Sprintf(text, args...)
}
