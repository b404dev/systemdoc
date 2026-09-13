#!/bin/sh
# Offline contract tests: mock only the network and host architecture.
set -eu
root=$(CDPATH='' cd -- "$(dirname "$0")/.." && pwd)
fixture=$(mktemp -d)
export TEST_REAL_OS="$(uname -s)"
export TEST_REAL_MV="$(command -v mv)"
trap 'rm -rf -- "$fixture"' EXIT
mkdir -p "$fixture/mock" "$fixture/assets" "$fixture/install space"
cat > "$fixture/mock/uname" <<'MOCK'
#!/bin/sh
case "$1" in
 -s) printf '%s\n' "${MOCK_OS:-Linux}" ;;
 -m) printf '%s\n' "${MOCK_ARCH:-x86_64}" ;;
esac
MOCK
cat > "$fixture/mock/curl" <<'MOCK'
#!/bin/sh
set -eu
output=''
url=''
while [ "$#" -gt 0 ]; do
 case "$1" in
  -o) output=$2; shift 2 ;;
  -w|--proto|--retry) shift 2 ;;
  https://*) url=$1; shift ;;
  *) shift ;;
 esac
done
printf '%s\n' "$url" >> "$MOCK_ASSETS/requests"
case "$url" in
 */releases/latest) printf 'https://github.com/example/systemdoc/releases/tag/v1.2.3' ;;
 */releases/download/v1.2.3/*)
  [ "${MOCK_FAIL:-0}" = 0 ] || exit 22
  cp "$MOCK_ASSETS/${url##*/}" "$output"
  if [ "${MOCK_CORRUPT:-0}" = 1 ] && [ "${url##*/}" != SHA256SUMS ]; then printf 'tampered' >> "$output"; fi
  ;;
 *) exit 22 ;;
esac
MOCK
# Exercise both platform option paths using the host's actual mv implementation.
cat > "$fixture/mock/mv" <<'MOCK'
#!/bin/sh
set -eu
case "$1" in -fh|-fT) shift ;; *) exit 2 ;; esac
[ "$1" != -- ] || shift
if [ "$TEST_REAL_OS" = Darwin ]; then exec "$TEST_REAL_MV" -fh "$@"; else exec "$TEST_REAL_MV" -fT -- "$@"; fi
MOCK
chmod +x "$fixture/mock/uname" "$fixture/mock/curl" "$fixture/mock/mv"
printf '#!/bin/sh\necho amd64\n' > "$fixture/assets/systemdoc-linux-amd64"
printf '#!/bin/sh\necho arm64\n' > "$fixture/assets/systemdoc-linux-arm64"
printf '#!/bin/sh\necho mac-arm64\n' > "$fixture/assets/systemdoc-darwin-arm64"
(cd "$fixture/assets" && if command -v sha256sum >/dev/null 2>&1; then sha256sum systemdoc-*; else shasum -a 256 systemdoc-*; fi > SHA256SUMS)
export MOCK_ASSETS="$fixture/assets"
export PATH="$fixture/mock:$PATH"
export SYSTEMDOC_REPO=example/systemdoc
export SYSTEMDOC_INSTALL_DIR="$fixture/install space"
unset SYSTEMDOC_VERSION
sh "$root/install.sh" > "$fixture/output"
[ "$("$SYSTEMDOC_INSTALL_DIR/systemdoc")" = amd64 ]
[ "$(wc -l < "$fixture/assets/requests")" -eq 3 ]
export SYSTEMDOC_VERSION=v1.2.3
MOCK_OS=Darwin MOCK_ARCH=arm64 sh "$root/install.sh" > "$fixture/output"
[ "$("$SYSTEMDOC_INSTALL_DIR/systemdoc")" = mac-arm64 ]
# A Mac's standard shasum path must also work without GNU sha256sum.
mkdir -p "$fixture/shasum-path"
for tool in curl uname mv; do ln -s "$fixture/mock/$tool" "$fixture/shasum-path/$tool"; done
for tool in shasum mktemp awk mkdir cp chmod rm; do ln -s "$(command -v "$tool")" "$fixture/shasum-path/$tool"; done
PATH="$fixture/shasum-path" MOCK_OS=Darwin MOCK_ARCH=arm64 /bin/sh "$root/install.sh" > "$fixture/output"
[ "$("$SYSTEMDOC_INSTALL_DIR/systemdoc")" = mac-arm64 ]
MOCK_OS=Linux MOCK_ARCH=aarch64 sh "$root/install.sh" > "$fixture/output"
[ "$("$SYSTEMDOC_INSTALL_DIR/systemdoc")" = arm64 ]
expect_failure() {
 if env "$@" sh "$root/install.sh" > "$fixture/output" 2>&1; then
  echo "Installer unexpectedly succeeded: $*" >&2; exit 1
 fi
 [ "$("$SYSTEMDOC_INSTALL_DIR/systemdoc")" = arm64 ]
}
expect_failure MOCK_OS=Darwin MOCK_ARCH=x86_64
expect_failure MOCK_OS=Darwin MOCK_ARCH=amd64
expect_failure MOCK_OS=Darwin MOCK_ARCH=arm64 MOCK_CORRUPT=1
expect_failure MOCK_OS=Darwin MOCK_ARCH=arm64 MOCK_FAIL=1
expect_failure MOCK_CORRUPT=1
expect_failure MOCK_FAIL=1
expect_failure MOCK_ARCH=armv7l
expect_failure MOCK_OS=FreeBSD
expect_failure SYSTEMDOC_VERSION=../../bad
expect_failure SYSTEMDOC_REPO=bad
printf 'missing\n' > "$fixture/assets/SHA256SUMS"
expect_failure
printf 'Installer tests passed (latest, pinned, Linux/macOS, both architectures, paths with spaces, and failure preservation).\n'
