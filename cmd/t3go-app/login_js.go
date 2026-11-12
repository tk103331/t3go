//go:build js && wasm

package main

import (
	"syscall/js"
)

// pageOrigin is the web client's own address, which serves the
// environment too.
func pageOrigin() string {
	if loc := js.Global().Get("location"); !loc.IsUndefined() {
		if origin := loc.Get("origin").String(); origin != "" && origin != "null" {
			return origin
		}
	}
	return ""
}

// pageFragment is the page's URL fragment, so a link like
// https://host/#token=... logs in without typing.
func pageFragment() string {
	if loc := js.Global().Get("location"); !loc.IsUndefined() {
		return loc.Get("hash").String()
	}
	return ""
}
