#!/bin/sh
# Builds the client with the environment server it ships, so an installed
# app works without a separately installed server.
#
#   ./scripts/build-app.sh                  # this machine's package
#   ./scripts/build-app.sh macos_arm64      # a named target
#   ./scripts/build-app.sh android_arm64    # a phone package
set -eu
cd "$(dirname "$0")/.."

case "$(uname -s)-$(uname -m)" in
  Darwin-arm64) default=macos_arm64 ;;
  Darwin-x86_64) default=macos_x64 ;;
  Linux-aarch64) default=linux_arm64 ;;
  *) default=linux_x64 ;;
esac

target=${1:-$default}
output=${2:-dist}
resources=resources

case "$target" in
  macos_arm64) server_os=darwin; server_arch=arm64; server_name=t3go ;;
  macos_x64) server_os=darwin; server_arch=amd64; server_name=t3go ;;
  windows_arm64) server_os=windows; server_arch=arm64; server_name=t3go.exe ;;
  windows_x64) server_os=windows; server_arch=amd64; server_name=t3go.exe ;;
  linux_arm64) server_os=linux; server_arch=arm64; server_name=t3go ;;
  linux_x64) server_os=linux; server_arch=amd64; server_name=t3go ;;
  # A phone talks to an environment; it never runs one.
  android_arm64|android_x64|ios|ios_simulator) server_os=; server_arch=; server_name= ;;
  *) echo "unknown target: $target" >&2; exit 2 ;;
esac

mkdir -p "$resources"
rm -f "$resources/t3go" "$resources/t3go.exe"
# A phone talks to an environment; it never runs one. MyGo copies the
# resources directory into every platform's app.
if [ -n "$server_os" ]; then
  echo "building the environment server for $server_os/$server_arch"
  GOOS=$server_os GOARCH=$server_arch CGO_ENABLED=0 go build -o "$resources/$server_name" ./cmd/t3go
  if [ "$server_os" = "darwin" ]; then
    # The package signs its bundle, so the server inside it carries its
    # own signature first.
    codesign --force --sign - "$resources/$server_name"
  fi
fi

echo "building the client for $target"
case "$target" in
  ios|ios_simulator) platform="ios/simulator" ;;
  android*) platform="android/${target#android_}" ;;
  macos_arm64) platform="darwin/arm64" ;;
  macos_x64) platform="darwin/x64" ;;
  windows_arm64) platform="windows/arm64" ;;
  windows_x64) platform="windows/x64" ;;
  linux_arm64) platform="linux/arm64" ;;
  linux_x64) platform="linux/x64" ;;
esac
go tool mygo build -platform "$platform" -out "$output"
