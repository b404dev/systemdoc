#!/bin/sh
# Download a verified release binary. Keep execution at the end so a truncated
# curl response cannot start installation with an incomplete function body.
set -eu

main() {
    repo=${SYSTEMDOC_REPO:-OWNER/systemdoc}
    version=${SYSTEMDOC_VERSION:-latest}
    install_dir=${SYSTEMDOC_INSTALL_DIR:-"$HOME/.local/bin"}
    fail() { printf 'systemdoc: %s\n' "$*" >&2; exit 1; }

    case "$repo" in
        OWNER/*) fail 'The public repository has not been configured yet.' ;;
        *[!a-zA-Z0-9_./-]*|/*|*/|*/*/*|*[.][.]*|'') fail 'Invalid SYSTEMDOC_REPO; expected owner/repository.' ;;
        */*) ;;
        *) fail 'Invalid SYSTEMDOC_REPO; expected owner/repository.' ;;
    esac
    case "$(uname -s)" in
        Linux) platform=linux ;;
        Darwin) platform=darwin ;;
        *) fail 'Release binaries support Linux and macOS.' ;;
    esac
    case "$(uname -m)" in
        x86_64|amd64) arch=amd64 ;;
        aarch64|arm64) arch=arm64 ;;
        *) fail 'Supported architectures: x86_64 (amd64) and aarch64 (arm64).' ;;
    esac
    if [ "$platform" = darwin ] && [ "$arch" != arm64 ]; then
        fail 'macOS support requires Apple Silicon (arm64). Use a native terminal rather than Rosetta.'
    fi
    for tool in curl mktemp awk mkdir cp chmod mv rm; do
        command -v "$tool" >/dev/null 2>&1 || fail "Required command missing: $tool"
    done
    if command -v sha256sum >/dev/null 2>&1; then checksum=sha256sum
    elif command -v shasum >/dev/null 2>&1; then checksum=shasum
    else fail 'Required checksum tool missing: sha256sum or shasum.'; fi
    [ -n "$install_dir" ] || fail 'Installation directory must not be empty.'
    case "$install_dir" in /*) ;; *) fail 'SYSTEMDOC_INSTALL_DIR must be an absolute path.' ;; esac

    if [ "$version" = latest ]; then
        release_url=$(curl --proto '=https' --tlsv1.2 -fsSL --retry 3 -o /dev/null -w '%{url_effective}' "https://github.com/$repo/releases/latest") || fail 'Could not find the latest release. Check the repository and your connection.'
        case "$release_url" in
            "https://github.com/$repo/releases/tag/"*) version=${release_url##*/} ;;
            *) fail 'GitHub did not return a release tag. Publish a release first.' ;;
        esac
    fi
    case "$version" in ''|*[!a-zA-Z0-9.+-]*) fail 'Invalid release version.' ;; esac
    case "$version" in v[0-9]*) ;; *) fail 'Release versions must start with v and a number, for example v0.1.0.' ;; esac

    asset=systemdoc-$platform-$arch
    base=https://github.com/$repo/releases/download/$version
    temporary=$(mktemp -d)
    staged=''
    trap 'rm -rf -- "$temporary"; if [ -n "$staged" ]; then rm -f -- "$staged"; fi' EXIT
    trap 'exit 130' INT
    trap 'exit 143' TERM HUP
    printf 'Downloading Systemdoc %s for %s %s…\n' "$version" "$platform" "$arch"
    curl --proto '=https' --tlsv1.2 -fsSL --retry 3 "$base/$asset" -o "$temporary/$asset" || fail 'Binary download failed; existing installation was not changed.'
    curl --proto '=https' --tlsv1.2 -fsSL --retry 3 "$base/SHA256SUMS" -o "$temporary/SHA256SUMS" || fail 'Checksum download failed; existing installation was not changed.'
    expected=$(awk -v file="$asset" 'NF == 2 && $2 == file {print $1}' "$temporary/SHA256SUMS")
    case "$expected" in ''|*[!0-9a-fA-F]*) fail 'Release checksum is missing or malformed.' ;; esac
    [ "${#expected}" -eq 64 ] || fail 'Release checksum is missing or duplicated.'
    if [ "$checksum" = sha256sum ]; then actual=$(sha256sum "$temporary/$asset"); else actual=$(shasum -a 256 "$temporary/$asset"); fi
    actual=${actual%% *}
    [ "$expected" = "$actual" ] || fail 'Checksum mismatch; existing installation was not changed.'
    [ -s "$temporary/$asset" ] || fail 'Downloaded binary is empty.'

    mkdir -p -- "$install_dir"
    [ ! -d "$install_dir/systemdoc" ] || fail 'Destination is a directory, not an executable.'
    staged=$(mktemp "$install_dir/.systemdoc.XXXXXX")
    cp -- "$temporary/$asset" "$staged"
    chmod 755 "$staged"
    if [ "$platform" = darwin ]; then
        mv -fh "$staged" "$install_dir/systemdoc"
    else
        mv -fT -- "$staged" "$install_dir/systemdoc"
    fi
    staged=''
    printf 'Installed %s\nRun: %s/systemdoc\n' "$version" "$install_dir"
    case ":$PATH:" in
        *":$install_dir:"*) ;;
        *) printf 'Add %s to PATH in your shell configuration to run systemdoc by name.\n' "$install_dir" ;;
    esac
}

main "$@"
