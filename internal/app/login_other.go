//go:build !(js && wasm)

package app

// pageOrigin is only meaningful in a browser.
func pageOrigin() string { return "" }

// pageFragment is only meaningful in a browser.
func pageFragment() string { return "" }
