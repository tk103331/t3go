package main

import (
	"runtime"
	"runtime/debug"
	"strings"
)

const (
	appVersion     = "0.1.0"
	appBuildNumber = "108"
)

func nsgoVersion() string {
	info, ok := debug.ReadBuildInfo()
	if !ok {
		return "unknown"
	}
	for _, dependency := range info.Deps {
		if dependency.Path == "github.com/oneo-me/nsgo" {
			version := dependency.Version
			if dependency.Replace != nil && dependency.Replace.Version != "" {
				version = dependency.Replace.Version
			}
			if strings.TrimSpace(version) != "" {
				return version
			}
		}
	}
	return "unknown"
}

func goRuntimeVersion() string {
	return runtime.Version() + " · " + runtime.GOOS + "/" + runtime.GOARCH
}
