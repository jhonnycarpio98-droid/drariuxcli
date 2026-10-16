#!/bin/bash
# Portable Drariux node binary: cgo ON (geth libsecp256k1) + static glibc link.
cd "$HOME/drariux-src/drariux" || { echo "NO_SRC" > "$HOME/build.status"; exit 1; }
export PATH=/usr/local/go/bin:$PATH
export GOTOOLCHAIN=local
export CGO_ENABLED=1
echo "cgo=$(go env CGO_ENABLED) cc=$(go env CC) gcc=$(command -v gcc)" > "$HOME/build.log"

LDF="-w -s -linkmode external -extldflags \"-static\" -X github.com/cosmos/cosmos-sdk/version.Name=drariux -X github.com/cosmos/cosmos-sdk/version.AppName=drariuxd -X github.com/cosmos/cosmos-sdk/version.Version=v20.0.0"
mkdir -p build
echo "building amd64 static-cgo" >> "$HOME/build.log"
GOOS=linux GOARCH=amd64 CC=gcc go build -tags netgo -trimpath -ldflags "$LDF" -o build/drariuxd_amd64 ./cmd/evmosd 2>> "$HOME/build.log"
rc=$?
echo "BUILD_RC=$rc" > "$HOME/build.status"
if [ $rc -eq 0 ]; then
  file build/drariuxd_amd64 >> "$HOME/build.status"
  ./build/drariuxd_amd64 version > "$HOME/build.ver" 2>&1
  gzip -9 -kf build/drariuxd_amd64 && mv build/drariuxd_amd64.gz build/drariuxd_linux_amd64.gz
  ls -la build/drariuxd_linux_amd64.gz >> "$HOME/build.status" 2>&1
  echo "DONE" >> "$HOME/build.status"
fi
