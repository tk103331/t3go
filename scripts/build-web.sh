#!/bin/sh
# Builds the landing page into DIR (web by default) and the WASM client
# into DIR/app. Serve it with `t3go serve --web DIR`.
set -eu
cd "$(dirname "$0")/.."
out=${1:-web}
mkdir -p "$out/app"
GOOS=js GOARCH=wasm go build -o "$out/app/app.wasm" ./cmd/t3go-app
cp "$(go env GOROOT)/lib/wasm/wasm_exec.js" "$out/app/wasm_exec.js"
cp cmd/t3go-app/t3go-mark.svg "$out/t3go-mark.svg"
cp cmd/t3go-app/t3go-mark.svg "$out/app/t3go-mark.svg"
out_abs=$(cd "$out" && pwd -P)
source_abs=$(cd web && pwd -P)
if [ "$out_abs" != "$source_abs" ]; then
  cp web/index.html "$out/index.html"
  cp web/app/index.html "$out/app/index.html"
fi
echo "built $out"
