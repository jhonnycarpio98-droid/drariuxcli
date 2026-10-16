#!/bin/bash
# arm64 static-cgo cross build (for 64-bit Termux). Needs the aarch64 cross
# compiler + static libc; netgo keeps DNS in pure Go so -static links cleanly.
cd "$HOME/drariux-src/drariux" || { echo "NO_SRC" > "$HOME/buildarm.status"; exit 1; }
export PATH=/usr/local/go/bin:$PATH GOTOOLCHAIN=local CGO_ENABLED=1
S="$HOME/buildarm.status"; L="$HOME/buildarm.log"
echo "apt aarch64 cross toolchain" > "$L"
if ! command -v aarch64-linux-gnu-gcc >/dev/null 2>&1; then
  sudo DEBIAN_FRONTEND=noninteractive apt-get install -y -q gcc-aarch64-linux-gnu libc6-dev-arm64-cross >> "$L" 2>&1 \
    || echo "APT_WARN (continuing)" >> "$L"
fi
command -v aarch64-linux-gnu-gcc >/dev/null 2>&1 || { echo "NO_CROSS_CC" > "$S"; exit 1; }
aarch64-linux-gnu-gcc --version | head -1 >> "$L"

export LC_ALL=C
LDF="-w -s -linkmode external -extld aarch64-linux-gnu-gcc -extldflags \"-static\" -X github.com/cosmos/cosmos-sdk/version.Name=drariux -X github.com/cosmos/cosmos-sdk/version.AppName=drariuxd -X github.com/cosmos/cosmos-sdk/version.Version=v20.0.0"
mkdir -p build
echo "building arm64 static-cgo" >> "$L"
GOOS=linux GOARCH=arm64 CC=aarch64-linux-gnu-gcc go build -tags netgo -trimpath -ldflags "$LDF" -o build/drariuxd_arm64 ./cmd/evmosd 2>> "$L"
rc=$?
echo "BUILD_RC=$rc" > "$S"
if [ $rc -eq 0 ]; then
  file build/drariuxd_arm64 >> "$S"
  gzip -9 -kf build/drariuxd_arm64 && mv build/drariuxd_arm64.gz build/drariuxd_linux_arm64.gz
  ls -la build/drariuxd_linux_arm64.gz >> "$S" 2>&1
  echo "DONE" >> "$S"
fi
