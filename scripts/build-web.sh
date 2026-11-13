#!/bin/sh
# Builds the web client into DIR (web by default): app.wasm,
# wasm_exec.js and the page. Serve it with `t3go serve --web DIR`.
set -eu
cd "$(dirname "$0")/.."
out=${1:-web}
mkdir -p "$out"
GOOS=js GOARCH=wasm go build -o "$out/app.wasm" ./cmd/t3go-app
cp "$(go env GOROOT)/lib/wasm/wasm_exec.js" "$out/wasm_exec.js"
cp cmd/t3go-app/t3go-mark.svg "$out/t3go-mark.svg"
if [ "$out" != "web" ]; then
  cp web/index.html "$out/index.html"
fi
echo "built $out"
