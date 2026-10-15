#!/bin/sh
# drariux installer — fetches a prebuilt binary from GitHub Releases.
#   curl -fsSL https://raw.githubusercontent.com/jhonnycarpio98-droid/drariuxcli/main/install.sh | sh
# Override with:  DRARIUX_VERSION=v0.1.0  or  DRARIUX_REPO=owner/repo
set -e

REPO="${DRARIUX_REPO:-jhonnycarpio98-droid/drariuxcli}"
VERSION="${DRARIUX_VERSION:-latest}"

if [ "$VERSION" = "latest" ]; then
  VERSION=$(curl -fsSL "https://api.github.com/repos/${REPO}/releases/latest" \
    | grep '"tag_name"' | head -1 | cut -d'"' -f4)
fi
if [ -z "$VERSION" ]; then
  echo "drariux: no released version found for ${REPO}" >&2
  exit 1
fi

OS=$(uname -s | tr '[:upper:]' '[:lower:]')
ARCH=$(uname -m)
case "$ARCH" in
  x86_64|amd64)  ARCH=amd64 ;;
  aarch64|arm64) ARCH=arm64 ;;
  *) echo "drariux: unsupported arch $ARCH" >&2; exit 1 ;;
esac

FILE="drariux_${VERSION}_${OS}_${ARCH}.tar.gz"
URL="https://github.com/${REPO}/releases/download/${VERSION}/${FILE}"

echo "Downloading drariux ${VERSION} (${OS}/${ARCH})..."
TMP=$(mktemp -d)
curl -fsSL "$URL" -o "$TMP/$FILE"
tar -xzf "$TMP/$FILE" -C "$TMP"

DEST="/usr/local/bin"
if [ ! -w "$DEST" ]; then
  DEST="${HOME}/.local/bin"
  mkdir -p "$DEST"
fi
install -m 0755 "$TMP/drariux" "$DEST/drariux" 2>/dev/null \
  || (command -v sudo >/dev/null && sudo install -m 0755 "$TMP/drariux" "/usr/local/bin/drariux")

echo "Installed: $("$DEST/drariux" version 2>/dev/null || /usr/local/bin/drariux version)"
echo "Try: drariux wallet new && drariux chain info"
echo "If it is not on PATH, add: export PATH=\"$DEST:\$PATH\""
rm -rf "$TMP"
