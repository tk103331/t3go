#!/bin/sh
# Builds the Asuka web client into DIR (web by default). Serve it with
# `t3go serve --web DIR`.
set -eu
cd "$(dirname "$0")/.."
out=${1:-web}
mkdir -p "$out"
go tool asuka build --platform web ./cmd/t3go-app
cp -R cmd/t3go-app/dist/web/. "$out/"
echo "built $out"
