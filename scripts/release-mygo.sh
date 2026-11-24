#!/bin/sh
# Builds the MyGo client for every platform and packages the artifacts
# under the names the download page links to.
#
#   ./scripts/release-mygo.sh              # build and package into release/
#   ./scripts/release-mygo.sh upload       # build, package and upload them
#
# The artifacts carry "mygo" in their names, so they never overwrite the
# NSGo client's files on the download site.
set -eu
cd "$(dirname "$0")/.."

version=$(sed -n 's/.*"version": "\([^"]*\)".*/\1/p' mygo.json)
out=${OUT:-dist-mygo}
release=${RELEASE:-release}
prefix="T3Go-mygo-${version}"

# The Android build needs a JDK of 17 or later.
if java_home=$(/usr/libexec/java_home -v 17 2>/dev/null); then
	JAVA_HOME=$java_home
	export JAVA_HOME
fi

mkdir -p "$release"

# package copies a built file into the release under its published name.
package() {
	src=$1
	name=$2
	if [ ! -f "$src" ]; then
		echo "missing $src" >&2
		exit 1
	fi
	cp "$src" "$release/$name"
	echo "packaged $name"
}

# zip_dir packs a directory into a zip, the way the NSGo release shipped
# the macOS app and the iOS simulator app.
zip_dir() {
	dir=$1
	name=$2
	rm -f "$release/$name"
	(cd "$dir" && zip -q -r "$OLDPWD/$release/$name" .)
	echo "packaged $name"
}

for target in macos_arm64 macos_x64 windows_x64 windows_arm64 linux_x64 linux_arm64; do
	echo "building $target"
	./scripts/build-app.sh "$target" "$out"
done

echo "building android_arm64"
./scripts/build-app.sh android_arm64 "$out"
package "$out/android/t3go-${version}.apk" "$prefix-android_arm64.apk"

echo "building android_x64"
./scripts/build-app.sh android_x64 "$out"
package "$out/android/t3go-${version}.apk" "$prefix-android_x64.apk"

echo "building ios_simulator"
./scripts/build-app.sh ios_simulator "$out"
zip_dir "$out/ios-simulator" "$prefix-ios_simulator.zip"

package "$out/darwin-arm64/t3go-${version}-arm64.dmg" "$prefix-macos_arm64.dmg"
package "$out/darwin-amd64/t3go-${version}-amd64.dmg" "$prefix-macos_x64.dmg"
zip_dir "$out/darwin-arm64" "$prefix-macos_arm64.zip"
zip_dir "$out/darwin-amd64" "$prefix-macos_x64.zip"
zip_dir "$out/windows-amd64" "$prefix-windows_x64.zip"
zip_dir "$out/windows-arm64" "$prefix-windows_arm64.zip"
package "$out/linux-amd64/t3go-${version}-linux-amd64.tar.gz" "$prefix-linux_x64.tar.gz"
package "$out/linux-arm64/t3go-${version}-linux-arm64.tar.gz" "$prefix-linux_arm64.tar.gz"

cd "$release"
shasum -a 256 ./* > SHA256SUMS
cd ..
echo "release ready in $release:"
ls -la "$release"

if [ "${1:-}" = "upload" ]; then
	./scripts/upload-mygo.sh "$release"
fi
