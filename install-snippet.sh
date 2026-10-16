set -e
V="${DRARIUX_VERSION:-v0.5.5}"
BASE="${DRARIUX_BASE:-http://16.16.47.157:8788}"        # IP-hosted release folder (no DNS)
REPO="${DRARIUX_REPO:-jhonnycarpio98-droid/drariuxcli}" # GitHub fallback (used if IP base unreachable)

OS=$(uname -s | tr '[:upper:]' '[:lower:]')
case "$OS" in
  linux|darwin) : ;;
  *) echo "drariux: unsupported OS '$OS'"; exit 1 ;;
esac

A=$(uname -m)
case "$A" in
  x86_64|amd64)   A=amd64 ;;
  aarch64|arm64)  A=arm64 ;;   # includes 64-bit Termux on Android
  armv7l|armv8l|arm|armhf)
    echo "drariux: 32-bit ARM has no build; install the 64-bit (arm64-v8a) Termux."
    exit 1 ;;
  *) echo "drariux: unsupported arch '$A'"; exit 1 ;;
esac

# Pick an install dir already on PATH where possible.
# Termux: $PREFIX/bin is on PATH by default; ~/.local/bin is NOT.
if [ -n "${PREFIX:-}" ] && printf '%s' "$PREFIX" | grep -q 'com\.termux'; then
  D="$PREFIX/bin"
elif [ -w /usr/local/bin ]; then
  D=/usr/local/bin
else
  D="$HOME/.local/bin"
fi
mkdir -p "$D"

F="drariux_${V}_${OS}_${A}.tar.gz"
echo ">> installing drariux ${V} (${OS}/${A}) -> $D"
T=$(mktemp -d)
if curl -fsSL "$BASE/$F" -o "$T/$F"; then
  SRC="$BASE"
else
  echo "   IP base unreachable, trying GitHub releases…"
  curl -fsSL "https://github.com/${REPO}/releases/download/${V}/${F}" -o "$T/$F"
  SRC="github"
fi
tar -xzf "$T/$F" -C "$T"
cp -f "$T/drariux" "$D/drariux"     # overwrite any older version
chmod 0755 "$D/drariux"
rm -rf "$T"

echo
echo "   source: $SRC"
"$D/drariux" version
case ":$PATH:" in
  *":$D:"*) ;;
  *) echo; echo "add to PATH:  export PATH=\"$D:\$PATH\"";;
esac
