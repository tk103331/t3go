//go:build !(js && wasm)

package main

func pageOrigin() string { return "" }

func pageFragment() string { return "" }
