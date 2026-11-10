#!/bin/sh
# Starts a local environment, the web client and the desktop client for
# debugging. Ctrl+C stops what this script started.
#
#   ./scripts/dev.sh                 # server + web + desktop
#   T3GO_DEV_APP=0 ./scripts/dev.sh  # server + web in this terminal
#   T3GO_DEV_WEB=0 ./scripts/dev.sh  # server + desktop
#   T3GO_DEV_LISTEN=127.0.0.1:4899 ./scripts/dev.sh
set -eu
cd "$(dirname "$0")/.."

home=${T3GO_DEV_HOME:-.dev/t3go}
listen=${T3GO_DEV_LISTEN:-127.0.0.1:4870}
with_web=${T3GO_DEV_WEB:-1}
with_app=${T3GO_DEV_APP:-1}

mkdir -p "$home"
go build -o bin/t3go ./cmd/t3go

server_args="--home $home --listen $listen"
if [ "$with_web" = "1" ]; then
  ./scripts/build-web.sh web >/dev/null
  server_args="$server_args --web web"
fi

if [ "$with_app" = "1" ]; then
  go build -o bin/t3go-app ./cmd/t3go-app
  ./bin/t3go serve $server_args >/tmp/t3go-dev-server.log 2>&1 &
  server=$!
  trap 'kill $server 2>/dev/null || true; wait $server 2>/dev/null || true' EXIT INT TERM

  ready=0
  i=0
  while [ "$i" -lt 100 ]; do
    if curl -fsS "http://$listen/api/v1/health" >/dev/null 2>&1; then
      ready=1
      break
    fi
    i=$((i + 1))
    sleep 0.1
  done
  if [ "$ready" = "0" ]; then
    echo "t3go did not answer on $listen; see /tmp/t3go-dev-server.log" >&2
    exit 1
  fi

  token=$(cat "$home/token")
  echo "server  http://$listen"
  echo "home    $home"
  echo "pair    t3go://pair?url=http://$listen&token=$token"
  echo "token   $token"
  if [ "$with_web" = "1" ]; then
    echo "web     http://$listen/"
  fi
  echo "app     connecting automatically"

  T3GO_SERVER="http://$listen" T3GO_TOKEN="$token" ./bin/t3go-app
  exit $?
fi

echo "server  http://$listen"
echo "home    $home"
echo "token   $home/token"
if [ "$with_web" = "1" ]; then
  echo "web     http://$listen/"
fi
exec ./bin/t3go serve $server_args
