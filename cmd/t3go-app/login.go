package main

import (
	"net/url"
	"strings"

	"github.com/tk/t3go/internal/client"
)

// parseLoginFragment reads a page fragment like "#token=..." or
// "#t3go://pair?url=...&token=...", so a pasted or bookmarked link logs
// in without typing anything.
func parseLoginFragment(fragment string) (baseURL, token string) {
	raw := strings.TrimPrefix(fragment, "#")
	if raw == "" {
		return "", ""
	}
	// A pair link pasted whole into the fragment.
	if u, t, ok := client.ParsePairing(raw); ok {
		return u, t
	}
	// The pair link may arrive percent-encoded inside the fragment.
	if decoded, err := url.QueryUnescape(raw); err == nil && decoded != raw {
		if u, t, ok := client.ParsePairing(decoded); ok {
			return u, t
		}
	}
	// Key=value pairs, upstream t3's "#token=..." form.
	values, err := url.ParseQuery(raw)
	if err != nil {
		return "", ""
	}
	if link := values.Get("pair"); link != "" {
		u, t, ok := client.ParsePairing(link)
		if ok {
			return u, t
		}
	}
	return values.Get("url"), values.Get("token")
}
