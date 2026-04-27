#!/bin/sh
# loqsu installer — fetches the latest release from
# https://github.com/PeoneEr/loqsu-cli/releases and installs the
# `loqsu` binary into a bin directory on $PATH.
#
# Usage:
#   curl -fsSL https://raw.githubusercontent.com/PeoneEr/loqsu-cli/main/install.sh | sh
#   curl -fsSL https://raw.githubusercontent.com/PeoneEr/loqsu-cli/main/install.sh | sh -s -- --prefix=$HOME/.local
#   curl -fsSL https://raw.githubusercontent.com/PeoneEr/loqsu-cli/main/install.sh | sh -s -- --version=v0.2.0

set -eu

REPO="PeoneEr/loqsu-cli"
NAME="loqsu"
PREFIX=""
VERSION=""

while [ $# -gt 0 ]; do
    case "$1" in
        --prefix=*)  PREFIX="${1#*=}" ;;
        --prefix)    PREFIX="${2:-}"; shift ;;
        --version=*) VERSION="${1#*=}" ;;
        --version)   VERSION="${2:-}"; shift ;;
        -h|--help)
            sed -n '2,8p' "$0" 2>/dev/null || cat <<'USAGE'
loqsu installer
  curl -fsSL https://loq.su/install.sh | sh
  curl -fsSL https://loq.su/install.sh | sh -s -- --prefix=$HOME/.local
  curl -fsSL https://loq.su/install.sh | sh -s -- --version=v0.2.0
USAGE
            exit 0
            ;;
        *) echo "loqsu: unknown option: $1" >&2; exit 2 ;;
    esac
    shift
done

# --- platform detection ---
case "$(uname -s)" in
    Linux*)  OS=linux ;;
    Darwin*) OS=darwin ;;
    *) echo "loqsu: unsupported OS: $(uname -s)" >&2; exit 1 ;;
esac

case "$(uname -m)" in
    x86_64|amd64) ARCH=amd64 ;;
    arm64|aarch64) ARCH=arm64 ;;
    *) echo "loqsu: unsupported arch: $(uname -m)" >&2; exit 1 ;;
esac

# --- pick install prefix ---
if [ -z "$PREFIX" ]; then
    if [ -w "/usr/local/bin" ] || command -v sudo >/dev/null 2>&1; then
        PREFIX="/usr/local"
    else
        PREFIX="$HOME/.local"
    fi
fi
DEST="$PREFIX/bin"

# --- resolve version ---
if [ -z "$VERSION" ]; then
    VERSION="$(
        curl -fsSL "https://api.github.com/repos/$REPO/releases/latest" \
        | sed -n 's/.*"tag_name": *"\([^"]*\)".*/\1/p' \
        | head -n1
    )"
fi
if [ -z "$VERSION" ]; then
    echo "loqsu: failed to resolve latest release; pass --version=vX.Y.Z" >&2
    exit 1
fi

ASSET="loqsu_${VERSION}_${OS}_${ARCH}.tar.gz"
URL="https://github.com/$REPO/releases/download/$VERSION/$ASSET"

TMP="$(mktemp -d)"
trap 'rm -rf "$TMP"' EXIT

echo "loqsu $VERSION ($OS/$ARCH) -> $DEST"
echo "  fetching $URL"
curl -fsSL "$URL" -o "$TMP/$ASSET"

# --- optional checksum verification ---
SUMS_URL="https://github.com/$REPO/releases/download/$VERSION/SHA256SUMS.txt"
if curl -fsSL "$SUMS_URL" -o "$TMP/SHA256SUMS.txt" 2>/dev/null; then
    expected="$(grep " $ASSET\$" "$TMP/SHA256SUMS.txt" | awk '{print $1}')"
    if [ -n "$expected" ]; then
        if command -v shasum >/dev/null 2>&1; then
            actual="$(shasum -a 256 "$TMP/$ASSET" | awk '{print $1}')"
        elif command -v sha256sum >/dev/null 2>&1; then
            actual="$(sha256sum "$TMP/$ASSET" | awk '{print $1}')"
        else
            actual=""
        fi
        if [ -n "$actual" ] && [ "$actual" != "$expected" ]; then
            echo "loqsu: checksum mismatch for $ASSET" >&2
            echo "  expected $expected" >&2
            echo "  got      $actual"  >&2
            exit 1
        fi
        [ -n "$actual" ] && echo "  sha256 ok"
    fi
fi

tar -xzf "$TMP/$ASSET" -C "$TMP"
if [ ! -f "$TMP/$NAME" ]; then
    echo "loqsu: archive did not contain '$NAME' binary" >&2
    exit 1
fi

mkdir -p "$DEST"
if [ -w "$DEST" ]; then
    install -m 0755 "$TMP/$NAME" "$DEST/$NAME"
else
    echo "  installing into $DEST (sudo)"
    sudo install -m 0755 "$TMP/$NAME" "$DEST/$NAME"
fi

echo
echo "Installed: $("$DEST/$NAME" --version 2>/dev/null || echo "$DEST/$NAME")"

case ":$PATH:" in
    *":$DEST:"*) ;;
    *) echo "Note: $DEST is not on \$PATH yet — add it to your shell rc." ;;
esac

echo "Try:  $NAME https://example.com/very/long/path"
