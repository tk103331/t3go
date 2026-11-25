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
  macos_arm64) server_os=darwin; server_arch=arm64; server_name=t3go-server ;;
  macos_x64) server_os=darwin; server_arch=amd64; server_name=t3go-server ;;
  windows_arm64) server_os=windows; server_arch=arm64; server_name=t3go-server.exe ;;
  windows_x64) server_os=windows; server_arch=amd64; server_name=t3go-server.exe ;;
  linux_arm64) server_os=linux; server_arch=arm64; server_name=t3go-server ;;
  linux_x64) server_os=linux; server_arch=amd64; server_name=t3go-server ;;
  # A phone talks to an environment; it never runs one.
  android_arm64|android_x64|ios|ios_simulator) server_os=; server_arch=; server_name= ;;
  *) echo "unknown target: $target" >&2; exit 2 ;;
esac

mkdir -p "$resources"
rm -f "$resources/t3go" "$resources/t3go.exe" "$resources/t3go-server" "$resources/t3go-server.exe"
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
  ios) platform="ios/arm64" ;;
  ios_simulator) platform="ios/simulator" ;;
  android_arm64) platform="android/arm64" ;;
  android_x64) platform="android/amd64" ;;
  macos_arm64) platform="darwin/arm64" ;;
  macos_x64) platform="darwin/amd64" ;;
  windows_arm64) platform="windows/arm64" ;;
  windows_x64) platform="windows/amd64" ;;
  linux_arm64) platform="linux/arm64" ;;
  linux_x64) platform="linux/amd64" ;;
esac
# macOS follows the platform's icon grid: a transparent margin around a
# smaller rounded square (scripts/make-macos-icon.swift). Phones keep the
# square icon, which their launchers mask themselves.
icon=cmd/t3go-app/assets/Icon.png
macos_icon=cmd/t3go-app/assets/Icon-macos.png
icon_backup=
restore_icon() {
  if [ -n "$icon_backup" ]; then
    cp "$icon_backup" "$icon"
    rm -f "$icon_backup"
    icon_backup=
  fi
}
trap restore_icon EXIT INT TERM
case "$target" in
  macos_*)
    if [ -f "$macos_icon" ]; then
      icon_backup=$(mktemp "${TMPDIR:-/tmp}/t3go-icon.XXXXXX")
      cp "$icon" "$icon_backup"
      cp "$macos_icon" "$icon"
    fi
    ;;
esac
# The Windows installer needs NSIS; the download page offers the
# executable in a zip, as the NSGo release did.
go tool mygo build -platform "$platform" -o "$output" -skip-installer
restore_icon
trap - EXIT INT TERM
