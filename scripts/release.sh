#!/bin/sh
set -eu
version=${1:?Usage: sh scripts/release.sh vX.Y.Z}
case "$version" in ''|*[!a-zA-Z0-9.+-]*) echo 'Invalid version' >&2; exit 1 ;; esac
case "$version" in v[0-9]*) ;; *) echo 'Version must start with v and a number' >&2; exit 1 ;; esac
mkdir -p dist
for platform in linux darwin; do
architectures="amd64 arm64"
[ "$platform" != darwin ] || architectures=arm64
for arch in $architectures; do
    CGO_ENABLED=0 GOOS=$platform GOARCH=$arch go build -trimpath -ldflags "-s -w -X main.version=$version" -o "dist/systemdoc-$platform-$arch" ./cmd/systemdoc
done
done
(cd dist && if command -v sha256sum >/dev/null 2>&1; then sha256sum systemdoc-linux-amd64 systemdoc-linux-arm64 systemdoc-darwin-arm64; else shasum -a 256 systemdoc-linux-amd64 systemdoc-linux-arm64 systemdoc-darwin-arm64; fi > SHA256SUMS)
printf 'Release files written to dist/ for %s\n' "$version"
