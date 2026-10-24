#!/usr/bin/env bash
# Build release binaries for S-Matrix (linux amd64 + arm64).
set -euo pipefail
ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
VERSION="${VERSION:-$(git -C "${ROOT}" describe --tags --always --dirty 2>/dev/null || echo dev)}"
OUT="${OUT:-${ROOT}/dist}"
LDFLAGS="-s -w -X main.version=${VERSION}"

cd "${ROOT}/s-matrix-backend"
go test ./... >/dev/null

mkdir -p "${OUT}"
echo "building ${VERSION}"

CGO_ENABLED=1 go build -ldflags "${LDFLAGS}" -o "${OUT}/smatrix-linux-amd64" ./cmd/server
echo "  ok amd64"

if command -v aarch64-linux-gnu-gcc >/dev/null 2>&1; then
  CC=aarch64-linux-gnu-gcc CGO_ENABLED=1 GOOS=linux GOARCH=arm64 \
    go build -ldflags "${LDFLAGS}" -o "${OUT}/smatrix-linux-arm64" ./cmd/server
  echo "  ok arm64"
else
  echo "  skip arm64 (需要 aarch64-linux-gnu-gcc)"
fi

( cd "${OUT}" && sha256sum smatrix-linux-* > SHA256SUMS )
echo "artifacts in ${OUT}"
ls -lh "${OUT}"
