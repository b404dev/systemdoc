#!/bin/sh
set -eu
version=${1:?Usage: sh scripts/release.sh vX.Y.Z}
case "$version" in ''|*[!a-zA-Z0-9.+-]*) echo 'Invalid version' >&2; exit 1 ;; esac
case "$version" in v[0-9]*) ;; *) echo 'Version must start with v and a number' >&2; exit 1 ;; esac
mkdir -p dist
for arch in amd64 arm64; do
    CGO_ENABLED=0 GOOS=linux GOARCH=$arch go build -trimpath -ldflags "-s -w -X main.version=$version" -o "dist/systemdoc-linux-$arch" ./cmd/systemdoc
done
(cd dist && sha256sum systemdoc-linux-amd64 systemdoc-linux-arm64 > SHA256SUMS)
printf 'Release files written to dist/ for %s\n' "$version"
