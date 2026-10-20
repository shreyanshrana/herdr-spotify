#!/bin/sh
set -eu

version=${1:-0.1.0}
version=${version#v}
case "$version" in
    ''|*[!0-9A-Za-z._-]*) printf '%s\n' 'Invalid release version' >&2; exit 1 ;;
esac

root=$(CDPATH= cd -- "$(dirname -- "$0")/.." && pwd)
cd "$root"
manifest_version=$(sed -n 's/^version = "\([^"]*\)"$/\1/p' herdr-plugin.toml)
if [ "$version" != "$manifest_version" ]; then
    printf 'Release version %s does not match manifest version %s\n' "$version" "$manifest_version" >&2
    exit 1
fi
mkdir -p dist
stage=$(mktemp -d "$root/dist/.package.XXXXXX")
trap 'rm -rf "$stage"' EXIT HUP INT TERM

for target in darwin_arm64 darwin_amd64 linux_arm64 linux_amd64; do
    os=${target%_*}
    arch=${target#*_}
    bundle="herdr-spotify_${version}_${target}"
    mkdir -p "$stage/$bundle/bin" "$stage/$bundle/examples" "$stage/$bundle/scripts"
    CGO_ENABLED=0 GOOS="$os" GOARCH="$arch" go build -trimpath -o "$stage/$bundle/bin/herdr-spotify" .
    cp README.md CONTRIBUTING.md LICENSE CHANGELOG.md herdr-plugin.toml "$stage/$bundle/"
    cp examples/herdr-config.toml "$stage/$bundle/examples/"
    cp scripts/setup.sh scripts/login.sh "$stage/$bundle/scripts/"
    COPYFILE_DISABLE=1 tar -czf "dist/$bundle.tar.gz" -C "$stage" "$bundle"
done

checksums="$stage/SHA256SUMS"
for target in darwin_arm64 darwin_amd64 linux_arm64 linux_amd64; do
    archive="herdr-spotify_${version}_${target}.tar.gz"
    (cd dist && shasum -a 256 "$archive") >> "$checksums"
done
cp "$checksums" dist/SHA256SUMS
printf 'Release archives and SHA256SUMS written to %s/dist\n' "$root"
