#!/bin/sh
# Uploads the MyGo client's artifacts to the download site, next to the
# NSGo client's files, which keep their names.
#
#   ./scripts/upload-mygo.sh release
#
# The site is the environment server's web directory; the host comes from
# the ssh config (t3go-server).
set -eu
cd "$(dirname "$0")/.."

release=${1:-release}
host=${T3GO_RELEASE_HOST:-t3go-server}
remote=${T3GO_RELEASE_DIR:-/opt/t3go/web}

if [ ! -d "$release" ]; then
	echo "no release directory: $release" >&2
	exit 1
fi

echo "uploading to $host:$remote"
scp "$release"/T3Go-mygo-* "$host:$remote/"
scp "$release"/SHA256SUMS "$host:$remote/SHA256SUMS-mygo"
ssh "$host" "ls -la '$remote' | grep -i mygo"
echo "uploaded"
